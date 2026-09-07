package model

import (
	"fmt"
	"net"
	"strings"
	"time"

	corepeer "github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

type KademliaConfig struct {
	Enabled                       *bool                        `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Mode                          string                       `json:"mode,omitempty" yaml:"mode,omitempty"`
	ProtocolPrefix                string                       `json:"protocolPrefix,omitempty" yaml:"protocolPrefix,omitempty"`
	ProtocolID                    string                       `json:"protocolId,omitempty" yaml:"protocolId,omitempty"`
	ProtocolExtension             string                       `json:"protocolExtension,omitempty" yaml:"protocolExtension,omitempty"`
	BucketSize                    *int                         `json:"bucketSize,omitempty" yaml:"bucketSize,omitempty"`
	Concurrency                   *int                         `json:"concurrency,omitempty" yaml:"concurrency,omitempty"`
	Resiliency                    *int                         `json:"resiliency,omitempty" yaml:"resiliency,omitempty"`
	LookupCheckConcurrency        *int                         `json:"lookupCheckConcurrency,omitempty" yaml:"lookupCheckConcurrency,omitempty"`
	RoutingTableLatencyTolerance  string                       `json:"routingTableLatencyTolerance,omitempty" yaml:"routingTableLatencyTolerance,omitempty"`
	RoutingTableRefreshPeriod     string                       `json:"routingTableRefreshPeriod,omitempty" yaml:"routingTableRefreshPeriod,omitempty"`
	RoutingTableRefreshTimeout    string                       `json:"routingTableRefreshTimeout,omitempty" yaml:"routingTableRefreshTimeout,omitempty"`
	MaxRecordAge                  string                       `json:"maxRecordAge,omitempty" yaml:"maxRecordAge,omitempty"`
	DisableAutoRefresh            *bool                        `json:"disableAutoRefresh,omitempty" yaml:"disableAutoRefresh,omitempty"`
	DisableProviders              *bool                        `json:"disableProviders,omitempty" yaml:"disableProviders,omitempty"`
	DisableValues                 *bool                        `json:"disableValues,omitempty" yaml:"disableValues,omitempty"`
	OptimisticProvide             *bool                        `json:"optimisticProvide,omitempty" yaml:"optimisticProvide,omitempty"`
	OptimisticProvideJobsPoolSize *int                         `json:"optimisticProvideJobsPoolSize,omitempty" yaml:"optimisticProvideJobsPoolSize,omitempty"`
	BootstrapTimeout              string                       `json:"bootstrapTimeout,omitempty" yaml:"bootstrapTimeout,omitempty"`
	BootstrapRetryInterval        string                       `json:"bootstrapRetryInterval,omitempty" yaml:"bootstrapRetryInterval,omitempty"`
	QueryFilter                   *KademliaFilterConfig        `json:"queryFilter,omitempty" yaml:"queryFilter,omitempty"`
	RoutingTableFilter            *KademliaFilterConfig        `json:"routingTableFilter,omitempty" yaml:"routingTableFilter,omitempty"`
	AddressFilter                 *KademliaFilterConfig        `json:"addressFilter,omitempty" yaml:"addressFilter,omitempty"`
	RoutingTableDiversity         *KademliaDiversityConfig     `json:"routingTableDiversity,omitempty" yaml:"routingTableDiversity,omitempty"`
	BootstrapSource               string                       `json:"bootstrapSource,omitempty" yaml:"bootstrapSource,omitempty"`
	BootstrapPeers                []string                     `json:"bootstrapPeers,omitempty" yaml:"bootstrapPeers,omitempty"`
	Datastore                     string                       `json:"datastore,omitempty" yaml:"datastore,omitempty"`
	Validator                     *KademliaValidatorConfig     `json:"validator,omitempty" yaml:"validator,omitempty"`
	ProviderStore                 *KademliaProviderStoreConfig `json:"providerStore,omitempty" yaml:"providerStore,omitempty"`
	RequestHook                   string                       `json:"requestHook,omitempty" yaml:"requestHook,omitempty"`
}

// Filters are local admission policies, independent of the Peer address advertised
// on its Swarm overlay. Explicit empty CIDR lists clear inherited restrictions.
type KademliaFilterConfig struct {
	Policy     string   `json:"policy,omitempty" yaml:"policy,omitempty"`
	AllowCIDRs []string `json:"allowCIDRs,omitempty" yaml:"allowCIDRs,omitempty"`
	DenyCIDRs  []string `json:"denyCIDRs,omitempty" yaml:"denyCIDRs,omitempty"`
}

type KademliaDiversityConfig struct {
	Enabled     *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	MaxPerCpl   *int  `json:"maxPerCpl,omitempty" yaml:"maxPerCpl,omitempty"`
	MaxForTable *int  `json:"maxForTable,omitempty" yaml:"maxForTable,omitempty"`
}

type KademliaValidatorConfig struct {
	Mode       string            `json:"mode,omitempty" yaml:"mode,omitempty"`
	Namespaces map[string]string `json:"namespaces,omitempty" yaml:"namespaces,omitempty"`
}

type KademliaProviderStoreConfig struct {
	Mode            string `json:"mode,omitempty" yaml:"mode,omitempty"`
	CleanupInterval string `json:"cleanupInterval,omitempty" yaml:"cleanupInterval,omitempty"`
	CacheSize       *int   `json:"cacheSize,omitempty" yaml:"cacheSize,omitempty"`
}

// Validate checks declarative extension policies. NodeConfig validates the
// shared scalar tuning fields and durations after applying its defaults.
func (c KademliaConfig) Validate() error {
	for name, filter := range map[string]*KademliaFilterConfig{
		"queryFilter": c.QueryFilter, "routingTableFilter": c.RoutingTableFilter, "addressFilter": c.AddressFilter,
	} {
		if filter == nil {
			continue
		}
		switch filter.Policy {
		case "", "all", "public", "private", "non-loopback":
		default:
			return fmt.Errorf("kademlia.%s.policy must be all, public, private or non-loopback", name)
		}
		for key, cidrs := range map[string][]string{"allowCIDRs": filter.AllowCIDRs, "denyCIDRs": filter.DenyCIDRs} {
			for _, cidr := range cidrs {
				if _, _, err := net.ParseCIDR(cidr); err != nil {
					return fmt.Errorf("kademlia.%s.%s: invalid CIDR %q", name, key, cidr)
				}
			}
		}
	}
	if d := c.RoutingTableDiversity; d != nil {
		if d.MaxPerCpl != nil && *d.MaxPerCpl <= 0 {
			return fmt.Errorf("kademlia.routingTableDiversity.maxPerCpl must be positive")
		}
		if d.MaxForTable != nil && *d.MaxForTable <= 0 {
			return fmt.Errorf("kademlia.routingTableDiversity.maxForTable must be positive")
		}
	}
	switch c.BootstrapSource {
	case "", "none", "static", "controller":
	default:
		return fmt.Errorf("kademlia.bootstrapSource must be none, static or controller")
	}
	if len(c.BootstrapPeers) > 0 && c.BootstrapSource != "static" {
		return fmt.Errorf("kademlia.bootstrapPeers requires bootstrapSource static")
	}
	for _, raw := range c.BootstrapPeers {
		address, err := ma.NewMultiaddr(raw)
		if err != nil {
			return fmt.Errorf("kademlia.bootstrapPeers: invalid multiaddr %q: %w", raw, err)
		}
		info, err := corepeer.AddrInfoFromP2pAddr(address)
		if err != nil || info.ID == "" || len(info.Addrs) == 0 {
			return fmt.Errorf("kademlia.bootstrapPeers: %q requires a transport address and /p2p Peer ID", raw)
		}
	}
	switch c.Datastore {
	case "", "memory", "null":
	default:
		return fmt.Errorf("kademlia.datastore must be memory or null")
	}
	if v := c.Validator; v != nil {
		switch v.Mode {
		case "", "default", "namespaced":
		default:
			return fmt.Errorf("kademlia.validator.mode must be default or namespaced")
		}
		for namespace, policy := range v.Namespaces {
			if namespace == "" || strings.ContainsAny(namespace, "/ \t\r\n") {
				return fmt.Errorf("kademlia.validator.namespaces: invalid namespace %q", namespace)
			}
			switch policy {
			case "public-key", "ipns", "bytes", "reject":
			default:
				return fmt.Errorf("kademlia.validator.namespaces.%s must be public-key, ipns, bytes or reject", namespace)
			}
			if policy == "public-key" && namespace != "pk" || policy == "ipns" && namespace != "ipns" {
				return fmt.Errorf("kademlia.validator.namespaces.%s: %s only validates its standard namespace", namespace, policy)
			}
			if v.Mode != "namespaced" && (namespace == "pk" && policy != "public-key" || namespace == "ipns" && policy != "ipns") {
				return fmt.Errorf("kademlia.validator: replacing %s requires mode namespaced", namespace)
			}
		}
	}
	if p := c.ProviderStore; p != nil {
		switch p.Mode {
		case "", "manager", "null":
		default:
			return fmt.Errorf("kademlia.providerStore.mode must be manager or null")
		}
		if p.Mode == "null" && (p.CleanupInterval != "" || p.CacheSize != nil) {
			return fmt.Errorf("kademlia.providerStore null does not use cleanupInterval or cacheSize")
		}
		if p.CleanupInterval != "" {
			interval, err := time.ParseDuration(p.CleanupInterval)
			if err != nil || interval <= 0 {
				return fmt.Errorf("kademlia.providerStore.cleanupInterval must be a positive duration")
			}
		}
		if p.CacheSize != nil && *p.CacheSize <= 0 {
			return fmt.Errorf("kademlia.providerStore.cacheSize must be positive")
		}
	}
	switch c.RequestHook {
	case "", "none", "log":
	default:
		return fmt.Errorf("kademlia.requestHook must be none or log")
	}
	// Private exact protocol IDs use a private upstream validation prefix.
	// The standard Amino wire ID retains its required tuning constraints.
	aminoProtocol := c.ProtocolPrefix+c.ProtocolExtension == "/ipfs"
	if c.ProtocolID != "" {
		aminoProtocol = c.ProtocolID == "/ipfs/kad/1.0.0"
	}
	if aminoProtocol {
		if c.BucketSize != nil && *c.BucketSize != 20 {
			return fmt.Errorf("kademlia /ipfs protocol requires bucketSize 20")
		}
		if c.DisableProviders != nil && *c.DisableProviders || c.DisableValues != nil && *c.DisableValues {
			return fmt.Errorf("kademlia /ipfs protocol requires providers and values enabled")
		}
		if v := c.Validator; v != nil {
			for namespace, policy := range v.Namespaces {
				if namespace != "pk" && namespace != "ipns" || namespace == "pk" && policy != "public-key" || namespace == "ipns" && policy != "ipns" {
					return fmt.Errorf("kademlia /ipfs protocol requires only standard pk and ipns validators")
				}
			}
			if v.Mode == "namespaced" && (len(v.Namespaces) != 2 || v.Namespaces["pk"] != "public-key" || v.Namespaces["ipns"] != "ipns") {
				return fmt.Errorf("kademlia /ipfs protocol requires both standard pk and ipns validators")
			}
		}
	}
	return nil
}
