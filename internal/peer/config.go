package peer

import (
	"fmt"
	"net"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	connmgr "github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
)

func hostOptions(config model.NodeConfig) ([]libp2p.Option, error) {
	// Keep the v2 transport/security stack explicit: libp2p defaults also
	// enable TLS and additional transports, changing connection startup costs.
	options := []libp2p.Option{
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Muxer(yamux.ID, yamux.DefaultTransport),
	}
	if config.Libp2p.UserAgent != "" {
		options = append(options, libp2p.UserAgent(config.Libp2p.UserAgent))
	}
	if config.Libp2p.DialTimeout != "" {
		dialTimeout, err := time.ParseDuration(config.Libp2p.DialTimeout)
		if err != nil {
			return nil, fmt.Errorf("parse libp2p dial timeout: %w", err)
		}
		options = append(options, libp2p.WithDialTimeout(dialTimeout))
	}
	if config.Libp2p.NATPortMap != nil && *config.Libp2p.NATPortMap {
		options = append(options, libp2p.NATPortMap())
	}
	if config.Libp2p.Relay != nil {
		if *config.Libp2p.Relay {
			options = append(options, libp2p.EnableRelay())
		} else {
			options = append(options, libp2p.DisableRelay())
		}
	}
	if config.Libp2p.RelayService != nil && *config.Libp2p.RelayService {
		options = append(options, libp2p.EnableRelayService())
	}

	cmConfig := config.Libp2p.ConnectionManager
	if cmConfig != nil {
		var managerOptions []connmgr.Option
		if cmConfig.GracePeriod != "" {
			grace, err := time.ParseDuration(cmConfig.GracePeriod)
			if err != nil {
				return nil, fmt.Errorf("parse connection manager grace period: %w", err)
			}
			managerOptions = append(managerOptions, connmgr.WithGracePeriod(grace))
		}
		manager, err := connmgr.NewConnManager(cmConfig.LowWater, cmConfig.HighWater, managerOptions...)
		if err != nil {
			return nil, fmt.Errorf("create connection manager: %w", err)
		}
		options = append(options, libp2p.ConnectionManager(manager))
	}
	if config.Libp2p.ConnectionLimit != nil && *config.Libp2p.ConnectionLimit > 0 {
		limits := rcmgr.PartialLimitConfig{
			System: rcmgr.ResourceLimits{Conns: rcmgr.LimitVal(*config.Libp2p.ConnectionLimit)},
		}
		manager, err := rcmgr.NewResourceManager(rcmgr.NewFixedLimiter(limits.Build(rcmgr.DefaultLimits.AutoScale())))
		if err != nil {
			return nil, fmt.Errorf("create resource manager: %w", err)
		}
		options = append(options, libp2p.ResourceManager(manager))
	}
	return options, nil
}

