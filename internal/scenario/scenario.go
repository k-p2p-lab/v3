package scenario

import (
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/k-p2p-lab/v3/internal/model"
	"gopkg.in/yaml.v3"
)

type Scenario struct {
	Version            int                         `json:"version" yaml:"version"`
	Name               string                      `json:"name" yaml:"name"`
	Seed               int64                       `json:"seed" yaml:"seed"`
	OnExit             string                      `json:"onExit,omitempty" yaml:"onExit,omitempty"`
	JobShutdownTimeout string                      `json:"jobShutdownTimeout,omitempty" yaml:"jobShutdownTimeout,omitempty"`
	Profiles           map[string]model.NodeConfig `json:"profiles,omitempty" yaml:"profiles,omitempty"`
	Phases             []Phase                     `json:"phases" yaml:"phases"`
}

type Phase struct {
	Name        string           `json:"name" yaml:"name"`
	Job         string           `json:"job,omitempty" yaml:"job,omitempty"`
	Jobs        []string         `json:"jobs,omitempty" yaml:"jobs,omitempty"`
	Action      string           `json:"action" yaml:"action"`
	Group       string           `json:"group" yaml:"group"`
	Role        string           `json:"role" yaml:"role"`
	Profile     string           `json:"profile,omitempty" yaml:"profile,omitempty"`
	NodeType    string           `json:"type,omitempty" yaml:"type,omitempty"`
	Count       int              `json:"count" yaml:"count"`
	Repeat      int              `json:"repeat,omitempty" yaml:"repeat,omitempty"`
	Parallel    bool             `json:"parallel,omitempty" yaml:"parallel,omitempty"`
	Parallelism int              `json:"parallelism,omitempty" yaml:"parallelism,omitempty"`
	Await       *bool            `json:"await,omitempty" yaml:"await,omitempty"`
	Duration    string           `json:"duration" yaml:"duration"`
	Timeout     string           `json:"timeout" yaml:"timeout"`
	Message     string           `json:"message,omitempty" yaml:"message,omitempty"`
	ReadyRatio  float64          `json:"readyRatio" yaml:"readyRatio"`
	MinCount    int              `json:"minCount,omitempty" yaml:"minCount,omitempty"`
	PayloadSize int              `json:"payloadSize" yaml:"payloadSize"`
	Topic       string           `json:"topic" yaml:"topic"`
	Interval    Distribution     `json:"interval" yaml:"interval"`
	Lifetime    Distribution     `json:"lifetime" yaml:"lifetime"`
	Node        model.NodeConfig `json:"node" yaml:"node"`
}

type Distribution struct {
	Model    string  `json:"model" yaml:"model"`
	Value    string  `json:"value" yaml:"value"`
	Mean     string  `json:"mean" yaml:"mean"`
	Sigma    string  `json:"sigma" yaml:"sigma"`
	XM       string  `json:"xm" yaml:"xm"`
	Alpha    float64 `json:"alpha" yaml:"alpha"`
	Beta     float64 `json:"beta" yaml:"beta"`
	Scale    string  `json:"scale" yaml:"scale"`
	Mu       float64 `json:"mu" yaml:"mu"`
	LogSigma float64 `json:"logSigma" yaml:"logSigma"`
	Min      string  `json:"min" yaml:"min"`
	Max      string  `json:"max" yaml:"max"`
}

