package scenario

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV2ChurnExampleValidates(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", "v2-churn.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(data); err != nil {
		t.Fatalf("v2 reproduction example: %v", err)
	}
}

func TestPhasePolicyDefaultsAndExplicitOptions(t *testing.T) {
	s, err := Parse([]byte(`
name: policy-options
phases:
  - action: join
    group: workers
    count: 1
  - action: join
    group: workers
    count: 2
    agentId: agent-b
    placement: single-agent
    parallel: true
    parallelism: 1
  - action: join
    group: workers
    count: 3
    placement: random
  - action: publish
    group: workers
    count: 1
  - action: publish
    group: workers
    count: 2
    onError: continue
    topic: '*'
    payloadEncoding: raw
  - action: leave
    group: workers
    count: 2
    onError: continue
`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Phases[0].Placement != "balanced" || !s.Phases[0].ShouldAwait() || s.Phases[0].Repeat != 1 {
		t.Fatalf("unexpected join defaults: %+v", s.Phases[0])
	}
	if s.Phases[1].AgentID != "agent-b" || s.Phases[1].Placement != "single-agent" || s.Phases[2].Placement != "random" {
		t.Fatalf("placement options were lost: %+v", s.Phases[:3])
	}
	if s.Phases[3].OnError != "fail" || s.Phases[3].PayloadEncoding != "envelope" || s.Phases[4].PayloadEncoding != "raw" || s.Phases[5].OnError != "continue" {
		t.Fatalf("unexpected operation policy: %+v", s.Phases[3:])
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("revalidating defaulted phases: %v", err)
	}
}

func TestPhaseRejectsUnsupportedPolicies(t *testing.T) {
	for _, tc := range []struct{ action, option, want string }{
		{"publish", "onError: retry", "onError must"},
		{"leave", "onError: retry", "onError must"},
		{"join", "onError: continue", "onError is only"},
		{"join", "placement: any", "placement must"},
		{"join", "agentId: ' '", "agentId must"},
		{"publish", "agentId: agent-a", "only supported for join"},
		{"leave", "placement: balanced", "only supported for join"},
		{"publish", "payloadEncoding: json", "payloadEncoding must"},
		{"join", "payloadEncoding: raw", "payloadEncoding is only"},
	} {
		t.Run(tc.action+"/"+tc.option, func(t *testing.T) {
			_, err := Parse([]byte("name: invalid-policy\nphases:\n  - action: " + tc.action + "\n    group: workers\n    count: 1\n    " + tc.option + "\n"))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}
