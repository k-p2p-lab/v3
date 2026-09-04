package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
)

type Config struct {
	Runtime       string
	DockerBinary  string
	DockerImage   string
	DockerNetwork string
	ID            string
	Name          string
	Listen        string
	AdvertiseURL  string
	SelfURL       string
	ControllerURL string
	Token         string
	Capacity      int
	DataDir       string
	Executable    string
	PeerAPIPort   int
	PeerP2PPort   int
	Labels        map[string]string
}

type process struct {
	node        model.Node
	apiURL      string
	configPath  string
	command     *exec.Cmd
	cancel      context.CancelFunc
	lease       *portLease
	exited      bool
	proxyRefs   int
	containerID string
	done        chan struct{}
	cleanupErr  error
}

type Server struct {
	config         Config
	logger         *slog.Logger
	client         *http.Client
	commandContext func(context.Context, string, ...string) *exec.Cmd
	docker         *dockerRuntime
	startedAt      time.Time
	mu             sync.RWMutex
	processes      map[string]*process
	ports          portPool
	runFences      map[string]uint64
	shuttingDown   bool
	eventsMu       sync.Mutex
	events         []model.TraceEvent
	flushNow       chan struct{}
}

func New(config Config, logger *slog.Logger) (*Server, error) {
	if config.Runtime == "" {
		config.Runtime = "docker"
	}
	if config.Runtime != "docker" && config.Runtime != "process" {
		return nil, fmt.Errorf("runtime must be docker or process")
	}
	if config.DockerBinary == "" {
		config.DockerBinary = "docker"
	}
	if config.DockerImage == "" {
		config.DockerImage = "kpl-v3:local"
	}
	if config.DockerNetwork == "" {
		config.DockerNetwork = "kpl-v3-peers"
	}
	if config.ID == "" {
		return nil, fmt.Errorf("agent id is required")
	}
	if config.Name == "" {
		config.Name = config.ID
	}
	if config.Listen == "" {
		config.Listen = ":8090"
	}
	if config.ControllerURL == "" || config.AdvertiseURL == "" {
		return nil, fmt.Errorf("controller url and advertise url are required")
	}
	if config.Capacity <= 0 {
		config.Capacity = 100
	}
	if config.DataDir == "" {
		config.DataDir = "data-agent"
	}
	if config.PeerAPIPort == 0 {
		config.PeerAPIPort = 18000
	}
	if config.PeerP2PPort == 0 {
		config.PeerP2PPort = 20000
	}
	if config.Executable == "" {
		executable, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve executable: %w", err)
		}
		config.Executable = executable
	}
	if config.SelfURL == "" {
		if config.Runtime == "docker" {
			config.SelfURL = config.AdvertiseURL
		} else {
			config.SelfURL = localURL(config.Listen)
		}
	}
	if config.Runtime == "docker" {
		for name, raw := range map[string]string{"controller-url": config.ControllerURL, "self-url": config.SelfURL} {
			parsed, err := url.Parse(raw)
			if err != nil || parsed.Hostname() == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
				return nil, fmt.Errorf("%s must be an HTTP(S) URL reachable from peer containers", name)
			}
			ip := net.ParseIP(parsed.Hostname())
			if strings.EqualFold(parsed.Hostname(), "localhost") || ip != nil && (ip.IsLoopback() || ip.IsUnspecified()) {
				return nil, fmt.Errorf("%s must be reachable from peer containers; loopback and unspecified addresses refer to the peer itself", name)
			}
		}
	}
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		config:         config,
		logger:         logger,
		client:         &http.Client{Timeout: 10 * time.Second},
		commandContext: exec.CommandContext,
		startedAt:      time.Now().UTC(),
		processes:      make(map[string]*process),
		runFences:      make(map[string]uint64),
		flushNow:       make(chan struct{}, 1),
	}
	if config.Runtime == "docker" {
		s.docker = &dockerRuntime{binary: config.DockerBinary, image: config.DockerImage, network: config.DockerNetwork, commandContext: exec.CommandContext}
	}
	return s, nil
}

