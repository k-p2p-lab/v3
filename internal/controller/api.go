package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
	"github.com/k-p2p-lab/v3/internal/webui"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func (s *Server) Run(ctx context.Context) error {
	if err := prepareDataDir(s.config.DataDir); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", s.config.Listen)
	if err != nil {
		return err
	}
	return s.serve(ctx, listener)
}

func (s *Server) serve(ctx context.Context, listener net.Listener) error {
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	server := &http.Server{
		Addr:              s.config.Listen,
		Handler:           s.Handler(runCtx),
		BaseContext:       func(net.Listener) context.Context { return runCtx },
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				s.state.markStaleAgents(agentStaleAfter)
			}
		}
	}()
	s.logger.Info("controller listening", "address", s.config.Listen)
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	var err error
	select {
	case err = <-served:
	case <-runCtx.Done():
	}
	// Close admission before waiting: no concurrent POST may add a run after
	// Wait starts, including requests already accepted by the HTTP listener.
	s.cancelMu.Lock()
	s.shuttingDown = true
	for _, cancel := range s.cancels {
		cancel()
	}
	s.cancelMu.Unlock()
	cancelRun()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	shutdownErr := server.Shutdown(shutdownCtx)
	cancelShutdown()
	if shutdownErr != nil {
		_ = server.Close()
	}
	// Scenario cancellation fences Agent creates, removes peers, and persists
	// the final experiment state. Returning sooner would let main exit while
	// those goroutines still own resources, especially after Linux SIGTERM.
	s.runs.Wait()
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return errors.Join(err, shutdownErr)
}

func (s *Server) Handler(ctx context.Context) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(s.state.metrics.registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/snapshot", s.handleSnapshot)
	mux.HandleFunc("/api/v1/agents", s.handleAgents)
	mux.HandleFunc("/api/v1/agents/register", s.handleAgentRegister)
	mux.HandleFunc("/api/v1/agents/heartbeat", s.handleAgentHeartbeat)
	mux.HandleFunc("/api/v1/nodes", s.handleNodes)
	mux.HandleFunc("/api/v1/network", s.handleNetwork)
	mux.HandleFunc("/api/v1/bootstrap", s.handleBootstrap)
	mux.HandleFunc("/api/v1/events", s.handleEvents)
	mux.HandleFunc("/api/v1/events/batch", s.handleEventBatch)
	mux.HandleFunc("/api/v1/experiments", s.handleExperiments(ctx))
	mux.HandleFunc("/api/v1/results", s.handleResults)
	mux.HandleFunc("/api/v1/experiments/", s.handleExperimentAction)
	mux.HandleFunc("/api/v1/stream", s.handleStream)
	mux.Handle("/", http.FileServer(http.FS(webui.FS())))
	return s.withMiddleware(mux)
}

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if s.config.Token != "" && r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("Authorization") != "Bearer "+s.config.Token {
				writeError(w, http.StatusUnauthorized, "valid bearer token required")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "time": time.Now().UTC()})
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.state.snapshot())
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.state.snapshot().Agents)
}

func (s *Server) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var agent model.Agent
	if err := decodeJSON(w, r, &agent); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	registered, err := s.state.registerAgent(agent)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, registered)
}

func (s *Server) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var heartbeat model.AgentHeartbeat
	if err := decodeJSON(w, r, &heartbeat); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.state.heartbeat(heartbeat); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.state.snapshot().Nodes)
}

func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	snapshot := s.state.snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"generatedAt": snapshot.GeneratedAt,
		"nodes":       snapshot.Nodes,
		"edges":       snapshot.Edges,
		"metrics":     snapshot.Metrics,
	})
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	runID := strings.TrimSpace(r.URL.Query().Get("runId"))
	if runID == "" {
		writeError(w, http.StatusBadRequest, "runId query parameter is required")
		return
	}
	snapshot := s.state.snapshot()
	type bootstrapNode struct {
		NodeID    string   `json:"nodeId"`
		PeerID    string   `json:"peerId"`
		Addresses []string `json:"addresses"`
	}
	var result []bootstrapNode
	for _, node := range snapshot.Nodes {
		if node.RunID == runID && node.Role == "boot" && node.State == model.NodeReady && node.PeerID != "" && len(node.Addresses) > 0 {
			result = append(result, bootstrapNode{NodeID: node.ID, PeerID: node.PeerID, Addresses: node.Addresses})
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.state.snapshot().Events)
}

func (s *Server) handleEventBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var batch model.EventBatch
	if err := decodeJSON(w, r, &batch); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(batch.Events) > 5000 {
		writeError(w, http.StatusRequestEntityTooLarge, "event batch cannot exceed 5000 events")
		return
	}
	if err := s.state.appendEvents(batch); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleExperiments(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, s.state.snapshot().Experiments)
		case http.MethodPost:
			// Cancel a slow upload immediately during shutdown; canceling a
			// request context alone does not unblock a server-side body Read.
			stopDeadline := context.AfterFunc(ctx, func() {
				_ = http.NewResponseController(w).SetReadDeadline(time.Now())
			})
			defer stopDeadline()
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				writeError(w, http.StatusBadRequest, "cannot read scenario")
				return
			}
			experiment, err := s.StartScenario(ctx, raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusAccepted, experiment)
		default:
			methodNotAllowed(w)
		}
	}
}

func (s *Server) handleExperimentAction(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/experiments/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	if parts[1] == "download" {
		s.handleResultDownload(w, r, parts[0])
		return
	}
	if parts[1] != "stop" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if err := s.StopScenario(parts[0]); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	updates, unsubscribe := s.state.subscribe()
	defer unsubscribe()
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	send := func() error {
		data, err := json.Marshal(s.state.snapshot())
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", data)
		flusher.Flush()
		return err
	}
	if err := send(); err != nil {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-updates:
			if err := send(); err != nil {
				return
			}
		case <-keepalive.C:
			_, _ = io.WriteString(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}
