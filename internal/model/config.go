package model

import (
	"fmt"
	"math"
	"net"
	"reflect"
	"strings"
	"time"
)

// NodeConfig is the complete, per-node protocol configuration. The flat fields
// are retained for v3 preview scenario compatibility; new scenarios should use
// the nested libp2p, kademlia, and gossipsub sections.
type NodeConfig struct {
	Type         string          `json:"type,omitempty" yaml:"type,omitempty"`
	Topics       []string        `json:"topics,omitempty" yaml:"topics,omitempty"`
	D            int             `json:"d,omitempty" yaml:"d,omitempty"`
	DLow         int             `json:"dLow,omitempty" yaml:"dLow,omitempty"`
	DHigh        int             `json:"dHigh,omitempty" yaml:"dHigh,omitempty"`
	DOut         int             `json:"dOut,omitempty" yaml:"dOut,omitempty"`
	DLazy        int             `json:"dLazy,omitempty" yaml:"dLazy,omitempty"`
	Heartbeat    string          `json:"heartbeat,omitempty" yaml:"heartbeat,omitempty"`
	ConnectLimit int             `json:"connectLimit,omitempty" yaml:"connectLimit,omitempty"`
	Libp2p       Libp2pConfig    `json:"libp2p,omitempty" yaml:"libp2p,omitempty"`
	Kademlia     KademliaConfig  `json:"kademlia,omitempty" yaml:"kademlia,omitempty"`
	GossipSub    GossipSubConfig `json:"gossipsub,omitempty" yaml:"gossipsub,omitempty"`
	Network      NetworkConfig   `json:"network,omitempty" yaml:"network,omitempty"`
}

type Libp2pConfig struct {
	UserAgent         string                   `json:"userAgent,omitempty" yaml:"userAgent,omitempty"`
	NATPortMap        *bool                    `json:"natPortMap,omitempty" yaml:"natPortMap,omitempty"`
	Relay             *bool                    `json:"relay,omitempty" yaml:"relay,omitempty"`
	RelayService      *bool                    `json:"relayService,omitempty" yaml:"relayService,omitempty"`
	ConnectionLimit   *int                     `json:"connectionLimit,omitempty" yaml:"connectionLimit,omitempty"`
	ConnectionManager *ConnectionManagerConfig `json:"connectionManager,omitempty" yaml:"connectionManager,omitempty"`
	DialTimeout       string                   `json:"dialTimeout,omitempty" yaml:"dialTimeout,omitempty"`
}

type ConnectionManagerConfig struct {
	LowWater    int    `json:"lowWater" yaml:"lowWater"`
	HighWater   int    `json:"highWater" yaml:"highWater"`
	GracePeriod string `json:"gracePeriod" yaml:"gracePeriod"`
}

type KademliaConfig struct {
	Enabled                       *bool  `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Mode                          string `json:"mode,omitempty" yaml:"mode,omitempty"`
	ProtocolPrefix                string `json:"protocolPrefix,omitempty" yaml:"protocolPrefix,omitempty"`
	ProtocolID                    string `json:"protocolId,omitempty" yaml:"protocolId,omitempty"`
	ProtocolExtension             string `json:"protocolExtension,omitempty" yaml:"protocolExtension,omitempty"`
	BucketSize                    *int   `json:"bucketSize,omitempty" yaml:"bucketSize,omitempty"`
	Concurrency                   *int   `json:"concurrency,omitempty" yaml:"concurrency,omitempty"`
	Resiliency                    *int   `json:"resiliency,omitempty" yaml:"resiliency,omitempty"`
	LookupCheckConcurrency        *int   `json:"lookupCheckConcurrency,omitempty" yaml:"lookupCheckConcurrency,omitempty"`
	RoutingTableLatencyTolerance  string `json:"routingTableLatencyTolerance,omitempty" yaml:"routingTableLatencyTolerance,omitempty"`
	RoutingTableRefreshPeriod     string `json:"routingTableRefreshPeriod,omitempty" yaml:"routingTableRefreshPeriod,omitempty"`
	RoutingTableRefreshTimeout    string `json:"routingTableRefreshTimeout,omitempty" yaml:"routingTableRefreshTimeout,omitempty"`
	MaxRecordAge                  string `json:"maxRecordAge,omitempty" yaml:"maxRecordAge,omitempty"`
	DisableAutoRefresh            *bool  `json:"disableAutoRefresh,omitempty" yaml:"disableAutoRefresh,omitempty"`
	DisableProviders              *bool  `json:"disableProviders,omitempty" yaml:"disableProviders,omitempty"`
	DisableValues                 *bool  `json:"disableValues,omitempty" yaml:"disableValues,omitempty"`
	OptimisticProvide             *bool  `json:"optimisticProvide,omitempty" yaml:"optimisticProvide,omitempty"`
	OptimisticProvideJobsPoolSize *int   `json:"optimisticProvideJobsPoolSize,omitempty" yaml:"optimisticProvideJobsPoolSize,omitempty"`
	BootstrapTimeout              string `json:"bootstrapTimeout,omitempty" yaml:"bootstrapTimeout,omitempty"`
	BootstrapRetryInterval        string `json:"bootstrapRetryInterval,omitempty" yaml:"bootstrapRetryInterval,omitempty"`
}

type GossipSubConfig struct {
	Enabled                *bool                 `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Router                 string                `json:"router,omitempty" yaml:"router,omitempty"`
	TopicMode              string                `json:"topicMode,omitempty" yaml:"topicMode,omitempty"`
	RandomDegree           *int                  `json:"randomDegree,omitempty" yaml:"randomDegree,omitempty"`
	RandomNetworkSize      *int                  `json:"randomNetworkSize,omitempty" yaml:"randomNetworkSize,omitempty"`
	Subscribe              *bool                 `json:"subscribe,omitempty" yaml:"subscribe,omitempty"`
	AllowPublish           *bool                 `json:"allowPublish,omitempty" yaml:"allowPublish,omitempty"`
	Topics                 []string              `json:"topics,omitempty" yaml:"topics,omitempty"`
	Params                 GossipSubParamsConfig `json:"params,omitempty" yaml:"params,omitempty"`
	Score                  *PeerScoreConfig      `json:"score,omitempty" yaml:"score,omitempty"`
	ScoreInspectInterval   string                `json:"scoreInspectInterval,omitempty" yaml:"scoreInspectInterval,omitempty"`
	FloodPublish           *bool                 `json:"floodPublish,omitempty" yaml:"floodPublish,omitempty"`
	PeerExchange           *bool                 `json:"peerExchange,omitempty" yaml:"peerExchange,omitempty"`
	MaxMessageSize         *int                  `json:"maxMessageSize,omitempty" yaml:"maxMessageSize,omitempty"`
	PeerOutboundQueueSize  *int                  `json:"peerOutboundQueueSize,omitempty" yaml:"peerOutboundQueueSize,omitempty"`
	SignaturePolicy        string                `json:"signaturePolicy,omitempty" yaml:"signaturePolicy,omitempty"`
	SeenMessagesTTL        string                `json:"seenMessagesTTL,omitempty" yaml:"seenMessagesTTL,omitempty"`
	ValidateQueueSize      *int                  `json:"validateQueueSize,omitempty" yaml:"validateQueueSize,omitempty"`
	ValidateThrottle       *int                  `json:"validateThrottle,omitempty" yaml:"validateThrottle,omitempty"`
	ValidateWorkers        *int                  `json:"validateWorkers,omitempty" yaml:"validateWorkers,omitempty"`
	SubscriptionBufferSize *int                  `json:"subscriptionBufferSize,omitempty" yaml:"subscriptionBufferSize,omitempty"`
}

