package model

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

type PeerScoreConfig struct {
	Enabled              *bool   `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	SkipAtomicValidation bool    `json:"skipAtomicValidation,omitempty" yaml:"skipAtomicValidation,omitempty"`
	TopicScoreCap        float64 `json:"topicScoreCap,omitempty" yaml:"topicScoreCap,omitempty"`
	// AppSpecificScore supplies the declarative P5 callback. A nil policy returns
	// zero and requires a zero weight. The policy is immutable during a Peer run.
	AppSpecificScore            *AppSpecificScoreConfig     `json:"appSpecificScore,omitempty" yaml:"appSpecificScore,omitempty"`
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

// AppSpecificScoreConfig chooses an unweighted P5 score by libp2p Peer ID.
// An explicit peer entry, including zero, takes precedence over Default.
type AppSpecificScoreConfig struct {
	Default float64            `json:"default" yaml:"default"`
	Peers   map[string]float64 `json:"peers,omitempty" yaml:"peers,omitempty"`
}

// IsEnabled requires explicit opt-in; a score block alone does not enable scoring.
func (c PeerScoreConfig) IsEnabled() bool { return c.Enabled != nil && *c.Enabled }

// WithDefaults fills only omitted parameter groups accepted by selective
// validation. libp2p's score worker and mesh scoring still use these parameters
// when their associated weights are disabled. Explicit zero durations remain
// invalid instead of being silently replaced.
func (c PeerScoreConfig) WithDefaults() PeerScoreConfig {
	if c.IsEnabled() && c.SkipAtomicValidation && c.DecayInterval == "" && c.DecayToZero == 0 {
		c.DecayInterval = "1s"
		c.DecayToZero = 0.01
	}
	if c.Enabled != nil {
		c.Enabled = boolPointer(*c.Enabled)
	}
	c.IPColocationFactorWhitelist = append([]string(nil), c.IPColocationFactorWhitelist...)
	if c.AppSpecificScore != nil {
		policy := *c.AppSpecificScore
		if policy.Peers != nil {
			policy.Peers = make(map[string]float64, len(c.AppSpecificScore.Peers))
			for id, value := range c.AppSpecificScore.Peers {
				policy.Peers[id] = value
			}
		}
		c.AppSpecificScore = &policy
	}
	if c.Topics != nil {
		topics := make(map[string]TopicScoreConfig, len(c.Topics))
		for topic, params := range c.Topics {
			if c.IsEnabled() && params.SkipAtomicValidation && params.TimeInMeshQuantum == "" && params.TimeInMeshWeight == 0 && params.TimeInMeshCap == 0 {
				params.TimeInMeshQuantum = "1s"
			}
			topics[topic] = params
		}
		c.Topics = topics
	}
	return c
}

// Validate checks the exact normalized values passed to the pubsub router.
func (c PeerScoreConfig) Validate() error { return c.validate() }

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

func (c PeerScoreConfig) validate() error {
	c = c.WithDefaults()
	if err := c.validateSyntax(); err != nil {
		return err
	}
	if !c.IsEnabled() {
		return nil
	}
	if !validNumber(c.TopicScoreCap) || c.TopicScoreCap < 0 {
		return fmt.Errorf("topicScoreCap must be finite and non-negative")
	}
	if !validNumber(c.AppSpecificWeight) {
		return fmt.Errorf("appSpecificWeight must be finite")
	}
	if c.AppSpecificWeight != 0 && c.AppSpecificScore == nil {
		return fmt.Errorf("appSpecificWeight requires appSpecificScore")
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
	if decayInterval < time.Second {
		return fmt.Errorf("decayInterval must be at least 1s")
	}
	if !validDecay(c.DecayToZero) {
		return fmt.Errorf("decayToZero must be between 0 and 1")
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
	if err := c.validateSyntax(); err != nil {
		return err
	}
	if !validNumber(c.TopicWeight) || c.TopicWeight < 0 {
		return fmt.Errorf("topicWeight must be finite and non-negative")
	}
	timeInMeshQuantum, err := scoreDuration("timeInMeshQuantum", c.TimeInMeshQuantum)
	if err != nil {
		return err
	}
	if timeInMeshQuantum <= 0 {
		return fmt.Errorf("timeInMeshQuantum must be positive")
	}
	if !c.SkipAtomicValidation || c.TimeInMeshWeight != 0 || timeInMeshQuantum != 0 || c.TimeInMeshCap != 0 {
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

func (c AppSpecificScoreConfig) validate(weight float64) error {
	if !validNumber(c.Default) || !validNumber(c.Default*weight) {
		return fmt.Errorf("appSpecificScore default and its weighted value must be finite")
	}
	seen := make(map[peer.ID]string, len(c.Peers))
	for raw, score := range c.Peers {
		id, err := peer.Decode(raw)
		if err != nil {
			return fmt.Errorf("appSpecificScore peers key %q is not a valid peer ID: %w", raw, err)
		}
		if previous, exists := seen[id]; exists {
			return fmt.Errorf("appSpecificScore peers keys %q and %q identify the same peer", previous, raw)
		}
		seen[id] = raw
		if !validNumber(score) || !validNumber(score*weight) {
			return fmt.Errorf("appSpecificScore peer %q and its weighted value must be finite", raw)
		}
	}
	return nil
}

func finiteScoreValues(values map[string]float64) error {
	for name, value := range values {
		if !validNumber(value) {
			return fmt.Errorf("%s must be finite", name)
		}
	}
	return nil
}

// Disabled blocks may retain incomplete tuning, but malformed values must not
// cross YAML/JSON transport or become latent failures on the next activation.
func (c PeerScoreConfig) validateSyntax() error {
	if err := finiteScoreValues(map[string]float64{
		"topicScoreCap":             c.TopicScoreCap,
		"appSpecificWeight":         c.AppSpecificWeight,
		"ipColocationFactorWeight":  c.IPColocationFactorWeight,
		"behaviourPenaltyWeight":    c.BehaviourPenaltyWeight,
		"behaviourPenaltyThreshold": c.BehaviourPenaltyThreshold,
		"behaviourPenaltyDecay":     c.BehaviourPenaltyDecay,
		"decayToZero":               c.DecayToZero,
	}); err != nil {
		return err
	}
	if c.AppSpecificScore != nil {
		weight := 0.0
		if c.IsEnabled() {
			weight = c.AppSpecificWeight
		}
		if err := c.AppSpecificScore.validate(weight); err != nil {
			return err
		}
	}

	if err := finiteScoreValues(map[string]float64{
		"thresholds.gossipThreshold":             c.Thresholds.GossipThreshold,
		"thresholds.publishThreshold":            c.Thresholds.PublishThreshold,
		"thresholds.graylistThreshold":           c.Thresholds.GraylistThreshold,
		"thresholds.acceptPXThreshold":           c.Thresholds.AcceptPXThreshold,
		"thresholds.opportunisticGraftThreshold": c.Thresholds.OpportunisticGraftThreshold,
	}); err != nil {
		return err
	}
	for name, raw := range map[string]string{"decayInterval": c.DecayInterval, "retainScore": c.RetainScore, "seenMessageTTL": c.SeenMessageTTL} {
		value, err := scoreDuration(name, raw)
		if err != nil {
			return err
		}
		if value < 0 {
			return fmt.Errorf("%s cannot be negative", name)
		}
	}
	for _, raw := range c.IPColocationFactorWhitelist {
		if _, _, err := net.ParseCIDR(raw); err != nil {
			return fmt.Errorf("invalid ipColocationFactorWhitelist entry %q: %w", raw, err)
		}
	}
	for topic, params := range c.Topics {
		if strings.TrimSpace(topic) == "" {
			return fmt.Errorf("score topic name cannot be empty")
		}
		if err := params.validateSyntax(); err != nil {
			return fmt.Errorf("topic %q: %w", topic, err)
		}
	}
	return nil
}

func (c TopicScoreConfig) validateSyntax() error {
	if err := finiteScoreValues(map[string]float64{
		"topicWeight":                    c.TopicWeight,
		"timeInMeshWeight":               c.TimeInMeshWeight,
		"timeInMeshCap":                  c.TimeInMeshCap,
		"firstMessageDeliveriesWeight":   c.FirstMessageDeliveriesWeight,
		"firstMessageDeliveriesDecay":    c.FirstMessageDeliveriesDecay,
		"firstMessageDeliveriesCap":      c.FirstMessageDeliveriesCap,
		"meshMessageDeliveriesWeight":    c.MeshMessageDeliveriesWeight,
		"meshMessageDeliveriesDecay":     c.MeshMessageDeliveriesDecay,
		"meshMessageDeliveriesThreshold": c.MeshMessageDeliveriesThreshold,
		"meshMessageDeliveriesCap":       c.MeshMessageDeliveriesCap,
		"meshFailurePenaltyWeight":       c.MeshFailurePenaltyWeight,
		"meshFailurePenaltyDecay":        c.MeshFailurePenaltyDecay,
		"invalidMessageDeliveriesWeight": c.InvalidMessageDeliveriesWeight,
		"invalidMessageDeliveriesDecay":  c.InvalidMessageDeliveriesDecay,
	}); err != nil {
		return err
	}

	for name, raw := range map[string]string{
		"timeInMeshQuantum":               c.TimeInMeshQuantum,
		"meshMessageDeliveriesActivation": c.MeshMessageDeliveriesActivation,
		"meshMessageDeliveriesWindow":     c.MeshMessageDeliveriesWindow,
	} {
		value, err := scoreDuration(name, raw)
		if err != nil {
			return err
		}
		if value < 0 {
			return fmt.Errorf("%s cannot be negative", name)
		}
	}
	return nil
}