func (s *Server) Run(ctx context.Context) error {
	// Bind before reconciling: a duplicate local Agent must not remove the
	// running Agent's peers and only then discover that its port is occupied.
	listener, err := net.Listen("tcp", s.config.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	if s.docker != nil {
		checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := s.docker.check(checkCtx)
		cancel()
		if err != nil {
			return fmt.Errorf("Docker runtime: %w", err)
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(ctx, containerStopTimeout)
		err = s.docker.removeAgentContainers(cleanupCtx, s.config.ID)
		cleanupCancel()
		if err != nil {
			return fmt.Errorf("clean up previous peer containers: %w", err)
		}
	}
	server := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      3 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}
	defer func() { s.beginShutdown(); s.waitStopped(containerStopTimeout) }()
	go s.controlLoop(ctx)
	go func() {
		<-ctx.Done()
		s.beginShutdown()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	s.logger.Info("agent listening", "id", s.config.ID, "address", listener.Addr(), "advertise", s.config.AdvertiseURL)
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) controlLoop(ctx context.Context) {
	registered := false
	heartbeat := time.NewTicker(2 * time.Second)
	flush := time.NewTicker(500 * time.Millisecond)
	defer heartbeat.Stop()
	defer flush.Stop()
	for {
		if !registered {
			if err := s.register(ctx); err != nil {
				s.logger.Warn("agent registration failed", "error", err)
			} else {
				registered = true
			}
		}
		select {
		case <-ctx.Done():
			s.flushEvents(context.Background())
			return
		case <-heartbeat.C:
			if registered {
				if err := s.heartbeat(ctx); err != nil {
					s.logger.Warn("agent heartbeat failed", "error", err)
					registered = false
				}
			}
		case <-flush.C:
			s.flushEvents(ctx)
		case <-s.flushNow:
			s.flushEvents(ctx)
		}
	}
}

func (s *Server) register(ctx context.Context) error {
	hostname, _ := os.Hostname()
	agent := model.Agent{
		ID:        s.config.ID,
		Name:      s.config.Name,
		URL:       strings.TrimRight(s.config.AdvertiseURL, "/"),
		Hostname:  hostname,
		Version:   "v3-dev",
		Capacity:  s.config.Capacity,
		State:     model.AgentOnline,
		Labels:    s.config.Labels,
		StartedAt: s.startedAt,
	}
	return s.postJSON(ctx, "/api/v1/agents/register", agent, nil)
}

func (s *Server) heartbeat(ctx context.Context) error {
	nodes := s.nodes()
	hostname, _ := os.Hostname()
	h := model.AgentHeartbeat{
		Agent: model.Agent{
			ID:          s.config.ID,
			Name:        s.config.Name,
			URL:         strings.TrimRight(s.config.AdvertiseURL, "/"),
			Hostname:    hostname,
			Version:     "v3-dev",
			Capacity:    s.config.Capacity,
			ActiveNodes: activeNodeCount(nodes),
			State:       model.AgentOnline,
			Labels:      s.config.Labels,
			StartedAt:   s.startedAt,
		},
		Nodes: nodes,
	}
	return s.postJSON(ctx, "/api/v1/agents/heartbeat", h, nil)
}

func (s *Server) createNode(ctx context.Context, request model.CreateNodeRequest) (model.Node, error) {
	if request.ID == "" || request.RunID == "" || request.Group == "" {
		return model.Node{}, fmt.Errorf("id, runId, and group are required")
	}
	if request.Lifetime != "" {
		lifetime, err := time.ParseDuration(request.Lifetime)
		if err != nil || lifetime < 0 {
			return model.Node{}, fmt.Errorf("lifetime must be a non-negative duration")
		}
	}
	resolvedConfig, nodeType := resolveCreateNodeConfig(request)
	if err := resolvedConfig.Validate(); err != nil {
		return model.Node{}, fmt.Errorf("invalid node config: %w", err)
	}
	if resolvedConfig.Network.Enabled() && s.config.Runtime != "docker" {
		return model.Node{}, fmt.Errorf("network conditions require the docker runtime; process mode cannot isolate network changes")
	}
	requestedNetwork, _ := json.Marshal(resolvedConfig.Network)
	networkConfig, err := resolvedConfig.Network.Resolve(rand.New(rand.NewSource(request.Seed)))
	if err != nil {
		return model.Node{}, fmt.Errorf("resolve peer network conditions: %w", err)
	}
	resolvedConfig.Network = networkConfig
	s.mu.Lock()
	if s.shuttingDown {
		s.mu.Unlock()
		return model.Node{}, fmt.Errorf("agent is shutting down")
	}
	if fence, exists := s.runFences[request.RunID]; exists && request.Generation <= fence {
		s.mu.Unlock()
		return model.Node{}, fmt.Errorf("run %q generation %d is fenced at generation %d", request.RunID, request.Generation, fence)
	}
	if _, exists := s.processes[request.ID]; exists {
		s.mu.Unlock()
		return model.Node{}, fmt.Errorf("node %q already exists", request.ID)
	}
	if activeNodeCountLocked(s.processes) >= s.config.Capacity {
		s.mu.Unlock()
		return model.Node{}, fmt.Errorf("agent capacity reached")
	}
	var lease *portLease
	if s.config.Runtime != "docker" {
		var ok bool
		lease, ok = s.ports.acquire(s.config.PeerAPIPort, s.config.PeerP2PPort)
		if !ok {
			s.mu.Unlock()
			return model.Node{}, fmt.Errorf("peer port range exhausted")
		}
	}
	now := time.Now().UTC()
	profile := request.Profile
	if profile == "" {
		profile = nodeType
	}
	topicsJSON, _ := json.Marshal(resolvedConfig.GossipSub.Topics)
	node := model.Node{
		ID:         request.ID,
		RunID:      request.RunID,
		Generation: request.Generation,
		AgentID:    s.config.ID,
		Group:      request.Group,
		Role:       request.Role,
		Type:       nodeType,
		Profile:    profile,
		State:      model.NodeStarting,
		Metadata: map[string]string{
			"runtime":       s.config.Runtime,
			"profile":       profile,
			"pubsubRouter":  resolvedConfig.GossipSub.Router,
			"pubsubEnabled": strconv.FormatBool(resolvedConfig.GossipSub.Enabled != nil && *resolvedConfig.GossipSub.Enabled),
			"allowPublish":  strconv.FormatBool(resolvedConfig.PublishAllowed()),
			"topicMode":     resolvedConfig.GossipSub.TopicMode,
			"topics":        strings.Join(resolvedConfig.GossipSub.Topics, ","),
			"topicsJSON":    string(topicsJSON),
			"dhtEnabled":    strconv.FormatBool(resolvedConfig.Kademlia.Enabled != nil && *resolvedConfig.Kademlia.Enabled),
			"dhtMode":       resolvedConfig.Kademlia.Mode,
		},
		StartedAt: now,
		LastSeen:  now,
	}
	processCtx, cancel := context.WithCancel(ctx)
	apiListen, p2pListen := "0.0.0.0:18000", "/ip4/0.0.0.0/tcp/20000"
	if lease != nil {
		apiListen = fmt.Sprintf("127.0.0.1:%d", lease.apiPort)
		p2pListen = fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", lease.p2pPort)
	}
	networkJSON, _ := json.Marshal(resolvedConfig.Network)
	node.Metadata["network"] = string(networkJSON)
	node.Metadata["networkRequested"] = string(requestedNetwork)
	node.Metadata["seed"] = strconv.FormatInt(request.Seed, 10)
	if request.Lifetime != "" {
		node.Metadata["lifetime"] = request.Lifetime
		node.Metadata["lifetimeBasis"] = "process-started"
		if s.config.Runtime == "docker" {
			node.Metadata["lifetimeBasis"] = "container-created"
		}
	}
	peerConfig := model.PeerProcessConfig{
		Runtime:       s.config.Runtime,
		Node:          node,
		NodeConfig:    resolvedConfig,
		Seed:          request.Seed,
		ControllerURL: strings.TrimRight(s.config.ControllerURL, "/"),
		AgentURL:      strings.TrimRight(s.config.SelfURL, "/"),
		APListen:      apiListen,
		P2PListen:     p2pListen,
		Token:         s.config.Token,
	}
	configPath, err := s.writePeerConfig(peerConfig)
	if err != nil {
		cancel()
		s.ports.release(lease)
		s.mu.Unlock()
		return model.Node{}, err
	}
	if s.config.Runtime == "docker" {
		data, err := json.Marshal(peerConfig)
		if err != nil {
			cancel()
			s.mu.Unlock()
			return model.Node{}, err
		}
		proc := &process{node: node, configPath: configPath, cancel: cancel, done: make(chan struct{})}
		s.processes[request.ID] = proc
		s.mu.Unlock()
		go s.runDockerProcess(processCtx, proc, data, request.Lifetime)
		return node, nil
	}
	commandContext := s.commandContext
	if commandContext == nil {
		commandContext = exec.CommandContext
	}
	command := commandContext(processCtx, s.config.Executable, "peer", "--config", configPath)
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		s.ports.release(lease)
		s.mu.Unlock()
		return model.Node{}, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		cancel()
		s.ports.release(lease)
		s.mu.Unlock()
		return model.Node{}, err
	}
	proc := &process{node: node, apiURL: fmt.Sprintf("http://127.0.0.1:%d", lease.apiPort), configPath: configPath, command: command, cancel: cancel, lease: lease, done: make(chan struct{})}
	if err := command.Start(); err != nil {
		cancel()
		s.ports.release(lease)
		s.mu.Unlock()
		return model.Node{}, fmt.Errorf("start peer process: %w", err)
	}
	s.processes[request.ID] = proc
	s.mu.Unlock()
	go s.pipeLogs(request.ID, "stdout", stdout)
	go s.pipeLogs(request.ID, "stderr", stderr)
	go s.waitProcess(request.ID, proc)
	scheduleLifetimeStop(processCtx, request.Lifetime, func() { _ = s.stopNode(request.ID) })
	return node, nil
}

func resolveCreateNodeConfig(request model.CreateNodeRequest) (model.NodeConfig, string) {
	kind := request.Type
	if kind == "" {
		kind = request.Config.Type
	}
	if kind == "" {
		if request.Role == "boot" {
			kind = "boot"
		} else {
			kind = "full"
		}
	}
	if base, ok := model.BuiltInNodeConfig(kind); ok {
		resolved := base.Merge(request.Config).WithDefaults()
		// The chosen built-in preset is authoritative, including aliases such as
		// worker, which canonicalize to full.
		resolved.Type = base.Type
		return resolved, resolved.Type
	}
	base, _ := model.BuiltInNodeConfig("full")
	base.Type = kind
	resolved := base.Merge(request.Config)
	resolved.Type = kind
	resolved = resolved.WithDefaults()
	return resolved, resolved.Type
}

func scheduleLifetimeStop(ctx context.Context, value string, stop func()) bool {
	if value == "" {
		return false
	}
	lifetime, err := time.ParseDuration(value)
	if err != nil || lifetime < 0 {
		return false
	}
	go func() {
		timer := time.NewTimer(lifetime)
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-timer.C:
			stop()
		}
	}()
	return true
}