// GossipSubParamsConfig covers every field in GossipSubParams provided by the
// pinned go-libp2p-pubsub version. Pointers distinguish an omitted value from an
// intentional zero, so experiments can override library defaults precisely.
type GossipSubParamsConfig struct {
	D                         *int     `json:"d,omitempty" yaml:"d,omitempty"`
	DLow                      *int     `json:"dLow,omitempty" yaml:"dLow,omitempty"`
	DHigh                     *int     `json:"dHigh,omitempty" yaml:"dHigh,omitempty"`
	DScore                    *int     `json:"dScore,omitempty" yaml:"dScore,omitempty"`
	DOut                      *int     `json:"dOut,omitempty" yaml:"dOut,omitempty"`
	DLazy                     *int     `json:"dLazy,omitempty" yaml:"dLazy,omitempty"`
	HistoryLength             *int     `json:"historyLength,omitempty" yaml:"historyLength,omitempty"`
	HistoryGossip             *int     `json:"historyGossip,omitempty" yaml:"historyGossip,omitempty"`
	GossipFactor              *float64 `json:"gossipFactor,omitempty" yaml:"gossipFactor,omitempty"`
	GossipRetransmission      *int     `json:"gossipRetransmission,omitempty" yaml:"gossipRetransmission,omitempty"`
	HeartbeatInitialDelay     string   `json:"heartbeatInitialDelay,omitempty" yaml:"heartbeatInitialDelay,omitempty"`
	HeartbeatInterval         string   `json:"heartbeatInterval,omitempty" yaml:"heartbeatInterval,omitempty"`
	SlowHeartbeatWarning      *float64 `json:"slowHeartbeatWarning,omitempty" yaml:"slowHeartbeatWarning,omitempty"`
	FanoutTTL                 string   `json:"fanoutTTL,omitempty" yaml:"fanoutTTL,omitempty"`
	PrunePeers                *int     `json:"prunePeers,omitempty" yaml:"prunePeers,omitempty"`
	PruneBackoff              string   `json:"pruneBackoff,omitempty" yaml:"pruneBackoff,omitempty"`
	UnsubscribeBackoff        string   `json:"unsubscribeBackoff,omitempty" yaml:"unsubscribeBackoff,omitempty"`
	Connectors                *int     `json:"connectors,omitempty" yaml:"connectors,omitempty"`
	MaxPendingConnections     *int     `json:"maxPendingConnections,omitempty" yaml:"maxPendingConnections,omitempty"`
	ConnectionTimeout         string   `json:"connectionTimeout,omitempty" yaml:"connectionTimeout,omitempty"`
	DirectConnectTicks        *uint64  `json:"directConnectTicks,omitempty" yaml:"directConnectTicks,omitempty"`
	DirectConnectInitialDelay string   `json:"directConnectInitialDelay,omitempty" yaml:"directConnectInitialDelay,omitempty"`
	OpportunisticGraftTicks   *uint64  `json:"opportunisticGraftTicks,omitempty" yaml:"opportunisticGraftTicks,omitempty"`
	OpportunisticGraftPeers   *int     `json:"opportunisticGraftPeers,omitempty" yaml:"opportunisticGraftPeers,omitempty"`
	GraftFloodThreshold       string   `json:"graftFloodThreshold,omitempty" yaml:"graftFloodThreshold,omitempty"`
	MaxIHaveLength            *int     `json:"maxIHaveLength,omitempty" yaml:"maxIHaveLength,omitempty"`
	MaxIHaveMessages          *int     `json:"maxIHaveMessages,omitempty" yaml:"maxIHaveMessages,omitempty"`
	MaxIDontWantLength        *int     `json:"maxIDontWantLength,omitempty" yaml:"maxIDontWantLength,omitempty"`
	MaxIDontWantMessages      *int     `json:"maxIDontWantMessages,omitempty" yaml:"maxIDontWantMessages,omitempty"`
	IWantFollowupTime         string   `json:"iWantFollowupTime,omitempty" yaml:"iWantFollowupTime,omitempty"`
	IDontWantMessageThreshold *int     `json:"iDontWantMessageThreshold,omitempty" yaml:"iDontWantMessageThreshold,omitempty"`
	IDontWantMessageTTL       *int     `json:"iDontWantMessageTTL,omitempty" yaml:"iDontWantMessageTTL,omitempty"`
}

