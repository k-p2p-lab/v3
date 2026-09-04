package model

import (
	"fmt"
	"math"
	"time"
)

// NetworkConfig configures Linux netem on a peer container's outbound P2P
// traffic. Delay and jitter are Go durations; jitter uses a normal distribution.
// Pointer fields preserve explicit zero overrides when merging node profiles.
// A delay of "0s" and percentages/rate of zero disable the respective setting.
type NetworkConfig struct {
	Delay            string   `json:"delay,omitempty" yaml:"delay,omitempty"`
	Jitter           string   `json:"jitter,omitempty" yaml:"jitter,omitempty"`
	LossPercent      *float64 `json:"lossPercent,omitempty" yaml:"lossPercent,omitempty"`
	DuplicatePercent *float64 `json:"duplicatePercent,omitempty" yaml:"duplicatePercent,omitempty"`
	CorruptPercent   *float64 `json:"corruptPercent,omitempty" yaml:"corruptPercent,omitempty"`
	ReorderPercent   *float64 `json:"reorderPercent,omitempty" yaml:"reorderPercent,omitempty"`
	RateMbps         *float64 `json:"rateMbps,omitempty" yaml:"rateMbps,omitempty"`
	QueueLimit       *int     `json:"queueLimit,omitempty" yaml:"queueLimit,omitempty"`
}

// Validate rejects unsupported values before a container or qdisc is created.
func (c NetworkConfig) Validate() error {
	delay, err := networkDuration("delay", c.Delay)
	if err != nil {
		return err
	}
	jitter, err := networkDuration("jitter", c.Jitter)
	if err != nil {
		return err
	}
	if jitter > 0 && delay == 0 {
		return fmt.Errorf("jitter requires a positive delay")
	}
	for _, field := range []struct {
		name  string
		value *float64
	}{
		{"lossPercent", c.LossPercent},
		{"duplicatePercent", c.DuplicatePercent},
		{"corruptPercent", c.CorruptPercent},
		{"reorderPercent", c.ReorderPercent},
	} {
		if field.value != nil && (math.IsNaN(*field.value) || math.IsInf(*field.value, 0) || *field.value < 0 || *field.value > 100) {
			return fmt.Errorf("%s must be finite and between 0 and 100", field.name)
		}
	}
	if c.ReorderPercent != nil && *c.ReorderPercent > 0 && delay == 0 {
		return fmt.Errorf("reorderPercent requires a positive delay")
	}
	if c.RateMbps != nil && (math.IsNaN(*c.RateMbps) || math.IsInf(*c.RateMbps, 0) || *c.RateMbps < 0) {
		return fmt.Errorf("rateMbps must be finite and non-negative (0 disables the limit)")
	}
	if c.QueueLimit != nil && (*c.QueueLimit <= 0 || uint64(*c.QueueLimit) > math.MaxUint32) {
		return fmt.Errorf("queueLimit must be between 1 and %d packets", uint64(math.MaxUint32))
	}
	return nil
}

// Enabled reports whether at least one impairment is configured. Call Validate
// first when accepting user input: this method does not report invalid values.
func (c NetworkConfig) Enabled() bool {
	delay, _ := time.ParseDuration(c.Delay)
	jitter, _ := time.ParseDuration(c.Jitter)
	if delay != 0 || jitter != 0 || c.QueueLimit != nil {
		return true
	}
	for _, value := range []*float64{c.LossPercent, c.DuplicatePercent, c.CorruptPercent, c.ReorderPercent, c.RateMbps} {
		if value != nil && *value != 0 {
			return true
		}
	}
	return false
}

func networkDuration(name, value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 {
		return 0, fmt.Errorf("%s must be a non-negative Go duration, got %q", name, value)
	}
	return duration, nil
}
