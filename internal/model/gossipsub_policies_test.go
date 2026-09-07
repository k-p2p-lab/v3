package model

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"strings"
	"testing"

	libcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"gopkg.in/yaml.v3"
)

func TestPubSubExtendedScenarioRoundTripRetainsExplicitZeros(t *testing.T) {
	source := `gossipsub:
  messageId: topic-sha256
  signaturePolicy: strict-no-sign
  author:
    noAuthor: true
  seenMessagesStrategy: last-seen
  topicOptions:
    kpl/default:
      messageId: sha256
      subscriptionBufferSize: 0
      validator:
        maxBytes: 0
        result: accept
        inline: false
  subscriptionFilter:
    pattern: '^kpl/'
    maxSubscriptions: 5
  protocols:
    - id: /experiment/mesh/1.0.0
      features: [mesh, px, idontwant]
  peerGater:
    threshold: 0.25
    retainStats: 0s
    topicDeliveryWeights:
      kpl/default: 2
  rpcInspector:
    maxMessages: 0
    maxMessageIds: 0
  discovery:
    mode: routing
    ttl: 1m
    limit: 8
    connector:
      minBackoff: 1s
      maxBackoff: 1m
      base: 2
      seed: 0
  publish:
    readinessMinPeers: 0
    readinessTimeout: 3s
    local: false
`
	var config NodeConfig
	decoder := yaml.NewDecoder(strings.NewReader(source))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		t.Fatal(err)
	}
	config = config.WithDefaults()
	if err := config.GossipSub.ValidateExtended(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var decoded NodeConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.GossipSub.ValidateExtended(); err != nil {
		t.Fatal(err)
	}
	c := decoded.GossipSub
	if c.TopicOptions["kpl/default"].SubscriptionBufferSize == nil || *c.TopicOptions["kpl/default"].SubscriptionBufferSize != 0 || c.RPCInspector.MaxMessages == nil || *c.RPCInspector.MaxMessages != 0 || c.Discovery.Connector.Seed == nil || *c.Discovery.Connector.Seed != 0 || c.Publish.Local == nil || *c.Publish.Local {
		t.Fatalf("explicit zero/false values lost: %s", data)
	}
}
func TestPubSubExtendedRejectsInvalidPolicyCombinations(t *testing.T) {
	negative, zero := -1, 0
	nan := math.NaN()
	tests := map[string]func(*GossipSubConfig){
		"readiness exceeds request budget": func(c *GossipSubConfig) {
			c.Publish = &PubSubPublishConfig{ReadinessMinPeers: &zero, ReadinessTimeout: "11s"}
		},
		"local virtual author": func(c *GossipSubConfig) {
			local := true
			c.Publish = &PubSubPublishConfig{Local: &local, Author: &PubSubAuthorConfig{PrivateKey: "unused"}}
		},
		"unsigned sequence validator": func(c *GossipSubConfig) {
			c.SignaturePolicy = "strict-no-sign"
			c.DefaultValidators = []PubSubValidatorConfig{{BasicSeqno: true}}
		},
		"message id":     func(c *GossipSubConfig) { c.MessageID = "invalid" },
		"cache strategy": func(c *GossipSubConfig) { c.SeenMessagesStrategy = "invalid" },
		"unknown topic": func(c *GossipSubConfig) {
			c.TopicOptions = map[string]PubSubTopicConfig{"unjoined": {MessageID: "sha256"}}
		},
		"topic excludes subscription": func(c *GossipSubConfig) {
			c.SubscriptionFilter = &PubSubSubscriptionFilterConfig{Allowlist: []string{"other"}}
		},
		"subscription regexp": func(c *GossipSubConfig) { c.SubscriptionFilter = &PubSubSubscriptionFilterConfig{Pattern: "["} },
		"subscription conflict": func(c *GossipSubConfig) {
			c.SubscriptionFilter = &PubSubSubscriptionFilterConfig{Pattern: ".*", Allowlist: []string{"kpl/default"}}
		},
		"subscription limit": func(c *GossipSubConfig) {
			c.SubscriptionFilter = &PubSubSubscriptionFilterConfig{MaxSubscriptions: &zero}
		},
		"direct peer missing transport": func(c *GossipSubConfig) { c.DirectPeers = []string{"/ip4/127.0.0.1/tcp/1000"} },
		"direct peer wrong router":      func(c *GossipSubConfig) { c.Router = "floodsub"; c.DirectPeers = []string{"invalid"} },
		"peer filter":                   func(c *GossipSubConfig) { c.PeerFilter = &PubSubPeerFilterConfig{Deny: []string{"invalid"}} },
		"protocol features":             func(c *GossipSubConfig) { c.Protocols = []PubSubProtocolConfig{{ID: "/x", Features: []string{"px"}}} },
		"protocol duplicate":            func(c *GossipSubConfig) { c.Protocols = []PubSubProtocolConfig{{ID: "/x"}, {ID: "/x"}} },
		"protocol none conflict": func(c *GossipSubConfig) {
			c.Protocols = []PubSubProtocolConfig{{ID: "/x", Features: []string{"none", "mesh"}}}
		},
		"protocol match":   func(c *GossipSubConfig) { c.ProtocolMatch = "prefix" },
		"random protocols": func(c *GossipSubConfig) { c.Router = "randomsub"; c.Protocols = []PubSubProtocolConfig{{ID: "/x"}} },
		"blacklist ttl":    func(c *GossipSubConfig) { c.Blacklist = &PubSubBlacklistConfig{TTL: "0s"} },
		"gater nan":        func(c *GossipSubConfig) { c.PeerGater = &PeerGaterConfig{Threshold: &nan} },
		"gater quiet":      func(c *GossipSubConfig) { c.PeerGater = &PeerGaterConfig{Quiet: "1ms"} },
		"gater weights": func(c *GossipSubConfig) {
			c.PeerGater = &PeerGaterConfig{TopicDeliveryWeights: map[string]float64{"topic": math.Inf(1)}}
		},
		"validator result": func(c *GossipSubConfig) { c.DefaultValidators = []PubSubValidatorConfig{{Result: "unknown"}} },
		"validator bounds": func(c *GossipSubConfig) {
			c.DefaultValidators = []PubSubValidatorConfig{{MinBytes: &zero, MaxBytes: &negative}}
		},
		"validator concurrency":      func(c *GossipSubConfig) { c.DefaultValidators = []PubSubValidatorConfig{{Concurrency: &zero}} },
		"rpc negative":               func(c *GossipSubConfig) { c.RPCInspector = &PubSubRPCInspectorConfig{MaxMessages: &negative} },
		"disabled discovery options": func(c *GossipSubConfig) { c.Discovery = &PubSubDiscoveryConfig{Mode: "none", TTL: "1m"} },
		"discovery limit":            func(c *GossipSubConfig) { c.Discovery = &PubSubDiscoveryConfig{Mode: "routing", Limit: &zero} },
		"discovery backoff": func(c *GossipSubConfig) {
			c.Discovery = &PubSubDiscoveryConfig{Connector: &PubSubDiscoveryConnectorConfig{MinBackoff: "2h"}}
		},
		"unsigned author default id": func(c *GossipSubConfig) {
			c.SignaturePolicy = "strict-no-sign"
			c.Author = &PubSubAuthorConfig{NoAuthor: true}
		},
		"no author signing": func(c *GossipSubConfig) { c.Author = &PubSubAuthorConfig{NoAuthor: true}; c.MessageID = "sha256" },
		"publish readiness": func(c *GossipSubConfig) { c.Publish = &PubSubPublishConfig{ReadinessTimeout: "1s"} },
		"publish validator data no consumer": func(c *GossipSubConfig) {
			c.Publish = &PubSubPublishConfig{ValidatorData: map[string]any{"result": "ignore"}}
		},
		"publish identity": func(c *GossipSubConfig) {
			c.Publish = &PubSubPublishConfig{Author: &PubSubAuthorConfig{NoAuthor: true}}
		},
	}
	for name, apply := range tests {
		t.Run(name, func(t *testing.T) {
			config := NodeConfig{}.WithDefaults().GossipSub
			apply(&config)
			if err := config.ValidateExtended(); err == nil {
				t.Fatal("accepted invalid PubSub policy")
			}
		})
	}
}
func TestPubSubAuthorIdentityValidatesKeyAndPeerID(t *testing.T) {
	key, _, err := libcrypto.GenerateEd25519Key(nil)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := libcrypto.MarshalPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := peer.IDFromPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	config := PubSubAuthorConfig{PrivateKey: base64.StdEncoding.EncodeToString(wire), PeerID: expected.String()}
	decoded, id, err := config.Identity()
	if err != nil || id != expected || !key.Equals(decoded) {
		t.Fatalf("identity decode: %v %s", err, id)
	}
	other, _, err := libcrypto.GenerateEd25519Key(nil)
	if err != nil {
		t.Fatal(err)
	}
	otherID, _ := peer.IDFromPrivateKey(other)
	config.PeerID = otherID.String()
	if _, _, err := config.Identity(); err == nil {
		t.Fatal("accepted key/peer ID mismatch")
	}
	config.PrivateKey = "not-a-key"
	if _, _, err := config.Identity(); err == nil || strings.Contains(err.Error(), config.PrivateKey) {
		t.Fatalf("private key error leaked key: %v", err)
	}
}