type PeerScoreConfig struct {
	SkipAtomicValidation bool    `json:"skipAtomicValidation,omitempty" yaml:"skipAtomicValidation,omitempty"`
	TopicScoreCap        float64 `json:"topicScoreCap,omitempty" yaml:"topicScoreCap,omitempty"`
	// AppSpecificWeight must remain zero: a serializable node config cannot
	// provide the application callback required by pubsub's P5 score term.
	AppSpecificWeight           float64                     `json:"appSpecificWeight,omitempty" yaml:"appSpecificWeight,omitempty"`
	IPColocationFactorWeight    float64                     `json:"ipColocationFactorWeight,omitempty" yaml:"ipColocationFactorWeight,omitempty"`
	IPColocationFactorThreshold int                         `json:"ipColocationFactorThreshold,omitempty" yaml:"ipColocationFactorThreshold,omitempty"`
	IPColocationFactorWhitelist []string                    `json:"ipColocationFactorWhitelist,omitempty" yaml:"ipColocationFactorWhitelist,omitempty"`
	BehaviourPenaltyWeight      float64                     `json:"behaviourPenaltyWeight,omitempty" yaml:"behaviourPenaltyWeight,omitempty"`
	BehaviourPenaltyThreshold   float64                     `json:"behaviourPenaltyThreshold,omitempty" yaml:"behaviourPenaltyThreshold,omitempty"`
	BehaviourPenaltyDecay       float64                     `json:"behaviourPenaltyDecay,omitempty" yaml:"behaviourPenaltyDecay,omitempty"`
	DecayInterval               string                      `json:"decayInterval,omitempty" yaml:"decayInterval,omitempty"`
	DecayToZero                 float64                     `json:"decayToZero,omitempty" yaml:"decayToZero,omitempty"`
	RetainScore                 string                      `json:"retainScore,omitempty" yaml:"retainScore,omitempty"`
	SeenMessageTTL              string                      `json:"seenMessageTTL,omitempty" yaml:"seenMessageTTL,omitempty"`
	Thresholds                  PeerScoreThresholdsConfig   `json:"thresholds" yaml:"thresholds"`
	Topics                      map[string]TopicScoreConfig `json:"topics" yaml:"topics"`
}

type PeerScoreThresholdsConfig struct {
	SkipAtomicValidation        bool    `json:"skipAtomicValidation,omitempty" yaml:"skipAtomicValidation,omitempty"`
	GossipThreshold             float64 `json:"gossipThreshold" yaml:"gossipThreshold"`
	PublishThreshold            float64 `json:"publishThreshold" yaml:"publishThreshold"`
	GraylistThreshold           float64 `json:"graylistThreshold" yaml:"graylistThreshold"`
	AcceptPXThreshold           float64 `json:"acceptPXThreshold" yaml:"acceptPXThreshold"`
	OpportunisticGraftThreshold float64 `json:"opportunisticGraftThreshold" yaml:"opportunisticGraftThreshold"`
}

type TopicScoreConfig struct {
	SkipAtomicValidation            bool    `json:"skipAtomicValidation,omitempty" yaml:"skipAtomicValidation,omitempty"`
	TopicWeight                     float64 `json:"topicWeight" yaml:"topicWeight"`
	TimeInMeshWeight                float64 `json:"timeInMeshWeight" yaml:"timeInMeshWeight"`
	TimeInMeshQuantum               string  `json:"timeInMeshQuantum" yaml:"timeInMeshQuantum"`
	TimeInMeshCap                   float64 `json:"timeInMeshCap" yaml:"timeInMeshCap"`
	FirstMessageDeliveriesWeight    float64 `json:"firstMessageDeliveriesWeight" yaml:"firstMessageDeliveriesWeight"`
	FirstMessageDeliveriesDecay     float64 `json:"firstMessageDeliveriesDecay" yaml:"firstMessageDeliveriesDecay"`
	FirstMessageDeliveriesCap       float64 `json:"firstMessageDeliveriesCap" yaml:"firstMessageDeliveriesCap"`
	MeshMessageDeliveriesWeight     float64 `json:"meshMessageDeliveriesWeight" yaml:"meshMessageDeliveriesWeight"`
	MeshMessageDeliveriesDecay      float64 `json:"meshMessageDeliveriesDecay" yaml:"meshMessageDeliveriesDecay"`
	MeshMessageDeliveriesThreshold  float64 `json:"meshMessageDeliveriesThreshold" yaml:"meshMessageDeliveriesThreshold"`
	MeshMessageDeliveriesCap        float64 `json:"meshMessageDeliveriesCap" yaml:"meshMessageDeliveriesCap"`
	MeshMessageDeliveriesActivation string  `json:"meshMessageDeliveriesActivation" yaml:"meshMessageDeliveriesActivation"`
	MeshMessageDeliveriesWindow     string  `json:"meshMessageDeliveriesWindow" yaml:"meshMessageDeliveriesWindow"`
	MeshFailurePenaltyWeight        float64 `json:"meshFailurePenaltyWeight" yaml:"meshFailurePenaltyWeight"`
	MeshFailurePenaltyDecay         float64 `json:"meshFailurePenaltyDecay" yaml:"meshFailurePenaltyDecay"`
	InvalidMessageDeliveriesWeight  float64 `json:"invalidMessageDeliveriesWeight" yaml:"invalidMessageDeliveriesWeight"`
	InvalidMessageDeliveriesDecay   float64 `json:"invalidMessageDeliveriesDecay" yaml:"invalidMessageDeliveriesDecay"`
}

func boolPointer(value bool) *bool { return &value }
func intPointer(value int) *int    { return &value }

func applyV2WorkerGossipDefaults(config *NodeConfig) {
	config.GossipSub.Params.DLow = intPointer(5)
	config.GossipSub.Params.DScore = intPointer(3)
	config.GossipSub.Params.MaxIHaveLength = intPointer(5500)
	config.GossipSub.Params.HeartbeatInitialDelay = "1s"
}