func (s *Server) writePeerConfig(config model.PeerProcessConfig) (string, error) {
	dir := filepath.Join(s.config.DataDir, "nodes", safeName(config.Node.ID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "peer.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Server) pipeLogs(nodeID, stream string, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 2*1024*1024)
	for scanner.Scan() {
		s.logger.Debug("peer output", "node", nodeID, "stream", stream, "message", scanner.Text())
	}
}

func (s *Server) waitProcess(nodeID string, proc *process) {
	err := proc.command.Wait()
	s.finishProcess(nodeID, proc, err, nil)
}

func (s *Server) finishProcess(nodeID string, proc *process, runErr, cleanupErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	proc.exited = true
	proc.cleanupErr = cleanupErr
	current, ok := s.processes[nodeID]
	if ok && current == proc {
		current.node.LastSeen = time.Now().UTC()
		setProcessMetadata(current, "stoppedAt", current.node.LastSeen.Format(time.RFC3339Nano))
		if cleanupErr == nil && (current.node.State == model.NodeStopping || runErr == nil) {
			current.node.State = model.NodeStopped
			current.node.Error = ""
		} else {
			current.node.State = model.NodeFailed
			current.node.Error = errors.Join(runErr, cleanupErr).Error()
		}
	}
	s.releaseProcessPortsLocked(proc)
	if proc.done != nil {
		close(proc.done)
	}
}

func (s *Server) releaseProcessPortsLocked(proc *process) {
	if proc.exited && proc.proxyRefs == 0 {
		s.ports.release(proc.lease)
	}
}

func (s *Server) stopNode(nodeID string) error {
	s.mu.Lock()
	proc, ok := s.processes[nodeID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("node %q not found", nodeID)
	}
	if proc.node.State == model.NodeStopped || proc.node.State == model.NodeFailed {
		if proc.cleanupErr != nil && proc.containerID != "" {
			s.retryContainerCleanupLocked(proc)
		}
		s.mu.Unlock()
		return nil
	}
	proc.node.State = model.NodeStopping
	proc.node.LastSeen = time.Now().UTC()
	if proc.node.Metadata["stopRequestedAt"] == "" {
		setProcessMetadata(proc, "stopRequestedAt", proc.node.LastSeen.Format(time.RFC3339Nano))
	}
	cancel := proc.cancel
	s.mu.Unlock()
	cancel()
	return nil
}

func (s *Server) stopAll() {
	s.mu.RLock()
	ids := make([]string, 0, len(s.processes))
	for id := range s.processes {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	for _, id := range ids {
		_ = s.stopNode(id)
	}
}

// stopRunGeneration establishes the fence before marking matching processes as
// stopping. A create for the stopped generation can therefore never commit
// after this method has taken the lock: it either committed first and is in the
// cancellation set, or observes the fence and is rejected.
func (s *Server) stopRunGeneration(runID string, generation uint64) {
	s.mu.Lock()
	if s.runFences == nil {
		s.runFences = make(map[string]uint64)
	}
	fence, exists := s.runFences[runID]
	if !exists || generation > fence {
		fence = generation
		s.runFences[runID] = fence
	}
	now := time.Now().UTC()
	cancels := make([]context.CancelFunc, 0)
	for _, proc := range s.processes {
		if proc.node.RunID != runID || proc.node.Generation > fence {
			continue
		}
		if proc.exited {
			if proc.cleanupErr != nil && proc.containerID != "" {
				s.retryContainerCleanupLocked(proc)
			}
			continue
		}
		if proc.node.State == model.NodeStopping || proc.node.State == model.NodeStopped || proc.node.State == model.NodeFailed {
			continue
		}
		proc.node.State = model.NodeStopping
		proc.node.LastSeen = now
		if proc.node.Metadata["stopRequestedAt"] == "" {
			setProcessMetadata(proc, "stopRequestedAt", now.Format(time.RFC3339Nano))
		}
		if proc.cancel != nil {
			cancels = append(cancels, proc.cancel)
		}
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (s *Server) updateNode(update model.Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	proc, ok := s.processes[update.ID]
	if !ok {
		return fmt.Errorf("node %q not found", update.ID)
	}
	if proc.exited || proc.node.State == model.NodeStopping || proc.node.State == model.NodeStopped || proc.node.State == model.NodeFailed {
		return nil
	}
	proc.node.PeerID = update.PeerID
	// A Docker peer may report before inspect has resolved its control endpoint.
	// Keep it starting until Agent can actually route publish requests to it.
	if update.State != model.NodeReady || proc.apiURL != "" || s.config.Runtime != "docker" {
		proc.node.State = update.State
	}
	if proc.node.State == model.NodeReady && proc.node.Metadata["readyAt"] == "" {
		setProcessMetadata(proc, "readyAt", time.Now().UTC().Format(time.RFC3339Nano))
	}
	proc.node.Addresses = append([]string(nil), update.Addresses...)
	proc.node.ConnectedPeers = append([]string(nil), update.ConnectedPeers...)
	proc.node.TopicPeers = update.TopicPeers
	proc.node.PeerScores = update.PeerScores
	proc.node.LastSeen = time.Now().UTC()
	proc.node.Error = update.Error
	return nil
}

func (s *Server) nodes() []model.Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]model.Node, 0, len(s.processes))
	for _, proc := range s.processes {
		result = append(result, proc.node)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func activeNodeCount(nodes []model.Node) int {
	count := 0
	for _, node := range nodes {
		if node.State != model.NodeStopping && node.State != model.NodeStopped && node.State != model.NodeFailed {
			count++
		}
	}
	return count
}

func activeNodeCountLocked(processes map[string]*process) int {
	count := 0
	for _, proc := range processes {
		if proc.node.State != model.NodeStopping && proc.node.State != model.NodeStopped && proc.node.State != model.NodeFailed {
			count++
		}
	}
	return count
}

func (s *Server) enqueueEvents(batch model.EventBatch) {
	s.eventsMu.Lock()
	for i := range batch.Events {
		batch.Events[i].AgentID = s.config.ID
	}
	s.events = append(s.events, batch.Events...)
	length := len(s.events)
	if length > 50000 {
		s.events = append([]model.TraceEvent(nil), s.events[length-50000:]...)
	}
	s.eventsMu.Unlock()
	if length >= 250 {
		select {
		case s.flushNow <- struct{}{}:
		default:
		}
	}
}

func (s *Server) flushEvents(ctx context.Context) {
	s.eventsMu.Lock()
	if len(s.events) == 0 {
		s.eventsMu.Unlock()
		return
	}
	count := len(s.events)
	if count > 5000 {
		count = 5000
	}
	batchEvents := append([]model.TraceEvent(nil), s.events[:count]...)
	s.events = append([]model.TraceEvent(nil), s.events[count:]...)
	s.eventsMu.Unlock()
	batch := model.EventBatch{AgentID: s.config.ID, Events: batchEvents}
	flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.postJSON(flushCtx, "/api/v1/events/batch", batch, nil); err != nil {
		s.eventsMu.Lock()
		s.events = append(batchEvents, s.events...)
		if len(s.events) > 50000 {
			s.events = s.events[len(s.events)-50000:]
		}
		s.eventsMu.Unlock()
		s.logger.Warn("telemetry flush failed", "events", len(batchEvents), "error", err)
	}
}

func (s *Server) postJSON(ctx context.Context, path string, input, output any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.config.ControllerURL, "/")+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
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
		return fmt.Errorf("controller returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	if output != nil {
		return json.NewDecoder(resp.Body).Decode(output)
	}
	return nil
}

func localURL(listen string) string {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		if strings.HasPrefix(listen, ":") {
			port = strings.TrimPrefix(listen, ":")
		} else {
			return "http://127.0.0.1:8090"
		}
	}
	return "http://127.0.0.1:" + port
}

func safeName(value string) string {
	return url.PathEscape(strings.ReplaceAll(value, "..", "_"))
}

func portFromAddress(address string) int {
	_, raw, err := net.SplitHostPort(address)
	if err != nil {
		return 0
	}
	port, _ := strconv.Atoi(raw)
	return port
}
