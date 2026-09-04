package model

import (
	"math"
	"strings"
	"testing"
)

func intPtr(value int) *int             { return &value }
func uint64Ptr(value uint64) *uint64    { return &value }
func float64Ptr(value float64) *float64 { return &value }

func TestNodeConfigExplicitZeroOverridesArePreserved(t *testing.T) {
	base, ok := BuiltInNodeConfig("full")
	if !ok {
		t.Fatal("full built-in profile is missing")
	}

	resolved := base.Merge(NodeConfig{
		GossipSub: GossipSubConfig{
			Params: GossipSubParamsConfig{
				HistoryGossip: intPtr(0),
				GossipFactor:  float64Ptr(0),
				DOut:          intPtr(0),
			},
		},
	}).WithDefaults()

	if resolved.GossipSub.Params.HistoryGossip == nil || *resolved.GossipSub.Params.HistoryGossip != 0 {
		t.Fatalf("explicit historyGossip=0 was not preserved: %#v", resolved.GossipSub.Params.HistoryGossip)
	}
	if resolved.GossipSub.Params.GossipFactor == nil || *resolved.GossipSub.Params.GossipFactor != 0 {
		t.Fatalf("explicit gossipFactor=0 was not preserved: %#v", resolved.GossipSub.Params.GossipFactor)
	}
	if resolved.GossipSub.Params.DOut == nil || *resolved.GossipSub.Params.DOut != 0 {
		t.Fatalf("explicit dOut=0 was not preserved: %#v", resolved.GossipSub.Params.DOut)
	}
}

func TestWorkerLikePresetsInheritV2GossipBaseline(t *testing.T) {
	for _, kind := range []string{"full", "light", "publisher", "subscriber", "observer", "relay", "gossip-only", "non-gossip", "mesh-only"} {
		config, ok := BuiltInNodeConfig(kind)
		if !ok {
			t.Fatalf("missing preset %q", kind)
		}
		params := config.GossipSub.Params
		if *params.DLow != 5 || *params.DScore != 3 || *params.MaxIHaveLength != 5500 || params.HeartbeatInitialDelay != "1s" {
			t.Errorf("preset %q did not inherit v2 worker baseline: %+v", kind, params)
		}
	}
}

func TestNodeConfigRejectsUnsafeRuntimeValues(t *testing.T) {
	zero := 0
	negative := -1
	tests := []NodeConfig{
		{Libp2p: Libp2pConfig{DialTimeout: "0s"}},
		{Kademlia: KademliaConfig{RoutingTableRefreshPeriod: "0s"}},
		{Kademlia: KademliaConfig{BootstrapRetryInterval: "0s"}},
		{GossipSub: GossipSubConfig{Params: GossipSubParamsConfig{HeartbeatInterval: "0s"}}},
		{GossipSub: GossipSubConfig{PeerOutboundQueueSize: &zero}},
		{GossipSub: GossipSubConfig{ValidateThrottle: &negative}},
		{GossipSub: GossipSubConfig{SubscriptionBufferSize: &negative}},
		{Libp2p: Libp2pConfig{ConnectionManager: &ConnectionManagerConfig{LowWater: 0, HighWater: 1, GracePeriod: "-1s"}}},
	}
	for _, config := range tests {
		if err := config.Validate(); err == nil {
			t.Errorf("unsafe config was accepted: %+v", config)
		}
	}
}

func TestNonGossipSubRoutersIgnoreMeshOnlyParameters(t *testing.T) {
	zero := 0
	zeroTicks := uint64(0)
	for _, kind := range []string{"flood", "random"} {
		t.Run(kind, func(t *testing.T) {
			config, ok := BuiltInNodeConfig(kind)
			if !ok {
				t.Fatalf("missing preset %q", kind)
			}
			config.GossipSub.Params.HistoryLength = &zero
			config.GossipSub.Params.DirectConnectTicks = &zeroTicks
			config.GossipSub.Params.HeartbeatInterval = "not-a-duration"
			if err := config.Validate(); err != nil {
				t.Fatalf("%s rejected ignored GossipSub mesh parameters: %v", kind, err)
			}
		})
	}
}

func TestPubSubZeroLimitsMatchUpstreamSemantics(t *testing.T) {
	zero := 0
	config, ok := BuiltInNodeConfig("full")
	if !ok {
		t.Fatal("full preset is missing")
	}
	config.GossipSub.MaxMessageSize = &zero
	config.GossipSub.ValidateThrottle = &zero
	if err := config.Validate(); err != nil {
		t.Fatalf("upstream-supported zero limits were rejected: %v", err)
	}

	negative := -1
	config.GossipSub.MaxMessageSize = &negative
	if err := config.Validate(); err == nil {
		t.Fatal("negative maxMessageSize was accepted")
	}
}