// BuiltInNodeConfig returns useful behavior presets. Any preset can be further
// overridden by a scenario profile or an inline node block.
func BuiltInNodeConfig(kind string) (NodeConfig, bool) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	config := NodeConfig{Type: kind}
	switch kind {
	case "boot":
		config.Kademlia.Mode = "server"
		config.GossipSub.Enabled = boolPointer(false)
		config.GossipSub.Params.DScore = intPointer(3)
	case "full", "worker":
		config.Type = "full"
		config.Kademlia.Mode = "server"
		config.Libp2p.ConnectionLimit = intPointer(55)
		applyV2WorkerGossipDefaults(&config)
	case "light":
		config.Kademlia.Mode = "client"
		config.Libp2p.ConnectionLimit = intPointer(32)
		applyV2WorkerGossipDefaults(&config)
	case "publisher":
		config.Kademlia.Mode = "client"
		config.Libp2p.ConnectionLimit = intPointer(55)
		applyV2WorkerGossipDefaults(&config)
		config.GossipSub.Subscribe = boolPointer(false)
		config.GossipSub.TopicMode = "publish"
	case "subscriber", "observer":
		config.Kademlia.Mode = "client"
		config.Libp2p.ConnectionLimit = intPointer(55)
		applyV2WorkerGossipDefaults(&config)
		config.GossipSub.AllowPublish = boolPointer(false)
	case "relay":
		config.Kademlia.Mode = "client"
		config.Libp2p.ConnectionLimit = intPointer(55)
		applyV2WorkerGossipDefaults(&config)
		config.GossipSub.TopicMode = "relay"
		config.GossipSub.AllowPublish = boolPointer(false)
	case "flood":
		config.Kademlia.Mode = "client"
		config.Libp2p.ConnectionLimit = intPointer(55)
		config.GossipSub.Router = "floodsub"
	case "random":
		config.Kademlia.Mode = "client"
		config.Libp2p.ConnectionLimit = intPointer(55)
		config.GossipSub.Router = "randomsub"
		config.GossipSub.RandomDegree = intPointer(6)
		config.GossipSub.RandomNetworkSize = intPointer(100)
	case "dht-only":
		config.Kademlia.Mode = "server"
		config.Libp2p.ConnectionLimit = intPointer(55)
		config.GossipSub.Enabled = boolPointer(false)
	case "gossip-only":
		config.Kademlia.Enabled = boolPointer(false)
		config.Libp2p.ConnectionLimit = intPointer(55)
		applyV2WorkerGossipDefaults(&config)
	case "non-gossip", "mesh-only":
		config.Kademlia.Mode = "server"
		config.Libp2p.ConnectionLimit = intPointer(55)
		applyV2WorkerGossipDefaults(&config)
		zeroInt := 0
		zeroFloat := 0.0
		config.GossipSub.Params.HistoryGossip = &zeroInt
		config.GossipSub.Params.GossipFactor = &zeroFloat
	default:
		return NodeConfig{}, false
	}
	return config.WithDefaults(), true
}

// Merge overlays explicitly set values on top of a base configuration.
func (c NodeConfig) Merge(overlay NodeConfig) NodeConfig {
	overlay.promoteLegacy()
	if overlay.GossipSub.Subscribe != nil && overlay.GossipSub.TopicMode == "" {
		if *overlay.GossipSub.Subscribe {
			overlay.GossipSub.TopicMode = "subscribe"
		} else {
			overlay.GossipSub.TopicMode = "publish"
		}
	}
	if overlay.Kademlia.ProtocolID != "" && overlay.Kademlia.ProtocolPrefix == "" {
		c.Kademlia.ProtocolPrefix = ""
		if overlay.Kademlia.ProtocolExtension == "" {
			c.Kademlia.ProtocolExtension = ""
		}
	}
	if overlay.Kademlia.ProtocolPrefix != "" && overlay.Kademlia.ProtocolID == "" {
		c.Kademlia.ProtocolID = ""
	}
	// Switching network delay/shaper modes replaces the inherited alternative.
	// Explicitly supplying both in one overlay remains invalid during Validate.
	if overlay.Network.Delay != "" && overlay.Network.DelayDistribution == nil {
		c.Network.DelayDistribution = nil
	}
	if overlay.Network.DelayDistribution != nil && overlay.Network.Delay == "" {
		c.Network.Delay = ""
	}
	if overlay.Network.TBF != nil && overlay.Network.RateMbps == nil {
		c.Network.RateMbps = nil
	}
	if overlay.Network.RateMbps != nil && overlay.Network.TBF == nil {
		c.Network.TBF = nil
	}
	base := reflect.ValueOf(&c).Elem()
	mergeNonZero(base, reflect.ValueOf(overlay))
	return c
}

func mergeNonZero(dst, src reflect.Value) {
	for i := 0; i < src.NumField(); i++ {
		s, d := src.Field(i), dst.Field(i)
		switch s.Kind() {
		case reflect.Struct:
			mergeNonZero(d, s)
		case reflect.Pointer, reflect.Interface:
			if !s.IsNil() {
				d.Set(s)
			}
		case reflect.Slice, reflect.Map:
			if !s.IsNil() {
				d.Set(s)
			}
		default:
			if !s.IsZero() {
				d.Set(s)
			}
		}
	}
}

func (c *NodeConfig) promoteLegacy() {
	if len(c.GossipSub.Topics) == 0 && len(c.Topics) > 0 {
		c.GossipSub.Topics = append([]string(nil), c.Topics...)
	}
	p := &c.GossipSub.Params
	if c.D != 0 && p.D == nil {
		p.D = intPointer(c.D)
	}
	if c.DLow != 0 && p.DLow == nil {
		p.DLow = intPointer(c.DLow)
	}
	if c.DHigh != 0 && p.DHigh == nil {
		p.DHigh = intPointer(c.DHigh)
	}
	if c.DOut != 0 && p.DOut == nil {
		p.DOut = intPointer(c.DOut)
	}
	if c.DLazy != 0 && p.DLazy == nil {
		p.DLazy = intPointer(c.DLazy)
	}
	if c.Heartbeat != "" && p.HeartbeatInterval == "" {
		p.HeartbeatInterval = c.Heartbeat
	}
	if c.ConnectLimit != 0 && c.Libp2p.ConnectionLimit == nil {
		c.Libp2p.ConnectionLimit = intPointer(c.ConnectLimit)
	}
}

