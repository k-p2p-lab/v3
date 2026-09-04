package peer

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/k-p2p-lab/v3/internal/model"
	"github.com/k-p2p-lab/v3/internal/netem"
)

// Apply impairments before opening any P2P connections. Process mode must never
// change the shared host (or Agent container) network namespace.
func prepareNetwork(ctx context.Context, config model.PeerProcessConfig) error {
	if err := config.NodeConfig.WithDefaults().Validate(); err != nil {
		return fmt.Errorf("validate peer config before network setup: %w", err)
	}
	if !config.NodeConfig.Network.Enabled() {
		return nil
	}
	if config.Runtime != "docker" {
		return fmt.Errorf("network conditions require an isolated Docker peer")
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("network conditions require Linux Docker containers")
	}
	if _, err := os.Stat("/.dockerenv"); err != nil {
		return fmt.Errorf("refusing network changes outside a Docker container: %w", err)
	}
	if config.P2PListen != "/ip4/0.0.0.0/tcp/20000" {
		return fmt.Errorf("Docker network conditions require the fixed P2P TCP port 20000")
	}
	if err := netem.Apply(ctx, config.NodeConfig.Network, 20000); err != nil {
		return fmt.Errorf("apply peer network conditions: %w", err)
	}
	return nil
}
