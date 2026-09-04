package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
)

// A Docker create runs outside Server.mu: peers report status during startup,
// and stop-all must be able to fence/cancel a create while the daemon is busy.
func (s *Server) runDockerProcess(ctx context.Context, proc *process, config []byte) {
	s.mu.RLock()
	node := proc.node
	s.mu.RUnlock()
	createCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	id, apiURL, err := s.docker.create(createCtx, node, config)
	cancel()
	if err != nil {
		var cleanupErr error
		if errors.Is(err, errDockerCleanup) {
			cleanupErr = err
			s.mu.Lock()
			proc.containerID = id
			s.mu.Unlock()
		}
		s.finishProcess(node.ID, proc, fmt.Errorf("create peer container: %w", err), cleanupErr)
		return
	}
	s.mu.Lock()
	proc.containerID = id
	proc.apiURL = apiURL
	metadata := make(map[string]string, len(proc.node.Metadata)+1)
	for key, value := range proc.node.Metadata {
		metadata[key] = value
	}
	metadata["containerId"] = id
	proc.node.Metadata = metadata
	s.mu.Unlock()
	err = s.docker.wait(ctx, id)
	// Killing the Docker CLI does not kill the container. Always ask the daemon
	// to remove this exact container using an independent cleanup context.
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
	cleanupErr := s.docker.remove(cleanupCtx, id)
	cleanupCancel()
	if cleanupErr != nil {
		cleanupErr = fmt.Errorf("remove peer container %s: %w", id, cleanupErr)
		s.logger.Error("peer container cleanup failed", "node", node.ID, "container", id, "error", cleanupErr)
	}
	s.finishProcess(node.ID, proc, err, cleanupErr)
}

func (s *Server) beginShutdown() {
	s.mu.Lock()
	s.shuttingDown = true
	s.mu.Unlock()
	s.stopAll()
}

// Caller holds Server.mu. Preserve the target of a failed rm so a subsequent
// stop request can recover after a temporary Docker daemon outage.
func (s *Server) retryContainerCleanupLocked(proc *process) {
	proc.exited = false
	proc.node.State = model.NodeStopping
	proc.done = make(chan struct{})
	id, nodeID := proc.containerID, proc.node.ID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := s.docker.remove(ctx, id)
		s.finishProcess(nodeID, proc, nil, err)
	}()
}

func (s *Server) waitRunContainers(ctx context.Context, runID string, generation uint64) error {
	s.mu.RLock()
	type pendingCleanup struct {
		proc *process
		done <-chan struct{}
	}
	var pending []pendingCleanup
	for _, proc := range s.processes {
		if proc.node.RunID == runID && proc.node.Generation <= generation {
			pending = append(pending, pendingCleanup{proc: proc, done: proc.done})
		}
	}
	s.mu.RUnlock()
	var failures []error
	for _, item := range pending {
		if item.done != nil {
			select {
			case <-item.done:
			case <-ctx.Done():
				return fmt.Errorf("wait for peer container cleanup: %w", ctx.Err())
			}
		}
		s.mu.RLock()
		if item.proc.cleanupErr != nil {
			failures = append(failures, item.proc.cleanupErr)
		}
		s.mu.RUnlock()
	}
	return errors.Join(failures...)
}

func (s *Server) waitStopped(timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	s.mu.RLock()
	done := make([]<-chan struct{}, 0, len(s.processes))
	for _, proc := range s.processes {
		if proc.done != nil {
			done = append(done, proc.done)
		}
	}
	s.mu.RUnlock()
	for _, ch := range done {
		select {
		case <-ch:
		case <-ctx.Done():
			s.logger.Warn("timed out waiting for peer cleanup")
			return
		}
	}
}
