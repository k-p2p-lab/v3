package controller

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
)

const recentEventLimit = 300

type state struct {
	mu           sync.RWMutex
	persistMu    sync.Mutex
	agents       map[string]model.Agent
	nodes        map[string]model.Node
	reservations map[string]string
	experiments  map[string]model.Experiment
	events       []model.TraceEvent
	watchers     map[chan struct{}]struct{}
	dataDir      string
}

func newState(dataDir string) *state {
	return &state{
		agents:       make(map[string]model.Agent),
		nodes:        make(map[string]model.Node),
		reservations: make(map[string]string),
		experiments:  make(map[string]model.Experiment),
		watchers:     make(map[chan struct{}]struct{}),
		dataDir:      dataDir,
	}
}

func (s *state) registerAgent(agent model.Agent) (model.Agent, error) {
	if strings.TrimSpace(agent.ID) == "" || strings.TrimSpace(agent.URL) == "" {
		return model.Agent{}, fmt.Errorf("agent id and url are required")
	}
	now := time.Now().UTC()
	s.mu.Lock()
	previous, exists := s.agents[agent.ID]
	if exists && agent.StartedAt.IsZero() {
		agent.StartedAt = previous.StartedAt
	}
	if exists {
		agent.ActiveNodes = previous.ActiveNodes
	} else {
		for _, agentID := range s.reservations {
			if agentID == agent.ID {
				agent.ActiveNodes++
			}
		}
	}
	if agent.StartedAt.IsZero() {
		agent.StartedAt = now
	}
	agent.LastSeen = now
	agent.State = model.AgentOnline
	s.agents[agent.ID] = agent
	s.mu.Unlock()
	s.notify()
	return agent, nil
}

func (s *state) heartbeat(h model.AgentHeartbeat) error {
	if h.Agent.ID == "" {
		return fmt.Errorf("agent id is required")
	}
	now := time.Now().UTC()
	s.mu.Lock()
	previous, ok := s.agents[h.Agent.ID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("agent %q is not registered", h.Agent.ID)
	}
	h.Agent.URL = firstNonEmpty(h.Agent.URL, previous.URL)
	h.Agent.Name = firstNonEmpty(h.Agent.Name, previous.Name)
	h.Agent.Hostname = firstNonEmpty(h.Agent.Hostname, previous.Hostname)
	h.Agent.Version = firstNonEmpty(h.Agent.Version, previous.Version)
	if h.Agent.Capacity == 0 {
		h.Agent.Capacity = previous.Capacity
	}
	if h.Agent.StartedAt.IsZero() {
		h.Agent.StartedAt = previous.StartedAt
	}
	h.Agent.LastSeen = now
	h.Agent.State = model.AgentOnline
	h.Agent.ActiveNodes = 0
	seen := make(map[string]struct{}, len(h.Nodes))
	for _, node := range h.Nodes {
		node.AgentID = h.Agent.ID
		if node.LastSeen.IsZero() {
			node.LastSeen = now
		}
		if node.StartedAt.IsZero() {
			if old, found := s.nodes[node.ID]; found {
				node.StartedAt = old.StartedAt
			} else {
				node.StartedAt = now
			}
		}
		if old, found := s.nodes[node.ID]; found && old.State == model.NodeStopping && node.State != model.NodeStopping && node.State != model.NodeStopped && node.State != model.NodeFailed {
			// Stopping is a local tombstone. A heartbeat assembled before the
			// DELETE must not resurrect the peer or consume capacity again.
			node = old
		}
		s.nodes[node.ID] = node
		if s.reservations[node.ID] == h.Agent.ID {
			delete(s.reservations, node.ID)
		}
		seen[node.ID] = struct{}{}
		if node.State != model.NodeStopping && node.State != model.NodeStopped && node.State != model.NodeFailed {
			h.Agent.ActiveNodes++
		}
	}
	for _, agentID := range s.reservations {
		if agentID == h.Agent.ID {
			h.Agent.ActiveNodes++
		}
	}
	for id, node := range s.nodes {
		if node.AgentID != h.Agent.ID {
			continue
		}
		if _, found := seen[id]; !found && node.State != model.NodeStopped {
			if s.reservations[id] == h.Agent.ID {
				continue
			}
			node.State = model.NodeStopped
			node.LastSeen = now
			s.nodes[id] = node
		}
	}
	s.agents[h.Agent.ID] = h.Agent
	s.mu.Unlock()
	s.notify()
	return nil
}

func (s *state) markStaleAgents(maxAge time.Duration) {
	now := time.Now().UTC()
	changed := false
	s.mu.Lock()
	for id, agent := range s.agents {
		if now.Sub(agent.LastSeen) > maxAge && agent.State != model.AgentOffline {
			agent.State = model.AgentOffline
			s.agents[id] = agent
			changed = true
		}
	}
	s.mu.Unlock()
	if changed {
		s.notify()
	}
}

func (s *state) appendEvents(batch model.EventBatch) error {
	if len(batch.Events) == 0 {
		return nil
	}
	now := time.Now().UTC()
	for i := range batch.Events {
		if batch.Events[i].AgentID == "" {
			batch.Events[i].AgentID = batch.AgentID
		}
		if batch.Events[i].Timestamp.IsZero() {
			batch.Events[i].Timestamp = now
		}
	}
	s.mu.Lock()
	s.events = append(s.events, batch.Events...)
	if len(s.events) > recentEventLimit {
		s.events = append([]model.TraceEvent(nil), s.events[len(s.events)-recentEventLimit:]...)
	}
	s.mu.Unlock()

	byRun := make(map[string][]model.TraceEvent)
	for _, event := range batch.Events {
		if event.RunID != "" {
			byRun[event.RunID] = append(byRun[event.RunID], event)
		}
	}
	for runID, events := range byRun {
		if err := s.persistEvents(runID, events); err != nil {
			return err
		}
	}
	s.notify()
	return nil
}

