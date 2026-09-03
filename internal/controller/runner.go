package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/elecbug/kpl-v3/internal/model"
	"github.com/elecbug/kpl-v3/internal/scenario"
)

type ServerConfig struct {
	Listen  string
	DataDir string
	Token   string
}

type Server struct {
	config   ServerConfig
	state    *state
	client   *http.Client
	logger   *slog.Logger
	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc
	nodeSeq  atomic.Uint64
}

func New(config ServerConfig, logger *slog.Logger) *Server {
	if config.Listen == "" {
		config.Listen = ":8080"
	}
	if config.DataDir == "" {
		config.DataDir = "data"
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		config:  config,
		state:   newState(config.DataDir),
		client:  &http.Client{Timeout: 10 * time.Second},
		logger:  logger,
		cancels: make(map[string]context.CancelFunc),
	}
}

func (s *Server) StartScenario(parent context.Context, raw []byte) (model.Experiment, error) {
	spec, err := scenario.Parse(raw)
	if err != nil {
		return model.Experiment{}, err
	}
	runID := fmt.Sprintf("run-%s-%04x", time.Now().UTC().Format("20060102T150405Z"), rand.New(rand.NewSource(time.Now().UnixNano())).Intn(65536))
	experiment := model.Experiment{
		ID:           runID,
		Name:         spec.Name,
		State:        "running",
		Seed:         spec.Seed,
		TotalPhases:  len(spec.Phases),
		StartedAt:    time.Now().UTC(),
		ScenarioYAML: string(raw),
	}
	if experiment.Seed == 0 {
		experiment.Seed = time.Now().UnixNano()
	}
	if err := s.persistManifest(experiment, raw); err != nil {
		return model.Experiment{}, err
	}
	s.state.mu.Lock()
	s.state.experiments[runID] = experiment
	s.state.mu.Unlock()
	s.state.notify()

	ctx, cancel := context.WithCancel(parent)
	s.cancelMu.Lock()
	s.cancels[runID] = cancel
	s.cancelMu.Unlock()
	go s.runScenario(ctx, experiment, spec)
	return experiment, nil
}

func (s *Server) StopScenario(runID string) error {
	s.cancelMu.Lock()
	cancel, ok := s.cancels[runID]
	s.cancelMu.Unlock()
	if !ok {
		return fmt.Errorf("running experiment %q not found", runID)
	}
	cancel()
	return nil
}

func (s *Server) runScenario(ctx context.Context, experiment model.Experiment, spec scenario.Scenario) {
	rng := rand.New(rand.NewSource(experiment.Seed))
	var runErr error
	for index, phase := range spec.Phases {
		if err := ctx.Err(); err != nil {
			runErr = err
			break
		}
		s.updateExperiment(experiment.ID, func(current *model.Experiment) {
			current.Phase = index + 1
			current.PhaseName = phase.Name
		})
		s.logger.Info("scenario phase started", "run", experiment.ID, "phase", phase.Name, "action", phase.Action)
		switch phase.Action {
		case "join":
			runErr = s.runJoin(ctx, experiment.ID, phase, rng)
		case "wait":
			duration, _ := time.ParseDuration(phase.Duration)
			runErr = sleepContext(ctx, duration)
		case "wait-ready":
			runErr = s.waitReady(ctx, experiment.ID, phase)
		case "publish":
			runErr = s.runPublish(ctx, experiment.ID, phase, rng)
		case "leave":
			runErr = s.runLeave(ctx, experiment.ID, phase, rng)
		case "stop-all":
			runErr = s.stopNodes(ctx, experiment.ID, "", 0, rng, scenario.Distribution{})
		}
		if runErr != nil {
			break
		}
	}
	finished := time.Now().UTC()
	s.updateExperiment(experiment.ID, func(current *model.Experiment) {
		current.FinishedAt = finished
		current.PhaseName = ""
		switch {
		case runErr == nil:
			current.State = "completed"
		case runErr == context.Canceled:
			current.State = "canceled"
		default:
			current.State = "failed"
			current.Error = runErr.Error()
		}
	})
	s.cancelMu.Lock()
	delete(s.cancels, experiment.ID)
	s.cancelMu.Unlock()
	if runErr != nil && runErr != context.Canceled {
		s.logger.Error("scenario failed", "run", experiment.ID, "error", runErr)
	} else {
		s.logger.Info("scenario finished", "run", experiment.ID, "result", runErr)
	}
}