func (c NodeConfig) WithDefaults() NodeConfig {
	c.promoteLegacy()
	if c.Type == "" {
		c.Type = "full"
	}
	if c.GossipSub.Enabled == nil {
		c.GossipSub.Enabled = boolPointer(true)
	}
	if c.GossipSub.Router == "" {
		c.GossipSub.Router = "gossipsub"
	}
	if c.GossipSub.TopicMode == "" {
		if c.GossipSub.Subscribe != nil && !*c.GossipSub.Subscribe {
			c.GossipSub.TopicMode = "publish"
		} else {
			c.GossipSub.TopicMode = "subscribe"
		}
	}
	if c.GossipSub.Subscribe == nil {
		c.GossipSub.Subscribe = boolPointer(true)
	}
	c.GossipSub.Subscribe = boolPointer(c.GossipSub.TopicMode == "subscribe")
	if c.GossipSub.AllowPublish == nil {
		c.GossipSub.AllowPublish = boolPointer(true)
	}
	if c.GossipSub.Score != nil && c.GossipSub.ScoreInspectInterval == "" {
		c.GossipSub.ScoreInspectInterval = "1s"
	}
	if len(c.GossipSub.Topics) == 0 {
		c.GossipSub.Topics = []string{"kpl/default"}
	}
	if c.Kademlia.Enabled == nil {
		c.Kademlia.Enabled = boolPointer(true)
	}
	if c.Kademlia.Mode == "" {
		c.Kademlia.Mode = "server"
	}
	if c.Kademlia.ProtocolPrefix == "" && c.Kademlia.ProtocolID == "" {
		c.Kademlia.ProtocolPrefix = "/k-p2p-lab/v3"
	}
	if c.Kademlia.BucketSize == nil {
		c.Kademlia.BucketSize = intPointer(20)
	}
	if c.Kademlia.BootstrapTimeout == "" {
		c.Kademlia.BootstrapTimeout = "60s"
	}
	if c.Kademlia.BootstrapRetryInterval == "" {
		c.Kademlia.BootstrapRetryInterval = "1s"
	}

	// These are the v2/libp2p defaults and remain explicit in manifests.
	p := &c.GossipSub.Params
	if p.D == nil {
		p.D = intPointer(6)
	}
	if p.DLow == nil {
		p.DLow = intPointer(5)
	}
	if p.DHigh == nil {
		p.DHigh = intPointer(12)
	}
	if p.DScore == nil {
		p.DScore = intPointer(4)
	}
	if p.DOut == nil {
		p.DOut = intPointer(2)
	}
	if p.DLazy == nil {
		p.DLazy = intPointer(6)
	}
	if p.HistoryLength == nil {
		p.HistoryLength = intPointer(5)
	}
	if p.HistoryGossip == nil {
		p.HistoryGossip = intPointer(3)
	}
	if p.GossipFactor == nil {
		value := 0.25
		p.GossipFactor = &value
	}
	if p.GossipRetransmission == nil {
		p.GossipRetransmission = intPointer(3)
	}
	if p.HeartbeatInitialDelay == "" {
		p.HeartbeatInitialDelay = "100ms"
	}
	if p.HeartbeatInterval == "" {
		p.HeartbeatInterval = "1s"
	}
	if p.SlowHeartbeatWarning == nil {
		value := 0.1
		p.SlowHeartbeatWarning = &value
	}
	if p.FanoutTTL == "" {
		p.FanoutTTL = "1m"
	}
	if p.PrunePeers == nil {
		p.PrunePeers = intPointer(16)
	}
	if p.PruneBackoff == "" {
		p.PruneBackoff = "1m"
	}
	if p.UnsubscribeBackoff == "" {
		p.UnsubscribeBackoff = "10s"
	}
	if p.Connectors == nil {
		p.Connectors = intPointer(8)
	}
	if p.MaxPendingConnections == nil {
		p.MaxPendingConnections = intPointer(128)
	}
	if p.ConnectionTimeout == "" {
		p.ConnectionTimeout = "30s"
	}
	if p.DirectConnectTicks == nil {
		value := uint64(300)
		p.DirectConnectTicks = &value
	}
	if p.DirectConnectInitialDelay == "" {
		p.DirectConnectInitialDelay = "1s"
	}
	if p.OpportunisticGraftTicks == nil {
		value := uint64(60)
		p.OpportunisticGraftTicks = &value
	}
	if p.OpportunisticGraftPeers == nil {
		p.OpportunisticGraftPeers = intPointer(2)
	}
	if p.GraftFloodThreshold == "" {
		p.GraftFloodThreshold = "10s"
	}
	if p.MaxIHaveLength == nil {
		p.MaxIHaveLength = intPointer(5000)
	}
	if p.MaxIHaveMessages == nil {
		p.MaxIHaveMessages = intPointer(10)
	}
	if p.MaxIDontWantLength == nil {
		p.MaxIDontWantLength = intPointer(10)
	}
	if p.MaxIDontWantMessages == nil {
		p.MaxIDontWantMessages = intPointer(1000)
	}
	if p.IWantFollowupTime == "" {
		p.IWantFollowupTime = "3s"
	}
	if p.IDontWantMessageThreshold == nil {
		p.IDontWantMessageThreshold = intPointer(1 << 10)
	}
	if p.IDontWantMessageTTL == nil {
		p.IDontWantMessageTTL = intPointer(3)
	}

	c.Topics = append([]string(nil), c.GossipSub.Topics...)
	c.D, c.DLow, c.DHigh = *p.D, *p.DLow, *p.DHigh
	c.DOut, c.DLazy, c.Heartbeat = *p.DOut, *p.DLazy, p.HeartbeatInterval
	if c.Libp2p.ConnectionLimit != nil {
		c.ConnectLimit = *c.Libp2p.ConnectionLimit
	}
	return c
}

