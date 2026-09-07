package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestKademliaDeclarativeOptionsRoundTripAndOverride(t *testing.T) {
	raw := `queryFilter:
  policy: private
  allowCIDRs: [10.42.0.0/16]
  denyCIDRs: [10.42.9.0/24]
routingTableFilter:
  policy: all
addressFilter:
  policy: non-loopback
routingTableDiversity:
  enabled: true
  maxPerCpl: 4
  maxForTable: 12
bootstrapSource: controller
datastore: memory
validator:
  mode: namespaced
  namespaces: {lab: bytes, blocked: reject}
providerStore:
  mode: manager
  cleanupInterval: 45s
  cacheSize: 32
requestHook: log
`
	var options KademliaConfig
	decoder := yaml.NewDecoder(strings.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&options); err != nil {
		t.Fatal(err)
	}
	if err := options.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(options)
	if err != nil {
		t.Fatal(err)
	}
	var copy KademliaConfig
	if err := json.Unmarshal(encoded, &copy); err != nil || !reflect.DeepEqual(copy, options) {
		t.Fatalf("options lost in Peer JSON: %+v %v", copy, err)
	}
	disabled := false
	base := NodeConfig{Kademlia: options}
	merged := base.Merge(NodeConfig{Kademlia: KademliaConfig{
		QueryFilter:           &KademliaFilterConfig{Policy: "all", AllowCIDRs: []string{}, DenyCIDRs: []string{}},
		RoutingTableDiversity: &KademliaDiversityConfig{Enabled: &disabled},
		Validator:             &KademliaValidatorConfig{Mode: "default"},
	}})
	if len(merged.Kademlia.QueryFilter.AllowCIDRs) != 0 || len(merged.Kademlia.QueryFilter.DenyCIDRs) != 0 || *merged.Kademlia.RoutingTableDiversity.Enabled || merged.Kademlia.Validator.Mode != "default" {
		t.Fatalf("explicit override retained prior policy: %+v", merged.Kademlia)
	}
	if len(base.Kademlia.QueryFilter.AllowCIDRs) != 1 || !*base.Kademlia.RoutingTableDiversity.Enabled {
		t.Fatal("merge mutated source options")
	}
}

func TestKademliaDeclarativeOptionsRejectInvalidPolicies(t *testing.T) {
	zero := 0
	for name, config := range map[string]KademliaConfig{
		"query policy":                    {QueryFilter: &KademliaFilterConfig{Policy: "arbitrary-code"}},
		"route CIDR":                      {RoutingTableFilter: &KademliaFilterConfig{AllowCIDRs: []string{"10.0.0.1"}}},
		"address CIDR":                    {AddressFilter: &KademliaFilterConfig{DenyCIDRs: []string{"bad/2"}}},
		"diversity":                       {RoutingTableDiversity: &KademliaDiversityConfig{MaxForTable: &zero}},
		"bootstrap source":                {BootstrapSource: "ipfs"},
		"implicit static":                 {BootstrapPeers: []string{"/ip4/10.0.0.1/tcp/20000"}},
		"bootstrap peer":                  {BootstrapSource: "static", BootstrapPeers: []string{"/ip4/10.0.0.1/tcp/20000"}},
		"datastore":                       {Datastore: "host-path"},
		"validator mode":                  {Validator: &KademliaValidatorConfig{Mode: "js"}},
		"namespace":                       {Validator: &KademliaValidatorConfig{Namespaces: map[string]string{"/lab": "bytes"}}},
		"namespace policy":                {Validator: &KademliaValidatorConfig{Namespaces: map[string]string{"lab": "js"}}},
		"default pk override":             {Validator: &KademliaValidatorConfig{Namespaces: map[string]string{"pk": "bytes"}}},
		"provider mode":                   {ProviderStore: &KademliaProviderStoreConfig{Mode: "host-path"}},
		"null provider tuning":            {ProviderStore: &KademliaProviderStoreConfig{Mode: "null", CleanupInterval: "1s"}},
		"provider interval":               {ProviderStore: &KademliaProviderStoreConfig{CleanupInterval: "0s"}},
		"provider cache":                  {ProviderStore: &KademliaProviderStoreConfig{CacheSize: &zero}},
		"request hook":                    {RequestHook: "exec"},
		"public-key custom namespace":     {Validator: &KademliaValidatorConfig{Mode: "namespaced", Namespaces: map[string]string{"lab": "public-key"}}},
		"ipns custom namespace":           {Validator: &KademliaValidatorConfig{Mode: "namespaced", Namespaces: map[string]string{"lab": "ipns"}}},
		"public protocol validator":       {ProtocolPrefix: "/ipfs", Validator: &KademliaValidatorConfig{Mode: "namespaced", Namespaces: map[string]string{"lab": "bytes"}}},
		"public protocol extra validator": {ProtocolPrefix: "/ipfs", Validator: &KademliaValidatorConfig{Namespaces: map[string]string{"lab": "bytes"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := config.Validate(); err == nil {
				t.Fatalf("invalid options accepted: %+v", config)
			}
		})
	}
	if err := (KademliaConfig{ProtocolPrefix: "/ipfs", Validator: &KademliaValidatorConfig{Mode: "namespaced", Namespaces: map[string]string{"pk": "public-key", "ipns": "ipns"}}}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestKademliaExactProtocolValidationMatchesWireIdentity(t *testing.T) {
	twentyFour, disabled := 24, true
	for name, tuning := range map[string]KademliaConfig{
		"bucket size":        {BucketSize: &twentyFour},
		"disabled values":    {DisableValues: &disabled},
		"disabled providers": {DisableProviders: &disabled},
		"custom validator":   {Validator: &KademliaValidatorConfig{Mode: "namespaced", Namespaces: map[string]string{"lab": "bytes"}}},
	} {
		t.Run(name, func(t *testing.T) {
			for _, exact := range []string{"/lab/exact/9.9.9", "/ipfs", "/ipfs/kad/1.0.0"} {
				tuning.ProtocolID = exact
				err := (NodeConfig{Kademlia: tuning}).Validate()
				if (err != nil) != (exact == "/ipfs/kad/1.0.0") {
					t.Fatalf("protocolId=%q validation=%v", exact, err)
				}
			}
		})
	}
	if err := (NodeConfig{Kademlia: KademliaConfig{ProtocolID: "/ipfs/kad/1.0.0"}}).Validate(); err != nil {
		t.Fatal(err)
	}
}