func (s *Server) runJoin(ctx context.Context, runID string, phase scenario.Phase, rng *rand.Rand) error {
	for i := 0; i < phase.Count; i++ {
		agent, err := s.chooseAgent()
		if err != nil {
			return err
		}
		seq := s.nodeSeq.Add(1)
		nodeID := fmt.Sprintf("%s-%s-%05d", runID, safeName(phase.Group), seq)
		lifetime := ""
		if phase.Lifetime.Model != "" {
			lifetime = phase.Lifetime.Sample(rng).String()
		}
		request := model.CreateNodeRequest{
			ID:       nodeID,
			RunID:    runID,
			Group:    phase.Group,
			Role:     phase.Role,
			Seed:     rng.Int63(),
			Config:   phase.Node.WithDefaults(),
			Lifetime: lifetime,
		}
		if err := s.callAgent(ctx, agent.URL, http.MethodPost, "/api/v1/nodes", request, nil); err != nil {
			s.releaseAgent(agent.ID)
			return fmt.Errorf("create node %s on agent %s: %w", nodeID, agent.ID, err)
		}
		if i+1 < phase.Count {
			if err := sleepContext(ctx, phase.Interval.Sample(rng)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) waitReady(ctx context.Context, runID string, phase scenario.Phase) error {
	timeout, _ := time.ParseDuration(phase.Timeout)
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		ready, total := s.readyCount(runID, phase.Group)
		if total > 0 && float64(ready)/float64(total) >= phase.ReadyRatio {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("ready barrier timed out for group %q: %d/%d ready", phase.Group, ready, total)
		case <-ticker.C:
		}
	}
}

func (s *Server) runPublish(ctx context.Context, runID string, phase scenario.Phase, rng *rand.Rand) error {
	for i := 0; i < phase.Count; i++ {
		nodes := s.matchingNodes(runID, phase.Group, model.NodeReady)
		if len(nodes) == 0 {
			return fmt.Errorf("no ready nodes in group %q", phase.Group)
		}
		node := nodes[rng.Intn(len(nodes))]
		agent, ok := s.agent(node.AgentID)
		if !ok || agent.State != model.AgentOnline {
			return fmt.Errorf("agent %q for node %q is unavailable", node.AgentID, node.ID)
		}
		request := model.PublishRequest{RunID: runID, Topic: phase.Topic, PayloadSize: phase.PayloadSize, TargetNodes: len(s.matchingNodes(runID, "", model.NodeReady))}
		path := "/api/v1/nodes/" + node.ID + "/publish"
		if err := s.callAgent(ctx, agent.URL, http.MethodPost, path, request, nil); err != nil {
			return fmt.Errorf("publish through node %s: %w", node.ID, err)
		}
		if i+1 < phase.Count {
			if err := sleepContext(ctx, phase.Interval.Sample(rng)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) runLeave(ctx context.Context, runID string, phase scenario.Phase, rng *rand.Rand) error {
	return s.stopNodes(ctx, runID, phase.Group, phase.Count, rng, phase.Interval)
}

func (s *Server) stopNodes(ctx context.Context, runID, group string, count int, rng *rand.Rand, interval scenario.Distribution) error {
	nodes := s.matchingNodes(runID, group, "")
	rng.Shuffle(len(nodes), func(i, j int) { nodes[i], nodes[j] = nodes[j], nodes[i] })
	if count <= 0 || count > len(nodes) {
		count = len(nodes)
	}
	for i := 0; i < count; i++ {
		node := nodes[i]
		agent, ok := s.agent(node.AgentID)
		if !ok {
			continue
		}
		if err := s.callAgent(ctx, agent.URL, http.MethodDelete, "/api/v1/nodes/"+node.ID, nil, nil); err != nil {
			return fmt.Errorf("stop node %s: %w", node.ID, err)
		}
		if i+1 < count {
			if err := sleepContext(ctx, interval.Sample(rng)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) chooseAgent() (model.Agent, error) {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	var candidates []model.Agent
	for _, agent := range s.state.agents {
		if agent.State == model.AgentOnline && (agent.Capacity <= 0 || agent.ActiveNodes < agent.Capacity) {
			candidates = append(candidates, agent)
		}
	}
	if len(candidates) == 0 {
		return model.Agent{}, fmt.Errorf("no online agent has free capacity")
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := utilization(candidates[i]), utilization(candidates[j])
		if left == right {
			return candidates[i].ID < candidates[j].ID
		}
		return left < right
	})
	selected := candidates[0]
	selected.ActiveNodes++
	s.state.agents[selected.ID] = selected
	return selected, nil
}

func utilization(agent model.Agent) float64 {
	if agent.Capacity <= 0 {
		return float64(agent.ActiveNodes)
	}
	return float64(agent.ActiveNodes) / float64(agent.Capacity)
}

func (s *Server) releaseAgent(id string) {
	s.state.mu.Lock()
	agent, ok := s.state.agents[id]
	if ok && agent.ActiveNodes > 0 {
		agent.ActiveNodes--
		s.state.agents[id] = agent
	}
	s.state.mu.Unlock()
}

func (s *Server) readyCount(runID, group string) (int, int) {
	nodes := s.matchingNodes(runID, group, "")
	ready := 0
	for _, node := range nodes {
		if node.State == model.NodeReady {
			ready++
		}
	}
	return ready, len(nodes)
}

func (s *Server) matchingNodes(runID, group, state string) []model.Node {
	s.state.mu.RLock()
	defer s.state.mu.RUnlock()
	var nodes []model.Node
	for _, node := range s.state.nodes {
		if node.RunID != runID || group != "" && node.Group != group {
			continue
		}
		if state != "" && node.State != state {
			continue
		}
		if node.State == model.NodeStopped || node.State == model.NodeFailed {
			continue
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func (s *Server) agent(id string) (model.Agent, bool) {
	s.state.mu.RLock()
	defer s.state.mu.RUnlock()
	agent, ok := s.state.agents[id]
	return agent, ok
}

func (s *Server) callAgent(ctx context.Context, baseURL, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(baseURL, "/")+path, body)
	if err != nil {
		return err
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if s.config.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.config.Token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("agent returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	if output != nil {
		return json.NewDecoder(resp.Body).Decode(output)
	}
	return nil
}

func (s *Server) updateExperiment(runID string, update func(*model.Experiment)) {
	s.state.mu.Lock()
	experiment := s.state.experiments[runID]
	update(&experiment)
	s.state.experiments[runID] = experiment
	s.state.mu.Unlock()
	s.state.notify()
	if err := s.persistExperiment(experiment); err != nil {
		s.logger.Warn("persist experiment state", "run", runID, "error", err)
	}
}

func (s *Server) persistManifest(experiment model.Experiment, raw []byte) error {
	dir := filepath.Join(s.config.DataDir, "runs", safeName(experiment.ID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create run directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenario.yaml"), raw, 0o644); err != nil {
		return fmt.Errorf("write scenario manifest: %w", err)
	}
	return s.persistExperiment(experiment)
}

func (s *Server) persistExperiment(experiment model.Experiment) error {
	dir := filepath.Join(s.config.DataDir, "runs", safeName(experiment.ID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create run directory: %w", err)
	}
	metadata, err := json.MarshalIndent(experiment, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "experiment.json"), metadata, 0o644)
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
