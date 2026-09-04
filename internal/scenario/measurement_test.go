package scenario

import (
	"testing"
)

func TestPublishDeliveryWindow(t *testing.T) {
	for _, value := range []string{"", "250ms", "10s", "1h"} {
		s := Scenario{Name: "window", Phases: []Phase{{Action: "publish", Group: "worker", Count: 1, DeliveryWindow: value}}}
		if err := s.Validate(); err != nil {
			t.Fatalf("valid window %q: %v", value, err)
		}
		if value == "" && s.Phases[0].DeliveryWindow != "10s" {
			t.Fatal("default window was not recorded")
		}
	}
	for _, value := range []string{"0s", "-1s", "forever", "1h1ns"} {
		s := Scenario{Name: "window", Phases: []Phase{{Action: "publish", Group: "worker", Count: 1, DeliveryWindow: value}}}
		if err := s.Validate(); err == nil {
			t.Fatalf("invalid window %q accepted", value)
		}
	}
	s := Scenario{Name: "window", Phases: []Phase{{Action: "wait", Duration: "1s", DeliveryWindow: "10s"}}}
	if err := s.Validate(); err == nil {
		t.Fatal("deliveryWindow accepted outside publish")
	}
}