func (c NodeConfig) Validate() error {
	c = c.WithDefaults()
	if err := c.Network.Validate(); err != nil {
		return fmt.Errorf("network: %w", err)
	}
	if c.Type == "" {
		return fmt.Errorf("node type is required")
	}
	if c.Libp2p.ConnectionLimit != nil && *c.Libp2p.ConnectionLimit < 0 {
		return fmt.Errorf("libp2p connectionLimit cannot be negative")
	}
	if c.Kademlia.Enabled != nil && *c.Kademlia.Enabled {
		if c.Kademlia.ProtocolID != "" && c.Kademlia.ProtocolPrefix != "" {
			return fmt.Errorf("kademlia protocolId and protocolPrefix are mutually exclusive")
		}
		if c.Kademlia.ProtocolID != "" && c.Kademlia.ProtocolExtension != "" {
			return fmt.Errorf("kademlia protocolExtension cannot be combined with protocolId")
		}
		switch c.Kademlia.Mode {
		case "auto", "auto-server", "client", "server":
		default:
			return fmt.Errorf("invalid kademlia mode %q", c.Kademlia.Mode)
		}
		if c.Kademlia.BucketSize != nil && *c.Kademlia.BucketSize <= 0 {
			return fmt.Errorf("kademlia bucketSize must be positive")
		}
		if c.Kademlia.Concurrency != nil && *c.Kademlia.Concurrency <= 0 {
			return fmt.Errorf("kademlia concurrency must be positive")
		}
		if c.Kademlia.Resiliency != nil && *c.Kademlia.Resiliency < 0 {
			return fmt.Errorf("kademlia resiliency cannot be negative")
		}
		if c.Kademlia.LookupCheckConcurrency != nil && *c.Kademlia.LookupCheckConcurrency <= 0 {
			return fmt.Errorf("kademlia lookupCheckConcurrency must be positive")
		}
		if c.Kademlia.OptimisticProvideJobsPoolSize != nil && *c.Kademlia.OptimisticProvideJobsPoolSize <= 0 {
			return fmt.Errorf("kademlia optimisticProvideJobsPoolSize must be positive")
		}
	}
	if c.GossipSub.Enabled != nil && *c.GossipSub.Enabled {
		if len(c.GossipSub.Topics) == 0 {
			return fmt.Errorf("gossipsub topics cannot be empty")
		}
		switch c.GossipSub.Router {
		case "gossipsub", "floodsub", "randomsub":
		default:
			return fmt.Errorf("invalid pubsub router %q", c.GossipSub.Router)
		}
		switch c.GossipSub.TopicMode {
		case "subscribe", "relay", "publish":
		default:
			return fmt.Errorf("invalid topicMode %q", c.GossipSub.TopicMode)
		}
		if c.GossipSub.Router == "randomsub" {
			if c.GossipSub.RandomDegree == nil || *c.GossipSub.RandomDegree <= 0 {
				return fmt.Errorf("randomsub requires a positive randomDegree")
			}
			if c.GossipSub.RandomNetworkSize == nil || *c.GossipSub.RandomNetworkSize <= 0 {
				return fmt.Errorf("randomsub requires a positive randomNetworkSize")
			}
		}
		if c.GossipSub.Router != "gossipsub" {
			if c.GossipSub.Score != nil {
				return fmt.Errorf("gossipsub score requires the gossipsub router")
			}
			if c.GossipSub.FloodPublish != nil || c.GossipSub.PeerExchange != nil {
				return fmt.Errorf("floodPublish and peerExchange require the gossipsub router")
			}
		}
		if c.GossipSub.Score == nil && c.GossipSub.ScoreInspectInterval != "" {
			return fmt.Errorf("scoreInspectInterval requires gossipsub score")
		}
		for name, value := range map[string]*int{
			"peerOutboundQueueSize": c.GossipSub.PeerOutboundQueueSize,
			"validateQueueSize":     c.GossipSub.ValidateQueueSize,
			"validateWorkers":       c.GossipSub.ValidateWorkers,
		} {
			if value != nil && *value <= 0 {
				return fmt.Errorf("gossipsub %s must be positive", name)
			}
		}
		for name, value := range map[string]*int{
			"maxMessageSize":         c.GossipSub.MaxMessageSize,
			"subscriptionBufferSize": c.GossipSub.SubscriptionBufferSize,
			"validateThrottle":       c.GossipSub.ValidateThrottle,
		} {
			if value != nil && *value < 0 {
				return fmt.Errorf("gossipsub %s cannot be negative", name)
			}
		}
		if c.GossipSub.Router == "gossipsub" {
			p := c.GossipSub.Params
			if *p.DLow > *p.D || *p.D > *p.DHigh {
				return fmt.Errorf("gossipsub degree must satisfy dLow <= d <= dHigh")
			}
			if *p.D <= 0 || *p.DLow < 0 || *p.DHigh <= 0 || *p.DScore < 0 || *p.DScore > *p.D || *p.DLazy < 0 {
				return fmt.Errorf("gossipsub degrees must be non-negative, with positive d/dHigh and dScore <= d")
			}
			if *p.DOut < 0 || *p.DOut >= *p.DLow || *p.DOut > *p.D/2 {
				return fmt.Errorf("gossipsub dOut must be < dLow and <= d/2")
			}
			if *p.HistoryLength <= 0 || *p.HistoryGossip < 0 || *p.HistoryGossip > *p.HistoryLength {
				return fmt.Errorf("gossipsub history requires 0 <= historyGossip <= historyLength")
			}
			if *p.DirectConnectTicks == 0 || *p.OpportunisticGraftTicks == 0 {
				return fmt.Errorf("gossipsub directConnectTicks and opportunisticGraftTicks must be positive")
			}
			if !validNumber(*p.GossipFactor) || *p.GossipFactor < 0 || *p.GossipFactor > 1 {
				return fmt.Errorf("gossipsub gossipFactor must be finite and in [0, 1]")
			}
			if !validNumber(*p.SlowHeartbeatWarning) || *p.SlowHeartbeatWarning < 0 {
				return fmt.Errorf("gossipsub slowHeartbeatWarning must be finite and non-negative")
			}
			for name, value := range map[string]int{
				"gossipRetransmission":      *p.GossipRetransmission,
				"prunePeers":                *p.PrunePeers,
				"connectors":                *p.Connectors,
				"maxPendingConnections":     *p.MaxPendingConnections,
				"opportunisticGraftPeers":   *p.OpportunisticGraftPeers,
				"maxIHaveLength":            *p.MaxIHaveLength,
				"maxIHaveMessages":          *p.MaxIHaveMessages,
				"maxIDontWantLength":        *p.MaxIDontWantLength,
				"maxIDontWantMessages":      *p.MaxIDontWantMessages,
				"iDontWantMessageThreshold": *p.IDontWantMessageThreshold,
				"iDontWantMessageTTL":       *p.IDontWantMessageTTL,
			} {
				if value < 0 {
					return fmt.Errorf("gossipsub %s cannot be negative", name)
				}
			}
		}
		switch c.GossipSub.SignaturePolicy {
		case "", "strict-sign", "strict-no-sign":
		default:
			return fmt.Errorf("invalid signaturePolicy %q", c.GossipSub.SignaturePolicy)
		}
		if c.GossipSub.Router == "gossipsub" && c.GossipSub.Score != nil {
			if err := c.GossipSub.Score.validate(); err != nil {
				return fmt.Errorf("gossipsub score: %w", err)
			}
		}
	}
	positiveDurations := map[string]bool{
		"libp2p.dialTimeout":                  true,
		"kademlia.routingTableRefreshPeriod":  true,
		"kademlia.routingTableRefreshTimeout": true,
		"kademlia.bootstrapTimeout":           true,
		"kademlia.bootstrapRetryInterval":     true,
	}
	durations := map[string]string{
		"libp2p.dialTimeout":                    c.Libp2p.DialTimeout,
		"kademlia.routingTableLatencyTolerance": c.Kademlia.RoutingTableLatencyTolerance,
		"kademlia.routingTableRefreshPeriod":    c.Kademlia.RoutingTableRefreshPeriod,
		"kademlia.routingTableRefreshTimeout":   c.Kademlia.RoutingTableRefreshTimeout,
		"kademlia.maxRecordAge":                 c.Kademlia.MaxRecordAge,
		"kademlia.bootstrapTimeout":             c.Kademlia.BootstrapTimeout,
		"kademlia.bootstrapRetryInterval":       c.Kademlia.BootstrapRetryInterval,
		"gossipsub.seenMessagesTTL":             c.GossipSub.SeenMessagesTTL,
		"gossipsub.scoreInspectInterval":        c.GossipSub.ScoreInspectInterval,
	}
	if c.GossipSub.Enabled != nil && *c.GossipSub.Enabled && c.GossipSub.Router == "gossipsub" {
		positiveDurations["gossipsub.heartbeatInterval"] = true
		for name, value := range map[string]string{
			"gossipsub.heartbeatInitialDelay":     c.GossipSub.Params.HeartbeatInitialDelay,
			"gossipsub.heartbeatInterval":         c.GossipSub.Params.HeartbeatInterval,
			"gossipsub.fanoutTTL":                 c.GossipSub.Params.FanoutTTL,
			"gossipsub.pruneBackoff":              c.GossipSub.Params.PruneBackoff,
			"gossipsub.unsubscribeBackoff":        c.GossipSub.Params.UnsubscribeBackoff,
			"gossipsub.connectionTimeout":         c.GossipSub.Params.ConnectionTimeout,
			"gossipsub.directConnectInitialDelay": c.GossipSub.Params.DirectConnectInitialDelay,
			"gossipsub.graftFloodThreshold":       c.GossipSub.Params.GraftFloodThreshold,
			"gossipsub.iWantFollowupTime":         c.GossipSub.Params.IWantFollowupTime,
		} {
			durations[name] = value
		}
	}
	for name, value := range durations {
		if value != "" {
			duration, err := time.ParseDuration(value)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			if duration < 0 {
				return fmt.Errorf("%s cannot be negative", name)
			}
			if duration == 0 && positiveDurations[name] {
				return fmt.Errorf("%s must be positive", name)
			}
		}
	}
	if cm := c.Libp2p.ConnectionManager; cm != nil {
		if cm.LowWater < 0 || cm.HighWater <= 0 || cm.LowWater > cm.HighWater {
			return fmt.Errorf("connectionManager requires 0 <= lowWater <= highWater")
		}
		if cm.GracePeriod != "" {
			gracePeriod, err := time.ParseDuration(cm.GracePeriod)
			if err != nil {
				return fmt.Errorf("connectionManager.gracePeriod: %w", err)
			}
			if gracePeriod < 0 {
				return fmt.Errorf("connectionManager.gracePeriod cannot be negative")
			}
		}
	}
	return nil
}