func (s *state) persistEvents(runID string, events []model.TraceEvent) error {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	runID = safeName(runID)
	dir := filepath.Join(s.dataDir, "runs", runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create run directory: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open event log: %w", err)
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 64*1024)
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("encode event: %w", err)
		}
		if _, err := w.Write(append(data, '\n')); err != nil {
			return fmt.Errorf("write event: %w", err)
		}
	}
	return w.Flush()
}

func (s *state) snapshot() model.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := model.Snapshot{GeneratedAt: time.Now().UTC()}
	for _, agent := range s.agents {
		result.Agents = append(result.Agents, agent)
	}
	for _, node := range s.nodes {
		result.Nodes = append(result.Nodes, node)
	}
	for _, experiment := range s.experiments {
		result.Experiments = append(result.Experiments, experiment)
	}
	result.Events = append([]model.TraceEvent(nil), s.events...)
	sort.Slice(result.Agents, func(i, j int) bool { return result.Agents[i].ID < result.Agents[j].ID })
	sort.Slice(result.Nodes, func(i, j int) bool { return result.Nodes[i].ID < result.Nodes[j].ID })
	sort.Slice(result.Experiments, func(i, j int) bool {
		return result.Experiments[i].StartedAt.After(result.Experiments[j].StartedAt)
	})
	result.Edges = networkEdges(result.Nodes)
	result.Metrics = calculateMetrics(result.Nodes, result.Experiments, result.Events)
	return result
}

func networkEdges(nodes []model.Node) []model.Edge {
	peerToNode := make(map[string]string)
	for _, node := range nodes {
		if node.PeerID != "" && node.State == model.NodeReady {
			peerToNode[node.PeerID] = node.ID
		}
	}
	seen := make(map[string]struct{})
	var edges []model.Edge
	for _, node := range nodes {
		if node.State != model.NodeReady {
			continue
		}
		for _, remotePeer := range node.ConnectedPeers {
			target, ok := peerToNode[remotePeer]
			if !ok || target == node.ID {
				continue
			}
			a, b := node.ID, target
			if a > b {
				a, b = b, a
			}
			key := a + "\x00" + b
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			edges = append(edges, model.Edge{Source: a, Target: b})
		}
	}
	return edges
}

func calculateMetrics(nodes []model.Node, experiments []model.Experiment, events []model.TraceEvent) model.Metrics {
	var result model.Metrics
	events = append([]model.TraceEvent(nil), events...)
	sort.SliceStable(events, func(i, j int) bool { return events[i].Timestamp.Before(events[j].Timestamp) })
	activeRun := ""
	if len(experiments) > 0 {
		activeRun = experiments[0].ID
	}
	participantNodes := 0
	for _, node := range nodes {
		if node.RunID == activeRun && node.State != model.NodeFailed {
			participantNodes++
		}
	}
	latencies := make([]float64, 0)
	reached := make(map[string]map[string]struct{})
	targets := make(map[string]int)
	published := make(map[string]struct{})
	for _, event := range events {
		if activeRun != "" && event.RunID != activeRun {
			continue
		}
		switch event.Type {
		case "publish":
			result.Published++
			if event.MessageID != "" {
				if reached[event.MessageID] == nil {
					reached[event.MessageID] = make(map[string]struct{})
				}
				reached[event.MessageID][event.NodeID] = struct{}{}
				published[event.MessageID] = struct{}{}
				if count := numberField(event.Fields, "targetNodes"); count > 0 {
					targets[event.MessageID] = count
				}
			}
		case "deliver":
			result.Delivered++
			if event.LatencyMS >= 0 {
				latencies = append(latencies, event.LatencyMS)
			}
			if event.MessageID != "" {
				if reached[event.MessageID] == nil {
					reached[event.MessageID] = make(map[string]struct{})
				}
				reached[event.MessageID][event.NodeID] = struct{}{}
			}
		case "duplicate":
			result.Duplicates++
		}
	}
	if len(latencies) > 0 {
		sort.Float64s(latencies)
		for _, value := range latencies {
			result.AverageLatencyMS += value
		}
		result.AverageLatencyMS /= float64(len(latencies))
		index := int(math.Ceil(float64(len(latencies))*0.95)) - 1
		if index < 0 {
			index = 0
		}
		result.P95LatencyMS = latencies[index]
	}
	if participantNodes > 0 && len(published) > 0 {
		for messageID := range published {
			denominator := targets[messageID]
			if denominator == 0 {
				denominator = participantNodes
			}
			result.Reachability += math.Min(1, float64(len(reached[messageID]))/float64(denominator))
		}
		result.Reachability /= float64(len(published))
	}
	return result
}

func numberField(fields map[string]any, key string) int {
	value, ok := fields[key]
	if !ok {
		return 0
	}
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	case json.Number:
		parsed, _ := number.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func (s *state) subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.watchers[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.watchers, ch)
		s.mu.Unlock()
	}
}

func (s *state) notify() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for watcher := range s.watchers {
		select {
		case watcher <- struct{}{}:
		default:
		}
	}
}

func safeName(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "unnamed"
	}
	return b.String()
}

func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
