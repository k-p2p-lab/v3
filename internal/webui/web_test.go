package webui

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/k-p2p-lab/v3/internal/scenario"
)

func TestDefaultScenarioMatchesSmokeExample(t *testing.T) {
	script, err := fs.ReadFile(FS(), "app.js")
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "const defaultScenario = `"
	start := strings.Index(string(script), prefix)
	if start < 0 {
		t.Fatal("dashboard default scenario is missing")
	}
	remainder := string(script)[start+len(prefix):]
	end := strings.Index(remainder, "`;")
	if end < 0 {
		t.Fatal("dashboard default scenario has no closing delimiter")
	}
	got, err := scenario.Parse([]byte(remainder[:end]))
	if err != nil {
		t.Fatalf("parse dashboard default scenario: %v", err)
	}
	example, err := os.ReadFile(filepath.Join("..", "..", "examples", "smoke.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := scenario.Parse(example)
	if err != nil {
		t.Fatalf("parse smoke example: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("dashboard default scenario differs from examples/smoke.yaml")
	}
	if len(got.Phases) == 0 || got.Phases[len(got.Phases)-1].Action != "stop-all" {
		t.Fatal("dashboard smoke scenario must clean up its peer processes")
	}
}