func TestGossipSubFloatingPointParametersMustBeFinite(t *testing.T) {
	tests := []struct {
		name  string
		field string
		apply func(*GossipSubParamsConfig)
	}{
		{
			name:  "NaN gossip factor",
			field: "gossipFactor",
			apply: func(params *GossipSubParamsConfig) {
				value := math.NaN()
				params.GossipFactor = &value
			},
		},
		{
			name:  "infinite gossip factor",
			field: "gossipFactor",
			apply: func(params *GossipSubParamsConfig) {
				value := math.Inf(1)
				params.GossipFactor = &value
			},
		},
		{
			name:  "NaN slow heartbeat warning",
			field: "slowHeartbeatWarning",
			apply: func(params *GossipSubParamsConfig) {
				value := math.NaN()
				params.SlowHeartbeatWarning = &value
			},
		},
		{
			name:  "infinite slow heartbeat warning",
			field: "slowHeartbeatWarning",
			apply: func(params *GossipSubParamsConfig) {
				value := math.Inf(1)
				params.SlowHeartbeatWarning = &value
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, ok := BuiltInNodeConfig("full")
			if !ok {
				t.Fatal("full preset is missing")
			}
			test.apply(&config.GossipSub.Params)
			err := config.Validate()
			if err == nil || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("Validate() error = %v, want %s finite-value error", err, test.field)
			}
		})
	}
}

func TestPeerScoreRejectsUnsupportedAppSpecificWeight(t *testing.T) {
	score := PeerScoreConfig{SkipAtomicValidation: true, AppSpecificWeight: 1}
	config, ok := BuiltInNodeConfig("full")
	if !ok {
		t.Fatal("full preset is missing")
	}
	config.GossipSub.Score = &score
	err := config.Validate()
	if err == nil || !strings.Contains(err.Error(), "appSpecificWeight is unsupported") {
		t.Fatalf("Validate() error = %v, want unsupported appSpecificWeight error", err)
	}
}

func TestNodeConfigPreflightsPeerScore(t *testing.T) {
	invalid := []PeerScoreConfig{
		{SkipAtomicValidation: true, IPColocationFactorWhitelist: []string{"not-a-cidr"}},
		{SkipAtomicValidation: true, DecayInterval: "500ms", DecayToZero: 0.01},
		{SkipAtomicValidation: true, Thresholds: PeerScoreThresholdsConfig{SkipAtomicValidation: true, GossipThreshold: -2, PublishThreshold: -1}},
		{SkipAtomicValidation: true, Topics: map[string]TopicScoreConfig{"kpl/default": {SkipAtomicValidation: true, InvalidMessageDeliveriesWeight: -1}}},
	}
	for _, score := range invalid {
		config := NodeConfig{GossipSub: GossipSubConfig{Score: &score}}
		if err := config.Validate(); err == nil {
			t.Errorf("invalid peer score was accepted: %+v", score)
		}
	}

	valid := PeerScoreConfig{
		SkipAtomicValidation: true,
		DecayInterval:        "1s",
		DecayToZero:          0.01,
		Thresholds:           PeerScoreThresholdsConfig{SkipAtomicValidation: true},
		Topics: map[string]TopicScoreConfig{
			"kpl/default": {
				SkipAtomicValidation:           true,
				InvalidMessageDeliveriesWeight: -1,
				InvalidMessageDeliveriesDecay:  0.5,
			},
		},
	}
	if err := (NodeConfig{GossipSub: GossipSubConfig{Score: &valid}}).Validate(); err != nil {
		t.Fatalf("valid peer score was rejected: %v", err)
	}
}

func TestNodeConfigMergeAppliesCompatibilitySubscribeAndProtocolChoice(t *testing.T) {
	base, ok := BuiltInNodeConfig("full")
	if !ok {
		t.Fatal("full built-in config is missing")
	}
	doNotSubscribe := false
	merged := base.Merge(NodeConfig{
		Kademlia:  KademliaConfig{ProtocolID: "/custom/kad/1.0.0"},
		GossipSub: GossipSubConfig{Subscribe: &doNotSubscribe},
	}).WithDefaults()
	if merged.GossipSub.TopicMode != "publish" {
		t.Fatalf("subscribe:false resolved topicMode=%q, want publish", merged.GossipSub.TopicMode)
	}
	if merged.Kademlia.ProtocolID != "/custom/kad/1.0.0" || merged.Kademlia.ProtocolPrefix != "" {
		t.Fatalf("protocol override did not replace the default prefix: %+v", merged.Kademlia)
	}
	withExtension := base.Merge(NodeConfig{Kademlia: KademliaConfig{ProtocolExtension: "/private"}})
	withExtension = withExtension.Merge(NodeConfig{Kademlia: KademliaConfig{ProtocolID: "/custom/exact/1.0.0"}}).WithDefaults()
	if withExtension.Kademlia.ProtocolExtension != "" {
		t.Fatalf("exact protocol override retained inherited extension: %+v", withExtension.Kademlia)
	}

	merged = merged.Merge(NodeConfig{Kademlia: KademliaConfig{ProtocolPrefix: "/custom/prefix"}}).WithDefaults()
	if merged.Kademlia.ProtocolPrefix != "/custom/prefix" || merged.Kademlia.ProtocolID != "" {
		t.Fatalf("protocol prefix did not replace the full override: %+v", merged.Kademlia)
	}
}

