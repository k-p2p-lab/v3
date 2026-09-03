package scenario

import (
	"math/rand"
	"testing"
	"time"
)

func TestParseAndDefaults(t *testing.T) {
	s, err := Parse([]byte(`
version: 1
name: smoke
seed: 42
phases:
  - action: join
    group: boot
    role: boot
    count: 1
  - action: wait-ready
    group: boot
  - action: publish
    group: boot
    count: 1
`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Phases[0].Node.D != 6 || s.Phases[1].ReadyRatio != 1 {
		t.Fatalf("defaults were not applied: %+v", s.Phases)
	}
}

func TestDistributionIsDeterministicAndClamped(t *testing.T) {
	d := Distribution{Model: "normal", Mean: "10ms", Sigma: "100ms", Min: "2ms", Max: "20ms"}
	a := d.Sample(rand.New(rand.NewSource(7)))
	b := d.Sample(rand.New(rand.NewSource(7)))
	if a != b {
		t.Fatalf("same seed produced %v and %v", a, b)
	}
	if a < 2*time.Millisecond || a > 20*time.Millisecond {
		t.Fatalf("sample outside clamp: %v", a)
	}
}