func Parse(data []byte) (Scenario, error) {
	var s Scenario
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&s); err != nil {
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
	if s.Version != 1 && s.Version != 2 {
		return fmt.Errorf("unsupported scenario version %d", s.Version)
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("scenario name is required")
	}
	if len(s.Phases) == 0 {
		return fmt.Errorf("scenario must contain at least one phase")
	}
	if s.OnExit == "" {
		s.OnExit = "cancel"
	}
	if s.OnExit != "cancel" && s.OnExit != "drain" {
		return fmt.Errorf("onExit must be cancel or drain")
	}
	if s.JobShutdownTimeout == "" {
		s.JobShutdownTimeout = "10s"
	}
	shutdownTimeout, err := time.ParseDuration(s.JobShutdownTimeout)
	if err != nil {
		return fmt.Errorf("invalid jobShutdownTimeout: %w", err)
	}
	if shutdownTimeout <= 0 {
		return fmt.Errorf("jobShutdownTimeout must be positive")
	}
	backgroundJoins := make(map[string]map[string]struct{})
	knownJobs := make(map[string]struct{})
	for i := range s.Phases {
		p := &s.Phases[i]
		if p.Action == "sleep" {
			p.Action = "wait"
		}
		if p.Action == "reset" {
			p.Action = "stop-all"
		}
		if p.Name == "" {
			p.Name = fmt.Sprintf("%s-%d", p.Action, i+1)
		}
		if p.NodeType != "" {
			if preset, ok := model.BuiltInNodeConfig(p.NodeType); ok {
				p.NodeType = preset.Type
			}
		}
		if p.Job == "" {
			p.Job = fmt.Sprintf("phase-%d", i+1)
		}
		if p.Repeat == 0 {
			p.Repeat = 1
		}
		if p.Repeat < 1 {
			return fmt.Errorf("phase %q: repeat must be positive", p.Name)
		}
		if p.Parallelism < 0 {
			return fmt.Errorf("phase %q: parallelism cannot be negative", p.Name)
		}
		if len(p.Jobs) > 0 && p.Action != "wait-ready" && p.Action != "wait-jobs" {
			return fmt.Errorf("phase %q: jobs is only supported for wait-ready and wait-jobs", p.Name)
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
			if err := s.resolveNodeConfig(p); err != nil {
				return fmt.Errorf("phase %q: %w", p.Name, err)
			}
			if !p.ShouldAwait() {
				if backgroundJoins[p.Group] == nil {
					backgroundJoins[p.Group] = make(map[string]struct{})
				}
				backgroundJoins[p.Group][p.Job] = struct{}{}
			}
		case "wait":
			duration, err := time.ParseDuration(p.Duration)
			if err != nil {
				return fmt.Errorf("phase %q: invalid duration: %w", p.Name, err)
			}
			if duration <= 0 {
				return fmt.Errorf("phase %q: duration must be positive", p.Name)
			}
		case "wait-ready":
			if p.MinCount < 0 {
				return fmt.Errorf("phase %q: minCount cannot be negative", p.Name)
			}
			for _, job := range p.Jobs {
				if _, exists := knownJobs[job]; !exists {
					return fmt.Errorf("phase %q: job %q has not been started", p.Name, job)
				}
			}
			if job := firstUnwaitedBackgroundJoin(backgroundJoins, p.Group, p.Jobs); job != "" && p.MinCount == 0 {
				return fmt.Errorf("phase %q: wait-ready after background join %q requires minCount or all relevant join jobs", p.Name, job)
			}
			removeBackgroundJoinJobs(backgroundJoins, p.Jobs)
			if p.ReadyRatio == 0 {
				p.ReadyRatio = 1
			}
			if p.ReadyRatio <= 0 || p.ReadyRatio > 1 {
				return fmt.Errorf("phase %q: readyRatio must be in (0, 1]", p.Name)
			}
			if p.Timeout == "" {
				p.Timeout = "1m"
			}
			timeout, err := time.ParseDuration(p.Timeout)
			if err != nil {
				return fmt.Errorf("phase %q: invalid timeout: %w", p.Name, err)
			}
			if timeout <= 0 {
				return fmt.Errorf("phase %q: timeout must be positive", p.Name)
			}
		case "wait-jobs":
			if !p.ShouldAwait() {
				return fmt.Errorf("phase %q: wait-jobs must use await: true", p.Name)
			}
			if p.Timeout == "" {
				p.Timeout = "5m"
			}
			timeout, err := time.ParseDuration(p.Timeout)
			if err != nil {
				return fmt.Errorf("phase %q: invalid timeout: %w", p.Name, err)
			}
			if timeout <= 0 {
				return fmt.Errorf("phase %q: timeout must be positive", p.Name)
			}
			if len(p.Jobs) == 0 {
				clear(backgroundJoins)
			} else {
				for _, job := range p.Jobs {
					if _, exists := knownJobs[job]; !exists {
						return fmt.Errorf("phase %q: job %q has not been started", p.Name, job)
					}
				}
				removeBackgroundJoinJobs(backgroundJoins, p.Jobs)
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
		case "stop-all", "log":
			if p.Action == "stop-all" {
				clear(backgroundJoins)
				clear(knownJobs)
			}
		default:
			return fmt.Errorf("phase %q: unknown action %q", p.Name, p.Action)
		}
		if !p.ShouldAwait() && p.Action != "join" && p.Action != "publish" && p.Action != "leave" {
			return fmt.Errorf("phase %q: await:false is only supported for join, publish, and leave", p.Name)
		}
		if p.Parallel && p.Action != "join" && p.Action != "publish" && p.Action != "leave" {
			return fmt.Errorf("phase %q: parallel is only supported for join, publish, and leave", p.Name)
		}
		if !p.ShouldAwait() {
			if _, exists := knownJobs[p.Job]; exists {
				return fmt.Errorf("phase %q: duplicate background job %q", p.Name, p.Job)
			}
			knownJobs[p.Job] = struct{}{}
		}
	}
	return nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func firstUnwaitedBackgroundJoin(background map[string]map[string]struct{}, group string, waited []string) string {
	var candidates []string
	for candidateGroup, jobs := range background {
		if group != "" && candidateGroup != group {
			continue
		}
		for job := range jobs {
			if !contains(waited, job) {
				candidates = append(candidates, job)
			}
		}
	}
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func removeBackgroundJoinJobs(background map[string]map[string]struct{}, waited []string) {
	for group, jobs := range background {
		for _, job := range waited {
			delete(jobs, job)
		}
		if len(jobs) == 0 {
			delete(background, group)
		}
	}
}

func (s *Scenario) resolveNodeConfig(phase *Phase) error {
	kind := phase.NodeType
	profile, hasProfile := s.Profiles[phase.Profile]
	if kind == "" && hasProfile && profile.Type != "" {
		kind = profile.Type
	}
	if kind == "" && phase.Profile != "" && !hasProfile {
		kind = phase.Profile
	}
	if kind == "" {
		if phase.Role == "boot" {
			kind = "boot"
		} else {
			kind = "full"
		}
	}
	base, builtIn := model.BuiltInNodeConfig(kind)
	if !builtIn {
		base, _ = model.BuiltInNodeConfig("full")
		base.Type = kind
	}
	if phase.Profile != "" {
		if hasProfile {
			base = base.Merge(profile)
		} else if !builtIn {
			return fmt.Errorf("unknown profile %q", phase.Profile)
		}
	}
	base = base.Merge(phase.Node)
	if phase.NodeType != "" {
		base.Type = phase.NodeType
	}
	// Built-in aliases are accepted at every overlay level, but the resolved
	// lifecycle type must remain canonical. Otherwise a custom profile with
	// `type: worker` creates a `full` peer at the Agent while later phase
	// filters keep looking for the non-canonical `worker` type.
	if preset, ok := model.BuiltInNodeConfig(base.Type); ok {
		base.Type = preset.Type
	}
	base = base.WithDefaults()
	if err := base.Validate(); err != nil {
		return err
	}
	phase.Node = base
	phase.NodeType = base.Type
	if phase.Profile == "" {
		phase.Profile = base.Type
	}
	return nil
}

func (p Phase) ShouldAwait() bool {
	return p.Await == nil || *p.Await
}

func (d Distribution) Validate(optional bool) error {
	if d.Model == "" {
		if optional {
			return nil
		}
		return fmt.Errorf("model is required")
	}
	var err error
	switch d.Model {
	case "fixed":
		_, err = parseDurationOrZero(d.Value)
	case "exponential", "poisson":
		_, err = positiveDuration(d.Mean, "mean")
	case "normal":
		if _, err = positiveDuration(d.Mean, "mean"); err == nil {
			_, err = parseDurationOrZero(d.Sigma)
		}
	case "pareto":
		if _, err = positiveDuration(d.XM, "xm"); err == nil && !finitePositive(d.Alpha) {
			err = fmt.Errorf("alpha must be positive and finite")
		}
	case "gamma":
		if !finitePositive(d.Alpha) {
			err = fmt.Errorf("alpha must be positive and finite")
		} else if d.Scale != "" && d.Beta != 0 {
			err = fmt.Errorf("gamma scale and beta are mutually exclusive")
		} else if d.Scale != "" {
			_, err = positiveDuration(d.Scale, "scale")
		} else if !finitePositive(d.Beta) {
			err = fmt.Errorf("gamma requires a positive finite beta rate or scale duration")
		}
	case "lognormal":
		if d.Sigma != "" && d.LogSigma != 0 {
			err = fmt.Errorf("lognormal sigma and logSigma are mutually exclusive")
		} else if logSigma, sigmaErr := d.lognormalSigma(); sigmaErr != nil {
			err = sigmaErr
		} else if math.IsNaN(d.Mu) || math.IsInf(d.Mu, 0) || math.IsNaN(logSigma) || math.IsInf(logSigma, 0) || logSigma < 0 {
			err = fmt.Errorf("lognormal mu must be finite and sigma must be finite and non-negative")
		}
	default:
		return fmt.Errorf("unsupported model %q", d.Model)
	}
	if err != nil {
		return err
	}
	minimum, err := parseDurationOrZero(d.Min)
	if err != nil {
		return fmt.Errorf("min: %w", err)
	}
	maximum, err := parseDurationOrZero(d.Max)
	if err != nil {
		return fmt.Errorf("max: %w", err)
	}
	if d.Min != "" && d.Max != "" && minimum > maximum {
		return fmt.Errorf("min cannot exceed max")
	}
	return nil
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
		value = durationFromNanoseconds(r.ExpFloat64() * float64(mean))
	case "poisson":
		mean, _ := time.ParseDuration(d.Mean)
		value = durationFromSeconds(float64(samplePoisson(r, mean.Seconds())))
	case "normal":
		mean, _ := time.ParseDuration(d.Mean)
		sigma, _ := time.ParseDuration(d.Sigma)
		value = durationFromNanoseconds(float64(mean) + r.NormFloat64()*float64(sigma))
	case "pareto":
		xm, _ := time.ParseDuration(d.XM)
		u := math.Max(r.Float64(), math.SmallestNonzeroFloat64)
		value = durationFromNanoseconds(float64(xm) / math.Pow(u, 1/d.Alpha))
	case "gamma":
		sample := sampleGamma(r, d.Alpha)
		if d.Scale != "" {
			scale, _ := time.ParseDuration(d.Scale)
			value = durationFromNanoseconds(sample * float64(scale))
		} else {
			value = durationFromSeconds(sample / d.Beta)
		}
	case "lognormal":
		logSigma, _ := d.lognormalSigma()
		value = durationFromSeconds(math.Exp(d.Mu + logSigma*r.NormFloat64()))
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
		if value > max {
			value = max
		}
	}
	return value
}

func (d Distribution) lognormalSigma() (float64, error) {
	if d.Sigma == "" {
		return d.LogSigma, nil
	}
	value, err := strconv.ParseFloat(d.Sigma, 64)
	if err != nil {
		return 0, fmt.Errorf("lognormal sigma must be a dimensionless number: %w", err)
	}
	return value, nil
}

func sampleGamma(r *rand.Rand, shape float64) float64 {
	if shape < 1 {
		u := math.Max(r.Float64(), math.SmallestNonzeroFloat64)
		return sampleGamma(r, shape+1) * math.Pow(u, 1/shape)
	}
	d := shape - 1.0/3.0
	c := 1 / math.Sqrt(9*d)
	for {
		x := r.NormFloat64()
		v := 1 + c*x
		if v <= 0 {
			continue
		}
		v = v * v * v
		u := r.Float64()
		if u < 1-0.0331*x*x*x*x || math.Log(u) < 0.5*x*x+d*(1-v+math.Log(v)) {
			return d * v
		}
	}
}

func samplePoisson(r *rand.Rand, lambda float64) int64 {
	if lambda < 30 {
		limit := math.Exp(-lambda)
		product := 1.0
		var count int64
		for product > limit {
			count++
			product *= r.Float64()
		}
		return count - 1
	}
	// PTRS (Hörmann, 1993) is an exact transformed-rejection sampler whose
	// expected cost remains constant for large means. A normal approximation
	// would subtly change v2 experiment distributions at higher rates.
	sqrtLambda := math.Sqrt(lambda)
	b := 0.931 + 2.53*sqrtLambda
	a := -0.059 + 0.02483*b
	invAlpha := 1.1239 + 1.1328/(b-3.4)
	vR := 0.9277 - 3.6224/(b-2)
	for {
		u := r.Float64() - 0.5
		v := r.Float64()
		us := 0.5 - math.Abs(u)
		k := math.Floor((2*a/us+b)*u + lambda + 0.43)
		if us >= 0.07 && v <= vR {
			return int64(k)
		}
		if k < 0 || us < 0.013 && v > us {
			continue
		}
		logFactorial, _ := math.Lgamma(k + 1)
		if math.Log(v*invAlpha/(a/(us*us)+b)) <= -lambda+k*math.Log(lambda)-logFactorial {
			return int64(k)
		}
	}
}

func durationFromSeconds(value float64) time.Duration {
	return durationFromNanoseconds(value * float64(time.Second))
}

func durationFromNanoseconds(value float64) time.Duration {
	if math.IsNaN(value) || value <= 0 {
		return 0
	}
	if math.IsInf(value, 1) || value >= float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(value)
}

func finitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
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
