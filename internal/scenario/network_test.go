package scenario

import (
	"strings"
	"testing"
)

func TestNetworkProfileAndPhaseOverrides(t *testing.T) {
	scenario, err := Parse([]byte(`
version: 2
name: network-profiles
profiles:
  wan:
    network:
      delay: 80ms
      jitter: 8ms
      lossPercent: 2.5
      duplicatePercent: 1
      rateMbps: 20
phases:
  - action: join
    group: slow
    profile: wan
    count: 2
  - action: join
    group: fast
    profile: wan
    count: 1
    node:
      network:
        delay: 0s
        jitter: 0s
        lossPercent: 0
        duplicatePercent: 0
        rateMbps: 0
`))
	if err != nil {
		t.Fatal(err)
	}
	slow, fast := scenario.Phases[0].Node.Network, scenario.Phases[1].Node.Network
	if slow.Delay != "80ms" || slow.Jitter != "8ms" || *slow.LossPercent != 2.5 || *slow.RateMbps != 20 {
		t.Fatalf("profile impairments lost: %+v", slow)
	}
	if fast.Enabled() || fast.LossPercent == nil || fast.RateMbps == nil {
		t.Fatalf("explicit phase zeros did not disable inherited impairments: %+v", fast)
	}
}

func TestScenarioRejectsInvalidNetworkBeforeExecution(t *testing.T) {
	for _, field := range []string{"lossPercent: 101", "lossPercent: .nan", "delay: -2ms", "jitter: 5ms", "reorderPercent: 10", "rateMbps: -2", "queueLimit: 0"} {
		t.Run(field, func(t *testing.T) {
			_, err := Parse([]byte("name: invalid-network\nphases:\n  - action: join\n    group: peers\n    count: 1\n    node:\n      network:\n        " + field + "\n"))
			if err == nil || !strings.Contains(err.Error(), "network:") {
				t.Fatalf("invalid impairment %q accepted: %v", field, err)
			}
		})
	}
}