func (c PeerScoreConfig) validate() error {
	if !validNumber(c.TopicScoreCap) || c.TopicScoreCap < 0 {
		return fmt.Errorf("topicScoreCap must be finite and non-negative")
	}
	if !validNumber(c.AppSpecificWeight) {
		return fmt.Errorf("appSpecificWeight must be finite")
	}
	if c.AppSpecificWeight != 0 {
		return fmt.Errorf("appSpecificWeight is unsupported because no application-specific score function is configured")
	}
	if !c.SkipAtomicValidation || c.IPColocationFactorWeight != 0 {
		if !validNumber(c.IPColocationFactorWeight) || c.IPColocationFactorWeight > 0 {
			return fmt.Errorf("ipColocationFactorWeight must be finite and non-positive")
		}
		if c.IPColocationFactorWeight != 0 && c.IPColocationFactorThreshold < 1 {
			return fmt.Errorf("ipColocationFactorThreshold must be at least 1 when its weight is enabled")
		}
	}
	for _, raw := range c.IPColocationFactorWhitelist {
		if _, _, err := net.ParseCIDR(raw); err != nil {
			return fmt.Errorf("invalid ipColocationFactorWhitelist entry %q: %w", raw, err)
		}
	}
	if !c.SkipAtomicValidation || c.BehaviourPenaltyWeight != 0 || c.BehaviourPenaltyThreshold != 0 {
		if !validNumber(c.BehaviourPenaltyWeight) || c.BehaviourPenaltyWeight > 0 {
			return fmt.Errorf("behaviourPenaltyWeight must be finite and non-positive")
		}
		if c.BehaviourPenaltyWeight != 0 && !validDecay(c.BehaviourPenaltyDecay) {
			return fmt.Errorf("behaviourPenaltyDecay must be between 0 and 1 when its weight is enabled")
		}
		if !validNumber(c.BehaviourPenaltyThreshold) || c.BehaviourPenaltyThreshold < 0 {
			return fmt.Errorf("behaviourPenaltyThreshold must be finite and non-negative")
		}
	}

	decayInterval, err := scoreDuration("decayInterval", c.DecayInterval)
	if err != nil {
		return err
	}
	if !c.SkipAtomicValidation || decayInterval != 0 || c.DecayToZero != 0 {
		if decayInterval < time.Second {
			return fmt.Errorf("decayInterval must be at least 1s")
		}
		if !validDecay(c.DecayToZero) {
			return fmt.Errorf("decayToZero must be between 0 and 1")
		}
	}
	for name, raw := range map[string]string{
		"retainScore":    c.RetainScore,
		"seenMessageTTL": c.SeenMessageTTL,
	} {
		value, err := scoreDuration(name, raw)
		if err != nil {
			return err
		}
		if value < 0 {
			return fmt.Errorf("%s cannot be negative", name)
		}
	}
	if err := c.Thresholds.validate(); err != nil {
		return fmt.Errorf("thresholds: %w", err)
	}
	for topic, params := range c.Topics {
		if strings.TrimSpace(topic) == "" {
			return fmt.Errorf("score topic name cannot be empty")
		}
		if err := params.validate(); err != nil {
			return fmt.Errorf("topic %q: %w", topic, err)
		}
	}
	return nil
}

