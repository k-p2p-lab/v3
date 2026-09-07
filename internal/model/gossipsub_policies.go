package model

// PubSub callback policies are declarative and deterministic; arbitrary Go functions
// are not loaded from scenario files. PrivateKey is base64 libp2p MarshalPrivateKey.
type PubSubTopicConfig struct {
	MessageID              string                 `json:"messageId,omitempty" yaml:"messageId,omitempty"`
	SubscriptionBufferSize *int                   `json:"subscriptionBufferSize,omitempty" yaml:"subscriptionBufferSize,omitempty"`
	Validator              *PubSubValidatorConfig `json:"validator,omitempty" yaml:"validator,omitempty"`
}

type PubSubPeerRule struct {
	Allow []string `json:"allow,omitempty" yaml:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty" yaml:"deny,omitempty"`
}

type PubSubPeerFilterConfig struct {
	Allow  []string                  `json:"allow,omitempty" yaml:"allow,omitempty"`
	Deny   []string                  `json:"deny,omitempty" yaml:"deny,omitempty"`
	Topics map[string]PubSubPeerRule `json:"topics,omitempty" yaml:"topics,omitempty"`
}

type PubSubSubscriptionFilterConfig struct {
	Allowlist        []string `json:"allowlist,omitempty" yaml:"allowlist,omitempty"`
	Pattern          string   `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	MaxSubscriptions *int     `json:"maxSubscriptions,omitempty" yaml:"maxSubscriptions,omitempty"`
}

type PubSubProtocolConfig struct {
	ID       string   `json:"id,omitempty" yaml:"id,omitempty"`
	Features []string `json:"features,omitempty" yaml:"features,omitempty"`
}

type PubSubBlacklistConfig struct {
	Peers []string `json:"peers,omitempty" yaml:"peers,omitempty"`
	TTL   string   `json:"ttl,omitempty" yaml:"ttl,omitempty"`
}

type PubSubValidatorConfig struct {
	BasicSeqno       bool   `json:"basicSeqno,omitempty" yaml:"basicSeqno,omitempty"`
	Result           string `json:"result,omitempty" yaml:"result,omitempty"`
	FailureResult    string `json:"failureResult,omitempty" yaml:"failureResult,omitempty"`
	MinBytes         *int   `json:"minBytes,omitempty" yaml:"minBytes,omitempty"`
	MaxBytes         *int   `json:"maxBytes,omitempty" yaml:"maxBytes,omitempty"`
	Pattern          string `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	Format           string `json:"format,omitempty" yaml:"format,omitempty"`
	Delay            string `json:"delay,omitempty" yaml:"delay,omitempty"`
	Timeout          string `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Concurrency      *int   `json:"concurrency,omitempty" yaml:"concurrency,omitempty"`
	Inline           *bool  `json:"inline,omitempty" yaml:"inline,omitempty"`
	UseValidatorData bool   `json:"useValidatorData,omitempty" yaml:"useValidatorData,omitempty"`
}

type PubSubRPCInspectorConfig struct {
	MaxMessages       *int     `json:"maxMessages,omitempty" yaml:"maxMessages,omitempty"`
	MaxSubscriptions  *int     `json:"maxSubscriptions,omitempty" yaml:"maxSubscriptions,omitempty"`
	MaxControlEntries *int     `json:"maxControlEntries,omitempty" yaml:"maxControlEntries,omitempty"`
	MaxMessageIDs     *int     `json:"maxMessageIds,omitempty" yaml:"maxMessageIds,omitempty"`
	MaxBytes          *int     `json:"maxBytes,omitempty" yaml:"maxBytes,omitempty"`
	RejectPeers       []string `json:"rejectPeers,omitempty" yaml:"rejectPeers,omitempty"`
}

type PubSubDiscoveryConfig struct {
	Mode      string                          `json:"mode,omitempty" yaml:"mode,omitempty"`
	TTL       string                          `json:"ttl,omitempty" yaml:"ttl,omitempty"`
	Limit     *int                            `json:"limit,omitempty" yaml:"limit,omitempty"`
	Connector *PubSubDiscoveryConnectorConfig `json:"connector,omitempty" yaml:"connector,omitempty"`
}

type PubSubDiscoveryConnectorConfig struct {
	CacheSize   *int     `json:"cacheSize,omitempty" yaml:"cacheSize,omitempty"`
	DialTimeout string   `json:"dialTimeout,omitempty" yaml:"dialTimeout,omitempty"`
	MinBackoff  string   `json:"minBackoff,omitempty" yaml:"minBackoff,omitempty"`
	MaxBackoff  string   `json:"maxBackoff,omitempty" yaml:"maxBackoff,omitempty"`
	TimeUnit    string   `json:"timeUnit,omitempty" yaml:"timeUnit,omitempty"`
	Base        *float64 `json:"base,omitempty" yaml:"base,omitempty"`
	Offset      string   `json:"offset,omitempty" yaml:"offset,omitempty"`
	Jitter      string   `json:"jitter,omitempty" yaml:"jitter,omitempty"`
	Seed        *int64   `json:"seed,omitempty" yaml:"seed,omitempty"`
}

type PubSubAuthorConfig struct {
	PeerID     string `json:"peerId,omitempty" yaml:"peerId,omitempty"`
	PrivateKey string `json:"privateKey,omitempty" yaml:"privateKey,omitempty"`
	NoAuthor   bool   `json:"noAuthor,omitempty" yaml:"noAuthor,omitempty"`
}

type PubSubPublishConfig struct {
	ReadinessMinPeers *int                `json:"readinessMinPeers,omitempty" yaml:"readinessMinPeers,omitempty"`
	ReadinessTimeout  string              `json:"readinessTimeout,omitempty" yaml:"readinessTimeout,omitempty"`
	Local             *bool               `json:"local,omitempty" yaml:"local,omitempty"`
	ValidatorData     map[string]any      `json:"validatorData,omitempty" yaml:"validatorData,omitempty"`
	Author            *PubSubAuthorConfig `json:"author,omitempty" yaml:"author,omitempty"`
}

type PeerGaterConfig struct {
	Threshold            *float64           `json:"threshold,omitempty" yaml:"threshold,omitempty"`
	GlobalDecay          *float64           `json:"globalDecay,omitempty" yaml:"globalDecay,omitempty"`
	SourceDecay          *float64           `json:"sourceDecay,omitempty" yaml:"sourceDecay,omitempty"`
	DecayInterval        string             `json:"decayInterval,omitempty" yaml:"decayInterval,omitempty"`
	DecayToZero          *float64           `json:"decayToZero,omitempty" yaml:"decayToZero,omitempty"`
	RetainStats          string             `json:"retainStats,omitempty" yaml:"retainStats,omitempty"`
	Quiet                string             `json:"quiet,omitempty" yaml:"quiet,omitempty"`
	DuplicateWeight      *float64           `json:"duplicateWeight,omitempty" yaml:"duplicateWeight,omitempty"`
	IgnoreWeight         *float64           `json:"ignoreWeight,omitempty" yaml:"ignoreWeight,omitempty"`
	RejectWeight         *float64           `json:"rejectWeight,omitempty" yaml:"rejectWeight,omitempty"`
	TopicDeliveryWeights map[string]float64 `json:"topicDeliveryWeights,omitempty" yaml:"topicDeliveryWeights,omitempty"`
}
