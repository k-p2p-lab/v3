package peer

import (
	"context"
	"fmt"
	"time"
)

func (s *Server) localPublication() bool {
	config := s.config.NodeConfig.GossipSub.Publish
	return config != nil && config.Local != nil && *config.Local
}

// Readiness is a minimum remote topic-peer count, checked before payload creation
// so setup delay never enters the envelope propagation clock. Mesh convergence
// can still require an explicit scenario settling phase.
func (s *Server) waitPublishReady(ctx context.Context, topic string) error {
	config := s.config.NodeConfig.GossipSub.Publish
	if config == nil || config.ReadinessMinPeers == nil || s.localPublication() {
		return ctx.Err()
	}
	if config.ReadinessTimeout != "" {
		timeout, err := time.ParseDuration(config.ReadinessTimeout)
		if err != nil {
			return err
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("topic %s readiness: %w", topic, err)
		}
		if s.pubsub != nil && len(s.pubsub.ListPeers(topic)) >= *config.ReadinessMinPeers {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("topic %s readiness: %w", topic, ctx.Err())
		case <-ticker.C:
		}
	}
}