func dhtOptions(config model.KademliaConfig) ([]dht.Option, error) {
	mode := dht.ModeServer
	switch config.Mode {
	case "auto":
		mode = dht.ModeAuto
	case "auto-server":
		mode = dht.ModeAutoServer
	case "client":
		mode = dht.ModeClient
	case "server":
		mode = dht.ModeServer
	default:
		return nil, fmt.Errorf("invalid kademlia mode %q", config.Mode)
	}
	options := []dht.Option{dht.Mode(mode)}
	if config.ProtocolID != "" {
		options = append(options, dht.V1ProtocolOverride(protocol.ID(config.ProtocolID)))
	} else if config.ProtocolPrefix != "" {
		options = append(options, dht.ProtocolPrefix(protocol.ID(config.ProtocolPrefix)))
	}
	if config.ProtocolExtension != "" {
		options = append(options, dht.ProtocolExtension(protocol.ID(config.ProtocolExtension)))
	}
	if config.BucketSize != nil {
		options = append(options, dht.BucketSize(*config.BucketSize))
	}
	if config.Concurrency != nil {
		options = append(options, dht.Concurrency(*config.Concurrency))
	}
	if config.Resiliency != nil {
		options = append(options, dht.Resiliency(*config.Resiliency))
	}
	if config.LookupCheckConcurrency != nil {
		options = append(options, dht.LookupCheckConcurrency(*config.LookupCheckConcurrency))
	}
	durations := []struct {
		name  string
		value string
		apply func(time.Duration) dht.Option
	}{
		{"routingTableLatencyTolerance", config.RoutingTableLatencyTolerance, dht.RoutingTableLatencyTolerance},
		{"routingTableRefreshPeriod", config.RoutingTableRefreshPeriod, dht.RoutingTableRefreshPeriod},
		{"routingTableRefreshTimeout", config.RoutingTableRefreshTimeout, dht.RoutingTableRefreshQueryTimeout},
		{"maxRecordAge", config.MaxRecordAge, dht.MaxRecordAge},
	}
	for _, item := range durations {
		if item.value == "" {
			continue
		}
		value, err := time.ParseDuration(item.value)
		if err != nil {
			return nil, fmt.Errorf("parse kademlia %s: %w", item.name, err)
		}
		options = append(options, item.apply(value))
	}
	if config.DisableAutoRefresh != nil && *config.DisableAutoRefresh {
		options = append(options, dht.DisableAutoRefresh())
	}
	if config.DisableProviders != nil && *config.DisableProviders {
		options = append(options, dht.DisableProviders())
	}
	if config.DisableValues != nil && *config.DisableValues {
		options = append(options, dht.DisableValues())
	}
	if config.OptimisticProvide != nil && *config.OptimisticProvide {
		options = append(options, dht.EnableOptimisticProvide())
	}
	if config.OptimisticProvideJobsPoolSize != nil {
		options = append(options, dht.OptimisticProvideJobsPoolSize(*config.OptimisticProvideJobsPoolSize))
	}
	return options, nil
}

