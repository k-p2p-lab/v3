package scenario

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/elecbug/kpl-v3/internal/model"
	"gopkg.in/yaml.v3"
)

type Scenario struct {
	Version int     `json:"version" yaml:"version"`
	Name    string  `json:"name" yaml:"name"`
	Seed    int64   `json:"seed" yaml:"seed"`
	Phases  []Phase `json:"phases" yaml:"phases"`
}

type Phase struct {
	Name        string           `json:"name" yaml:"name"`
	Action      string           `json:"action" yaml:"action"`
	Group       string           `json:"group" yaml:"group"`
	Role        string           `json:"role" yaml:"role"`
	Count       int              `json:"count" yaml:"count"`
	Duration    string           `json:"duration" yaml:"duration"`
	Timeout     string           `json:"timeout" yaml:"timeout"`
	ReadyRatio  float64          `json:"readyRatio" yaml:"readyRatio"`
	PayloadSize int              `json:"payloadSize" yaml:"payloadSize"`
	Topic       string           `json:"topic" yaml:"topic"`
	Interval    Distribution     `json:"interval" yaml:"interval"`
	Lifetime    Distribution     `json:"lifetime" yaml:"lifetime"`
	Node        model.NodeConfig `json:"node" yaml:"node"`
}

type Distribution struct {
	Model string  `json:"model" yaml:"model"`
	Value string  `json:"value" yaml:"value"`
	Mean  string  `json:"mean" yaml:"mean"`
	Sigma string  `json:"sigma" yaml:"sigma"`
	XM    string  `json:"xm" yaml:"xm"`
	Alpha float64 `json:"alpha" yaml:"alpha"`
	Min   string  `json:"min" yaml:"min"`
	Max   string  `json:"max" yaml:"max"`
}

func Parse(data []byte) (Scenario, error) {
	var s Scenario
	if err := yaml.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("decode scenario: %w", err)
	}
	if err := s.Validate(); err != nil {
		return s, err
	}
	return s, nil
}

func (s *Scenario) Validate() error {
	if s.Version == 0 {
		s.Version = 1
	}
	if s.Version != 1 {
		return fmt.Errorf("unsupported scenario version %d", s.Version)
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("scenario name is required")
	}
	if len(s.Phases) == 0 {
		return fmt.Errorf("scenario must contain at least one phase")
	}
	for i := range s.Phases {
		p := &s.Phases[i]
		if p.Name == "" {
			p.Name = fmt.Sprintf("%s-%d", p.Action, i+1)
		}
		switch p.Action {
		case "join":
			if p.Group == "" || p.Count < 1 {
				return fmt.Errorf("phase %q: join requires group and positive count", p.Name)
			}
			if p.Role == "" {
				p.Role = "worker"
			}
			if p.Role != "boot" && p.Role != "worker" {
				return fmt.Errorf("phase %q: role must be boot or worker", p.Name)
			}
			if err := p.Interval.Validate(true); err != nil {
				return fmt.Errorf("phase %q interval: %w", p.Name, err)
			}
			if err := p.Lifetime.Validate(true); err != nil {
				return fmt.Errorf("phase %q lifetime: %w", p.Name, err)
			}
			p.Node = p.Node.WithDefaults()
			if p.Node.DLow > p.Node.D || p.Node.D > p.Node.DHigh {
				return fmt.Errorf("phase %q: node degree must satisfy dLow <= d <= dHigh", p.Name)
			}
			if p.Node.DOut < 0 || p.Node.DOut > p.Node.DLow {
				return fmt.Errorf("phase %q: dOut must be between 0 and dLow", p.Name)
			}
		case "wait":
			if _, err := time.ParseDuration(p.Duration); err != nil {
				return fmt.Errorf("phase %q: invalid duration: %w", p.Name, err)
			}
		case "wait-ready":
			if p.ReadyRatio == 0 {
				p.ReadyRatio = 1
			}
			if p.ReadyRatio <= 0 || p.ReadyRatio > 1 {
				return fmt.Errorf("phase %q: readyRatio must be in (0, 1]", p.Name)
			}
			if p.Timeout == "" {
				p.Timeout = "1m"
			}
			if _, err := time.ParseDuration(p.Timeout); err != nil {
				return fmt.Errorf("phase %q: invalid timeout: %w", p.Name, err)
			}
		case "publish":
			if p.Group == "" || p.Count < 1 {
				return fmt.Errorf("phase %q: publish requires group and positive count", p.Name)
			}
			if p.PayloadSize <= 0 {
				p.PayloadSize = 32
			}
			if err := p.Interval.Validate(true); err != nil {
				return fmt.Errorf("phase %q interval: %w", p.Name, err)
			}
		case "leave":
			if p.Group == "" || p.Count < 1 {
				return fmt.Errorf("phase %q: leave requires group and positive count", p.Name)
			}
			if err := p.Interval.Validate(true); err != nil {
				return fmt.Errorf("phase %q interval: %w", p.Name, err)
			}
		case "stop-all":
		default:
			return fmt.Errorf("phase %q: unknown action %q", p.Name, p.Action)
		}
	}
	return nil
}

func (d Distribution) Validate(optional bool) error {
	if d.Model == "" {
		if optional {
			return nil
		}
		return fmt.Errorf("model is required")
	}
	switch d.Model {
	case "fixed":
		_, err := parseDurationOrZero(d.Value)
		return err
	case "exponential":
		_, err := positiveDuration(d.Mean, "mean")
		return err
	case "normal":
		if _, err := positiveDuration(d.Mean, "mean"); err != nil {
			return err
		}
		_, err := parseDurationOrZero(d.Sigma)
		return err
	case "pareto":
		if _, err := positiveDuration(d.XM, "xm"); err != nil {
			return err
		}
		if d.Alpha <= 0 {
			return fmt.Errorf("alpha must be positive")
		}
		return nil
	default:
		return fmt.Errorf("unsupported model %q", d.Model)
	}
}

func (d Distribution) Sample(r *rand.Rand) time.Duration {
	var value time.Duration
	switch d.Model {
	case "":
		return 0
	case "fixed":
		value, _ = time.ParseDuration(d.Value)
	case "exponential":
		mean, _ := time.ParseDuration(d.Mean)
		value = time.Duration(r.ExpFloat64() * float64(mean))
	case "normal":
		mean, _ := time.ParseDuration(d.Mean)
		sigma, _ := time.ParseDuration(d.Sigma)
		value = time.Duration(float64(mean) + r.NormFloat64()*float64(sigma))
	case "pareto":
		xm, _ := time.ParseDuration(d.XM)
		u := math.Max(r.Float64(), math.SmallestNonzeroFloat64)
		value = time.Duration(float64(xm) / math.Pow(u, 1/d.Alpha))
	}
	if value < 0 {
		value = 0
	}
	if d.Min != "" {
		min, _ := time.ParseDuration(d.Min)
		if value < min {
			value = min
		}
	}
	if d.Max != "" {
		max, _ := time.ParseDuration(d.Max)
		if max > 0 && value > max {
			value = max
		}
	}
	return value
}

func parseDurationOrZero(value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", value)
	}
	if d < 0 {
		return 0, fmt.Errorf("duration cannot be negative")
	}
	return d, nil
}

func positiveDuration(value, name string) (time.Duration, error) {
	d, err := parseDurationOrZero(value)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return d, nil
}