func TestBuiltInNodeConfigs(t *testing.T) {
	tests := []struct {
		name        string
		kind        string
		wantType    string
		wantKadMode string
		check       func(*testing.T, NodeConfig)
	}{
		{
			name:        "boot",
			kind:        "boot",
			wantType:    "boot",
			wantKadMode: "server",
			check: func(t *testing.T, config NodeConfig) {
				t.Helper()
				if config.GossipSub.Enabled == nil || *config.GossipSub.Enabled {
					t.Fatal("boot must default to DHT-only; GossipSub should be explicitly enabled when wanted")
				}
				if config.GossipSub.Params.DScore == nil || *config.GossipSub.Params.DScore != 3 {
					t.Fatalf("boot dScore = %#v, want 3", config.GossipSub.Params.DScore)
				}
			},
		},
		{
			name:        "full",
			kind:        "full",
			wantType:    "full",
			wantKadMode: "server",
			check: func(t *testing.T, config NodeConfig) {
				t.Helper()
				if config.Libp2p.ConnectionLimit == nil || *config.Libp2p.ConnectionLimit != 55 {
					t.Fatalf("full connection limit = %#v, want v2 hard limit 55", config.Libp2p.ConnectionLimit)
				}
			},
		},
		{
			name:        "worker alias",
			kind:        "worker",
			wantType:    "full",
			wantKadMode: "server",
		},
		{
			name:        "random",
			kind:        "random",
			wantType:    "random",
			wantKadMode: "client",
			check: func(t *testing.T, config NodeConfig) {
				t.Helper()
				if config.GossipSub.RandomDegree == nil || *config.GossipSub.RandomDegree != 6 {
					t.Fatalf("random degree = %#v, want 6", config.GossipSub.RandomDegree)
				}
				if config.GossipSub.RandomNetworkSize == nil || *config.GossipSub.RandomNetworkSize != 100 {
					t.Fatalf("random network size = %#v, want 100", config.GossipSub.RandomNetworkSize)
				}
			},
		},
		{
			name:        "non-gossip",
			kind:        "non-gossip",
			wantType:    "non-gossip",
			wantKadMode: "server",
			check: func(t *testing.T, config NodeConfig) {
				t.Helper()
				if config.GossipSub.Params.HistoryGossip == nil || *config.GossipSub.Params.HistoryGossip != 0 {
					t.Fatalf("non-gossip historyGossip = %#v, want 0", config.GossipSub.Params.HistoryGossip)
				}
				if config.GossipSub.Params.GossipFactor == nil || *config.GossipSub.Params.GossipFactor != 0 {
					t.Fatalf("non-gossip gossipFactor = %#v, want 0", config.GossipSub.Params.GossipFactor)
				}
			},
		},
		{
			name:        "mesh-only alias",
			kind:        "mesh-only",
			wantType:    "mesh-only",
			wantKadMode: "server",
			check: func(t *testing.T, config NodeConfig) {
				t.Helper()
				if config.GossipSub.Params.HistoryGossip == nil || *config.GossipSub.Params.HistoryGossip != 0 {
					t.Fatalf("mesh-only historyGossip = %#v, want 0", config.GossipSub.Params.HistoryGossip)
				}
				if config.GossipSub.Params.GossipFactor == nil || *config.GossipSub.Params.GossipFactor != 0 {
					t.Fatalf("mesh-only gossipFactor = %#v, want 0", config.GossipSub.Params.GossipFactor)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, ok := BuiltInNodeConfig(test.kind)
			if !ok {
				t.Fatalf("built-in profile %q is missing", test.kind)
			}
			if config.Type != test.wantType {
				t.Fatalf("type = %q, want %q", config.Type, test.wantType)
			}
			if config.Kademlia.Mode != test.wantKadMode {
				t.Fatalf("kademlia mode = %q, want %q", config.Kademlia.Mode, test.wantKadMode)
			}
			if err := config.Validate(); err != nil {
				t.Fatalf("built-in profile %q is invalid: %v", test.kind, err)
			}
			if test.check != nil {
				test.check(t, config)
			}
		})
	}
}

func TestNodeConfigMergeOverlaysNestedValues(t *testing.T) {
	base, ok := BuiltInNodeConfig("full")
	if !ok {
		t.Fatal("full built-in profile is missing")
	}

	resolved := base.Merge(NodeConfig{
		Libp2p: Libp2pConfig{
			Relay: boolPointer(false),
		},
		Kademlia: KademliaConfig{
			Mode:       "client",
			BucketSize: intPtr(32),
		},
		GossipSub: GossipSubConfig{
			Params: GossipSubParamsConfig{
				D:                 intPtr(8),
				DHigh:             intPtr(16),
				HeartbeatInterval: "250ms",
			},
		},
	}).WithDefaults()

	if resolved.Libp2p.Relay == nil || *resolved.Libp2p.Relay {
		t.Fatalf("nested libp2p.relay=false override was not preserved: %#v", resolved.Libp2p.Relay)
	}
	if resolved.Kademlia.Mode != "client" {
		t.Fatalf("nested kademlia.mode = %q, want client", resolved.Kademlia.Mode)
	}
	if resolved.Kademlia.BucketSize == nil || *resolved.Kademlia.BucketSize != 32 {
		t.Fatalf("nested kademlia.bucketSize = %#v, want 32", resolved.Kademlia.BucketSize)
	}
	if resolved.GossipSub.Params.D == nil || *resolved.GossipSub.Params.D != 8 {
		t.Fatalf("nested gossipsub.params.d = %#v, want 8", resolved.GossipSub.Params.D)
	}
	if resolved.GossipSub.Params.DHigh == nil || *resolved.GossipSub.Params.DHigh != 16 {
		t.Fatalf("nested gossipsub.params.dHigh = %#v, want 16", resolved.GossipSub.Params.DHigh)
	}
	if resolved.GossipSub.Params.HeartbeatInterval != "250ms" {
		t.Fatalf("nested heartbeatInterval = %q, want 250ms", resolved.GossipSub.Params.HeartbeatInterval)
	}
	if err := resolved.Validate(); err != nil {
		t.Fatalf("resolved overlay is invalid: %v", err)
	}
}

func TestNodeConfigValidationRejectsUnsafeGossipSubParams(t *testing.T) {
	tests := []struct {
		name        string
		config      NodeConfig
		wantErrPart string
	}{
		{
			name: "randomsub requires degree",
			config: NodeConfig{GossipSub: GossipSubConfig{
				Router:            "randomsub",
				RandomDegree:      intPtr(0),
				RandomNetworkSize: intPtr(100),
			}},
			wantErrPart: "randomDegree",
		},
		{
			name: "randomsub requires network size",
			config: NodeConfig{GossipSub: GossipSubConfig{
				Router:            "randomsub",
				RandomDegree:      intPtr(6),
				RandomNetworkSize: intPtr(0),
			}},
			wantErrPart: "randomNetworkSize",
		},
		{
			name: "zero history length",
			config: NodeConfig{GossipSub: GossipSubConfig{Params: GossipSubParamsConfig{
				HistoryLength: intPtr(0),
			}}},
			wantErrPart: "history",
		},
		{
			name: "history gossip exceeds length",
			config: NodeConfig{GossipSub: GossipSubConfig{Params: GossipSubParamsConfig{
				HistoryLength: intPtr(2),
				HistoryGossip: intPtr(3),
			}}},
			wantErrPart: "history",
		},
		{
			name: "zero direct connect ticks",
			config: NodeConfig{GossipSub: GossipSubConfig{Params: GossipSubParamsConfig{
				DirectConnectTicks: uint64Ptr(0),
			}}},
			wantErrPart: "directConnectTicks",
		},
		{
			name: "zero opportunistic graft ticks",
			config: NodeConfig{GossipSub: GossipSubConfig{Params: GossipSubParamsConfig{
				OpportunisticGraftTicks: uint64Ptr(0),
			}}},
			wantErrPart: "opportunisticGraftTicks",
		},
		{
			name: "dOut is not below dLow",
			config: NodeConfig{GossipSub: GossipSubConfig{Params: GossipSubParamsConfig{
				D:     intPtr(8),
				DLow:  intPtr(4),
				DHigh: intPtr(12),
				DOut:  intPtr(4),
			}}},
			wantErrPart: "dOut",
		},
		{
			name: "dOut exceeds half of d",
			config: NodeConfig{GossipSub: GossipSubConfig{Params: GossipSubParamsConfig{
				D:     intPtr(4),
				DLow:  intPtr(4),
				DHigh: intPtr(12),
				DOut:  intPtr(3),
			}}},
			wantErrPart: "dOut",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.config.Validate()
			if err == nil {
				t.Fatal("Validate() succeeded, want error")
			}
			if !strings.Contains(err.Error(), test.wantErrPart) {
				t.Fatalf("Validate() error = %q, want it to contain %q", err, test.wantErrPart)
			}
		})
	}
}
