package peer

import (
	"context"
	"fmt"
	"strings"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/discovery"
	corepeer "github.com/libp2p/go-libp2p/core/peer"
	routingdiscovery "github.com/libp2p/go-libp2p/p2p/discovery/routing"
)

// With no explicit block, KPL keeps its existing budgeted Controller discovery.
// An explicit block selects libp2p's discovery pipeline, or disables discovery.
func (s *Server) gossipDiscoveryOptions() ([]pubsub.Option, error) {
	config := s.config.NodeConfig.GossipSub.Discovery
	if config == nil || config.Mode == "none" {
		return nil, nil
	}
	var backend discovery.Discovery
	switch config.Mode {
	case "", "controller":
		if strings.TrimSpace(s.config.ControllerURL) == "" {
			return nil, fmt.Errorf("controller discovery requires controller URL")
		}
		backend = controllerPubSubDiscovery{server: s}
	case "routing":
		if s.dht == nil {
			return nil, fmt.Errorf("routing discovery requires enabled Kademlia")
		}
		backend = runScopedDiscovery{Discovery: routingdiscovery.NewRoutingDiscovery(s.dht), runID: s.config.Node.RunID}
	default:
		return nil, fmt.Errorf("unknown discovery mode %q", config.Mode)
	}
	options, err := gossipDiscoverOptions(*config)
	if err != nil {
		return nil, err
	}
	return []pubsub.Option{pubsub.WithDiscovery(backend, options...)}, nil
}

type runScopedDiscovery struct {
	discovery.Discovery
	runID string
}

func (d runScopedDiscovery) Advertise(ctx context.Context, ns string, opts ...discovery.Option) (time.Duration, error) {
	return d.Discovery.Advertise(ctx, "kpl/"+d.runID+"/"+ns, opts...)
}

func (d runScopedDiscovery) FindPeers(ctx context.Context, ns string, opts ...discovery.Option) (<-chan corepeer.AddrInfo, error) {
	return d.Discovery.FindPeers(ctx, "kpl/"+d.runID+"/"+ns, opts...)
}

type controllerPubSubDiscovery struct{ server *Server }

func (d controllerPubSubDiscovery) Advertise(ctx context.Context, ns string, opts ...discovery.Option) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	options := discovery.Options{Ttl: time.Minute}
	if err := options.Apply(opts...); err != nil {
		return 0, err
	}
	if options.Ttl <= 0 {
		options.Ttl = time.Minute
	}
	// The Controller already advertises ready peers from their topic config.
	// TTL controls this callback's renewal; liveness remains Agent heartbeat based.
	return options.Ttl, nil
}

func (d controllerPubSubDiscovery) FindPeers(ctx context.Context, ns string, opts ...discovery.Option) (<-chan corepeer.AddrInfo, error) {
	options := discovery.Options{}
	if err := options.Apply(opts...); err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, discoveryRequestLimit)
	defer cancel()
	topic := strings.TrimPrefix(ns, "floodsub:")
	nodes, err := d.server.fetchDiscoveryPeers(requestCtx, topic)
	if err != nil {
		return nil, err
	}
	limit := options.Limit
	if limit <= 0 {
		limit = len(nodes)
	}
	nodes = selectDiscoveryPeers(d.server.config.Node.ID, topic, nodes, limit)
	result := make(chan corepeer.AddrInfo, len(nodes))
	for _, node := range nodes {
		if err := ctx.Err(); err != nil {
			close(result)
			return nil, err
		}
		info, err := addrInfo(node)
		if err == nil && info.ID != d.server.host.ID() {
			result <- info
		}
	}
	close(result)
	return result, nil
}