func (c PeerScoreThresholdsConfig) validate() error {
	if !c.SkipAtomicValidation || c.PublishThreshold != 0 || c.GossipThreshold != 0 || c.GraylistThreshold != 0 {
		if !validNumber(c.GossipThreshold) || c.GossipThreshold > 0 {
			return fmt.Errorf("gossipThreshold must be finite and non-positive")
		}
		if !validNumber(c.PublishThreshold) || c.PublishThreshold > c.GossipThreshold {
			return fmt.Errorf("publishThreshold must be finite and <= gossipThreshold")
		}
		if !validNumber(c.GraylistThreshold) || c.GraylistThreshold > c.PublishThreshold {
			return fmt.Errorf("graylistThreshold must be finite and <= publishThreshold")
		}
	}
	if !c.SkipAtomicValidation || c.AcceptPXThreshold != 0 {
		if !validNumber(c.AcceptPXThreshold) || c.AcceptPXThreshold < 0 {
			return fmt.Errorf("acceptPXThreshold must be finite and non-negative")
		}
	}
	if !c.SkipAtomicValidation || c.OpportunisticGraftThreshold != 0 {
		if !validNumber(c.OpportunisticGraftThreshold) || c.OpportunisticGraftThreshold < 0 {
			return fmt.Errorf("opportunisticGraftThreshold must be finite and non-negative")
		}
	}
	return nil
}

func (c TopicScoreConfig) validate() error {
	if !validNumber(c.TopicWeight) || c.TopicWeight < 0 {
		return fmt.Errorf("topicWeight must be finite and non-negative")
	}
	timeInMeshQuantum, err := scoreDuration("timeInMeshQuantum", c.TimeInMeshQuantum)
	if err != nil {
		return err
	}
	if !c.SkipAtomicValidation || c.TimeInMeshWeight != 0 || timeInMeshQuantum != 0 || c.TimeInMeshCap != 0 {
		if timeInMeshQuantum == 0 {
			return fmt.Errorf("timeInMeshQuantum must be non-zero")
		}
		if !validNumber(c.TimeInMeshWeight) || c.TimeInMeshWeight < 0 {
			return fmt.Errorf("timeInMeshWeight must be finite and non-negative")
		}
		if c.TimeInMeshWeight != 0 && (timeInMeshQuantum < 0 || !finitePositive(c.TimeInMeshCap)) {
			return fmt.Errorf("enabled time-in-mesh scoring requires a positive quantum and cap")
		}
	}
	if !c.SkipAtomicValidation || c.FirstMessageDeliveriesWeight != 0 || c.FirstMessageDeliveriesDecay != 0 || c.FirstMessageDeliveriesCap != 0 {
		if !validNumber(c.FirstMessageDeliveriesWeight) || c.FirstMessageDeliveriesWeight < 0 {
			return fmt.Errorf("firstMessageDeliveriesWeight must be finite and non-negative")
		}
		if c.FirstMessageDeliveriesWeight != 0 && (!validDecay(c.FirstMessageDeliveriesDecay) || !finitePositive(c.FirstMessageDeliveriesCap)) {
			return fmt.Errorf("enabled first-message scoring requires decay in (0,1) and a positive cap")
		}
	}
	meshActivation, err := scoreDuration("meshMessageDeliveriesActivation", c.MeshMessageDeliveriesActivation)
	if err != nil {
		return err
	}
	meshWindow, err := scoreDuration("meshMessageDeliveriesWindow", c.MeshMessageDeliveriesWindow)
	if err != nil {
		return err
	}
	if !c.SkipAtomicValidation || c.MeshMessageDeliveriesWeight != 0 || c.MeshMessageDeliveriesDecay != 0 || c.MeshMessageDeliveriesThreshold != 0 || c.MeshMessageDeliveriesCap != 0 || meshActivation != 0 || meshWindow != 0 {
		if !validNumber(c.MeshMessageDeliveriesWeight) || c.MeshMessageDeliveriesWeight > 0 {
			return fmt.Errorf("meshMessageDeliveriesWeight must be finite and non-positive")
		}
		if meshWindow < 0 {
			return fmt.Errorf("meshMessageDeliveriesWindow cannot be negative")
		}
		if c.MeshMessageDeliveriesWeight != 0 && (!validDecay(c.MeshMessageDeliveriesDecay) || !finitePositive(c.MeshMessageDeliveriesThreshold) || !finitePositive(c.MeshMessageDeliveriesCap) || meshActivation < time.Second) {
			return fmt.Errorf("enabled mesh-message scoring requires decay in (0,1), positive threshold/cap, and activation >= 1s")
		}
	}
	if !c.SkipAtomicValidation || c.MeshFailurePenaltyWeight != 0 || c.MeshFailurePenaltyDecay != 0 {
		if !validNumber(c.MeshFailurePenaltyWeight) || c.MeshFailurePenaltyWeight > 0 {
			return fmt.Errorf("meshFailurePenaltyWeight must be finite and non-positive")
		}
		if c.MeshFailurePenaltyWeight != 0 && !validDecay(c.MeshFailurePenaltyDecay) {
			return fmt.Errorf("enabled mesh-failure scoring requires decay in (0,1)")
		}
	}
	if !c.SkipAtomicValidation || c.InvalidMessageDeliveriesWeight != 0 || c.InvalidMessageDeliveriesDecay != 0 {
		if !validNumber(c.InvalidMessageDeliveriesWeight) || c.InvalidMessageDeliveriesWeight > 0 {
			return fmt.Errorf("invalidMessageDeliveriesWeight must be finite and non-positive")
		}
		if !validDecay(c.InvalidMessageDeliveriesDecay) {
			return fmt.Errorf("invalidMessageDeliveriesDecay must be between 0 and 1")
		}
	}
	return nil
}

func scoreDuration(name, raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return value, nil
}

func validNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validDecay(value float64) bool {
	return validNumber(value) && value > 0 && value < 1
}

func finitePositive(value float64) bool {
	return validNumber(value) && value > 0
}

func (c NodeConfig) PublishAllowed() bool {
	c = c.WithDefaults()
	return c.GossipSub.Enabled != nil && *c.GossipSub.Enabled && c.GossipSub.AllowPublish != nil && *c.GossipSub.AllowPublish
}