func gossipSubOptions(config model.GossipSubConfig, tracer pubsub.EventTracer) ([]pubsub.Option, error) {
	options := []pubsub.Option{pubsub.WithEventTracer(tracer)}
	if config.PeerOutboundQueueSize != nil {
		options = append(options, pubsub.WithPeerOutboundQueueSize(*config.PeerOutboundQueueSize))
	}
	if config.MaxMessageSize != nil {
		options = append(options, pubsub.WithMaxMessageSize(*config.MaxMessageSize))
	}
	if config.ValidateQueueSize != nil {
		options = append(options, pubsub.WithValidateQueueSize(*config.ValidateQueueSize))
	}
	if config.ValidateThrottle != nil {
		options = append(options, pubsub.WithValidateThrottle(*config.ValidateThrottle))
	}
	if config.ValidateWorkers != nil {
		options = append(options, pubsub.WithValidateWorkers(*config.ValidateWorkers))
	}
	if config.SeenMessagesTTL != "" {
		ttl, err := time.ParseDuration(config.SeenMessagesTTL)
		if err != nil {
			return nil, fmt.Errorf("parse seen messages TTL: %w", err)
		}
		options = append(options, pubsub.WithSeenMessagesTTL(ttl))
	}
	switch config.SignaturePolicy {
	case "strict-sign":
		options = append(options, pubsub.WithMessageSignaturePolicy(pubsub.StrictSign))
	case "strict-no-sign":
		options = append(options, pubsub.WithMessageSignaturePolicy(pubsub.StrictNoSign))
	}
	if config.Router != "gossipsub" {
		return options, nil
	}
	if config.FloodPublish != nil {
		options = append(options, pubsub.WithFloodPublish(*config.FloodPublish))
	}
	if config.PeerExchange != nil {
		options = append(options, pubsub.WithPeerExchange(*config.PeerExchange))
	}
	params := pubsub.DefaultGossipSubParams()
	p := config.Params
	params.D = *p.D
	params.Dlo = *p.DLow
	params.Dhi = *p.DHigh
	params.Dscore = *p.DScore
	params.Dout = *p.DOut
	params.Dlazy = *p.DLazy
	params.HistoryLength = *p.HistoryLength
	params.HistoryGossip = *p.HistoryGossip
	params.GossipFactor = *p.GossipFactor
	params.GossipRetransmission = *p.GossipRetransmission
	params.SlowHeartbeatWarning = *p.SlowHeartbeatWarning
	params.PrunePeers = *p.PrunePeers
	params.Connectors = *p.Connectors
	params.MaxPendingConnections = *p.MaxPendingConnections
	params.DirectConnectTicks = *p.DirectConnectTicks
	params.OpportunisticGraftTicks = *p.OpportunisticGraftTicks
	params.OpportunisticGraftPeers = *p.OpportunisticGraftPeers
	params.MaxIHaveLength = *p.MaxIHaveLength
	params.MaxIHaveMessages = *p.MaxIHaveMessages
	params.MaxIDontWantLength = *p.MaxIDontWantLength
	params.MaxIDontWantMessages = *p.MaxIDontWantMessages
	params.IDontWantMessageThreshold = *p.IDontWantMessageThreshold
	params.IDontWantMessageTTL = *p.IDontWantMessageTTL

	durations := []struct {
		name   string
		value  string
		target *time.Duration
	}{
		{"heartbeatInitialDelay", p.HeartbeatInitialDelay, &params.HeartbeatInitialDelay},
		{"heartbeatInterval", p.HeartbeatInterval, &params.HeartbeatInterval},
		{"fanoutTTL", p.FanoutTTL, &params.FanoutTTL},
		{"pruneBackoff", p.PruneBackoff, &params.PruneBackoff},
		{"unsubscribeBackoff", p.UnsubscribeBackoff, &params.UnsubscribeBackoff},
		{"connectionTimeout", p.ConnectionTimeout, &params.ConnectionTimeout},
		{"directConnectInitialDelay", p.DirectConnectInitialDelay, &params.DirectConnectInitialDelay},
		{"graftFloodThreshold", p.GraftFloodThreshold, &params.GraftFloodThreshold},
		{"iWantFollowupTime", p.IWantFollowupTime, &params.IWantFollowupTime},
	}
	for _, item := range durations {
		value, err := time.ParseDuration(item.value)
		if err != nil {
			return nil, fmt.Errorf("parse gossipsub %s: %w", item.name, err)
		}
		*item.target = value
	}

	options = append(options, pubsub.WithGossipSubParams(params))
	if config.Score != nil {
		score, thresholds, err := peerScoreOptions(*config.Score)
		if err != nil {
			return nil, err
		}
		options = append(options, pubsub.WithPeerScore(score, thresholds))
	}
	return options, nil
}

