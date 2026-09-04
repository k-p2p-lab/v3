package distribution

import (
	"math/rand"
	"testing"
	"time"
)

func TestNormalSignedMeanAndZeroClipping(t *testing.T) {
	for _, mean := range []string{"-2ms", "0s", "2ms"} {
		d := Distribution{Model: "normal", Mean: mean, Sigma: "1ms"}
		if err := d.Validate(false); err != nil {
			t.Fatalf("mean %s: %v", mean, err)
		}
		left, right := rand.New(rand.NewSource(42)), rand.New(rand.NewSource(42))
		for i := 0; i < 100; i++ {
			a, b := d.Sample(left), d.Sample(right)
			if a < 0 || a != b {
				t.Fatalf("mean %s produced invalid/non-reproducible samples: %v/%v", mean, a, b)
			}
		}
	}
	negative := Distribution{Model: "normal", Mean: "-1s", Sigma: "0s"}
	if value := negative.Sample(rand.New(rand.NewSource(1))); value != 0 {
		t.Fatalf("negative sample = %v, want 0", value)
	}
	negative.Min = "1ms"
	if value := negative.Sample(rand.New(rand.NewSource(1))); value != time.Millisecond {
		t.Fatalf("minimum-clamped sample = %v", value)
	}
	if err := (Distribution{Model: "normal", Mean: "0s", Sigma: "-1ms"}).Validate(false); err == nil {
		t.Fatal("negative standard deviation accepted")
	}
}

func TestSharedDistributionsRemainSeededAndBounded(t *testing.T) {
	for _, d := range []Distribution{
		{Model: "fixed", Value: "3ms"},
		{Model: "exponential", Mean: "5ms"},
		{Model: "poisson", Mean: "50s"},
		{Model: "normal", Mean: "0s", Sigma: "5ms"},
		{Model: "pareto", XM: "1ms", Alpha: 2},
		{Model: "gamma", Alpha: 2, Scale: "2ms"},
		{Model: "lognormal", Mu: -3, LogSigma: 0.5},
	} {
		t.Run(d.Model, func(t *testing.T) {
			d.Min, d.Max = "1ms", "1s"
			if err := d.Validate(false); err != nil {
				t.Fatal(err)
			}
			left, right := rand.New(rand.NewSource(9)), rand.New(rand.NewSource(9))
			for i := 0; i < 100; i++ {
				a, b := d.Sample(left), d.Sample(right)
				if a != b || a < time.Millisecond || a > time.Second {
					t.Fatalf("samples = %v/%v", a, b)
				}
			}
		})
	}
}
