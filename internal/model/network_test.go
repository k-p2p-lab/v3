package model

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestNetworkConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    NetworkConfig
		wantError string
	}{
		{"empty", NetworkConfig{}, ""},
		{"all impairments", NetworkConfig{Delay: "100ms", Jitter: "10ms", LossPercent: float64Ptr(100), DuplicatePercent: float64Ptr(1), CorruptPercent: float64Ptr(0.1), ReorderPercent: float64Ptr(2), RateMbps: float64Ptr(10), QueueLimit: intPtr(1000)}, ""},
		{"explicit zeros", NetworkConfig{Delay: "0s", Jitter: "0s", LossPercent: float64Ptr(0), ReorderPercent: float64Ptr(0), RateMbps: float64Ptr(0)}, ""},
		{"invalid delay", NetworkConfig{Delay: "1s; rm -rf /"}, "delay"},
		{"negative delay", NetworkConfig{Delay: "-1ms"}, "delay"},
		{"invalid jitter", NetworkConfig{Delay: "1s", Jitter: "not-time"}, "jitter"},
		{"negative jitter", NetworkConfig{Delay: "1s", Jitter: "-1ns"}, "jitter"},
		{"jitter requires delay", NetworkConfig{Jitter: "1ms"}, "jitter requires"},
		{"reorder requires delay", NetworkConfig{ReorderPercent: float64Ptr(1)}, "reorderPercent requires"},
		{"loss out of range", NetworkConfig{LossPercent: float64Ptr(101)}, "lossPercent"},
		{"negative loss", NetworkConfig{LossPercent: float64Ptr(-1)}, "lossPercent"},
		{"duplicate nan", NetworkConfig{DuplicatePercent: float64Ptr(math.NaN())}, "duplicatePercent"},
		{"corrupt inf", NetworkConfig{CorruptPercent: float64Ptr(math.Inf(1))}, "corruptPercent"},
		{"reorder out of range", NetworkConfig{ReorderPercent: float64Ptr(101)}, "reorderPercent"},
		{"negative rate", NetworkConfig{RateMbps: float64Ptr(-1)}, "rateMbps"},
		{"infinite rate", NetworkConfig{RateMbps: float64Ptr(math.Inf(1))}, "rateMbps"},
		{"nan rate", NetworkConfig{RateMbps: float64Ptr(math.NaN())}, "rateMbps"},
		{"zero queue", NetworkConfig{QueueLimit: intPtr(0)}, "queueLimit"},
		{"negative queue", NetworkConfig{QueueLimit: intPtr(-1)}, "queueLimit"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.config.Validate()
			if test.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Validate() = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestNetworkConfigEnabled(t *testing.T) {
	for _, test := range []struct {
		config  NetworkConfig
		enabled bool
	}{
		{NetworkConfig{}, false},
		{NetworkConfig{Delay: "0s", Jitter: "0s", LossPercent: float64Ptr(0), DuplicatePercent: float64Ptr(0), CorruptPercent: float64Ptr(0), ReorderPercent: float64Ptr(0), RateMbps: float64Ptr(0)}, false},
		{NetworkConfig{Delay: "1ns"}, true},
		{NetworkConfig{LossPercent: float64Ptr(100)}, true},
		{NetworkConfig{DuplicatePercent: float64Ptr(1)}, true},
		{NetworkConfig{CorruptPercent: float64Ptr(1)}, true},
		{NetworkConfig{Delay: "1ms", ReorderPercent: float64Ptr(1)}, true},
		{NetworkConfig{RateMbps: float64Ptr(1)}, true},
		{NetworkConfig{QueueLimit: intPtr(1)}, true},
	} {
		if actual := test.config.Enabled(); actual != test.enabled {
			t.Errorf("Enabled(%+v) = %v, want %v", test.config, actual, test.enabled)
		}
	}
}

func TestNetworkOverridesPreserveExplicitZeroAndJSON(t *testing.T) {
	base := NodeConfig{Network: NetworkConfig{Delay: "100ms", Jitter: "10ms", LossPercent: float64Ptr(5), DuplicatePercent: float64Ptr(2), RateMbps: float64Ptr(10)}}
	merged := base.Merge(NodeConfig{Network: NetworkConfig{Delay: "0s", Jitter: "0s", LossPercent: float64Ptr(0), RateMbps: float64Ptr(0)}}).WithDefaults()
	if merged.Network.Delay != "0s" || merged.Network.Jitter != "0s" || *merged.Network.LossPercent != 0 || *merged.Network.RateMbps != 0 || *merged.Network.DuplicatePercent != 2 {
		t.Fatalf("incorrect network overlay: %+v", merged.Network)
	}
	if base.Network.Delay != "100ms" || *base.Network.LossPercent != 5 {
		t.Fatal("merge modified the base profile")
	}
	data, err := json.Marshal(merged)
	if err != nil {
		t.Fatal(err)
	}
	var restored NodeConfig
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Network.LossPercent == nil || *restored.Network.LossPercent != 0 || restored.Network.RateMbps == nil || *restored.Network.RateMbps != 0 {
		t.Fatalf("network zero overrides were lost over the agent JSON API: %s", data)
	}
}

func TestNodeValidationIncludesNetwork(t *testing.T) {
	if err := (NodeConfig{Network: NetworkConfig{LossPercent: float64Ptr(101)}}).Validate(); err == nil || !strings.Contains(err.Error(), "network: lossPercent") {
		t.Fatalf("NodeConfig.Validate() = %v", err)
	}
}