func peerScoreOptions(config model.PeerScoreConfig) (*pubsub.PeerScoreParams, *pubsub.PeerScoreThresholds, error) {
	if config.AppSpecificWeight != 0 {
		return nil, nil, fmt.Errorf("peer score appSpecificWeight is unsupported because no application-specific score function is configured")
	}
	whitelist := make([]*net.IPNet, 0, len(config.IPColocationFactorWhitelist))
	for _, raw := range config.IPColocationFactorWhitelist {
		_, network, err := net.ParseCIDR(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("parse peer score whitelist %q: %w", raw, err)
		}
		whitelist = append(whitelist, network)
	}
	decayInterval, err := parseOptionalDuration(config.DecayInterval)
	if err != nil {
		return nil, nil, fmt.Errorf("parse peer score decayInterval: %w", err)
	}
	retainScore, err := parseOptionalDuration(config.RetainScore)
	if err != nil {
		return nil, nil, fmt.Errorf("parse peer score retainScore: %w", err)
	}
	seenTTL, err := parseOptionalDuration(config.SeenMessageTTL)
	if err != nil {
		return nil, nil, fmt.Errorf("parse peer score seenMessageTTL: %w", err)
	}
	topics := make(map[string]*pubsub.TopicScoreParams, len(config.Topics))
	for topic, input := range config.Topics {
		timeInMeshQuantum, err := parseOptionalDuration(input.TimeInMeshQuantum)
		if err != nil {
			return nil, nil, fmt.Errorf("parse score topic %s timeInMeshQuantum: %w", topic, err)
		}
		activation, err := parseOptionalDuration(input.MeshMessageDeliveriesActivation)
		if err != nil {
			return nil, nil, fmt.Errorf("parse score topic %s meshMessageDeliveriesActivation: %w", topic, err)
		}
		window, err := parseOptionalDuration(input.MeshMessageDeliveriesWindow)
		if err != nil {
			return nil, nil, fmt.Errorf("parse score topic %s meshMessageDeliveriesWindow: %w", topic, err)
		}
		topics[topic] = &pubsub.TopicScoreParams{
			SkipAtomicValidation: input.SkipAtomicValidation,
			TopicWeight:          input.TopicWeight,
			TimeInMeshWeight:     input.TimeInMeshWeight, TimeInMeshQuantum: timeInMeshQuantum, TimeInMeshCap: input.TimeInMeshCap,
			FirstMessageDeliveriesWeight: input.FirstMessageDeliveriesWeight, FirstMessageDeliveriesDecay: input.FirstMessageDeliveriesDecay, FirstMessageDeliveriesCap: input.FirstMessageDeliveriesCap,
			MeshMessageDeliveriesWeight: input.MeshMessageDeliveriesWeight, MeshMessageDeliveriesDecay: input.MeshMessageDeliveriesDecay,
			MeshMessageDeliveriesThreshold: input.MeshMessageDeliveriesThreshold, MeshMessageDeliveriesCap: input.MeshMessageDeliveriesCap,
			MeshMessageDeliveriesActivation: activation, MeshMessageDeliveriesWindow: window,
			MeshFailurePenaltyWeight: input.MeshFailurePenaltyWeight, MeshFailurePenaltyDecay: input.MeshFailurePenaltyDecay,
			InvalidMessageDeliveriesWeight: input.InvalidMessageDeliveriesWeight, InvalidMessageDeliveriesDecay: input.InvalidMessageDeliveriesDecay,
		}
	}
	score := &pubsub.PeerScoreParams{
		SkipAtomicValidation:        config.SkipAtomicValidation,
		Topics:                      topics,
		TopicScoreCap:               config.TopicScoreCap,
		AppSpecificScore:            func(peer.ID) float64 { return 0 },
		AppSpecificWeight:           config.AppSpecificWeight,
		IPColocationFactorWeight:    config.IPColocationFactorWeight,
		IPColocationFactorThreshold: config.IPColocationFactorThreshold,
		IPColocationFactorWhitelist: whitelist,
		BehaviourPenaltyWeight:      config.BehaviourPenaltyWeight,
		BehaviourPenaltyThreshold:   config.BehaviourPenaltyThreshold,
		BehaviourPenaltyDecay:       config.BehaviourPenaltyDecay,
		DecayInterval:               decayInterval,
		DecayToZero:                 config.DecayToZero,
		RetainScore:                 retainScore,
		SeenMsgTTL:                  seenTTL,
	}
	t := config.Thresholds
	thresholds := &pubsub.PeerScoreThresholds{
		SkipAtomicValidation:        t.SkipAtomicValidation,
		GossipThreshold:             t.GossipThreshold,
		PublishThreshold:            t.PublishThreshold,
		GraylistThreshold:           t.GraylistThreshold,
		AcceptPXThreshold:           t.AcceptPXThreshold,
		OpportunisticGraftThreshold: t.OpportunisticGraftThreshold,
	}
	return score, thresholds, nil
}

func parseOptionalDuration(value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	return time.ParseDuration(value)
}
