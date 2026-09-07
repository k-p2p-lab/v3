package scenario

import (
	"strings"
	"testing"
)

func TestParseRequiresOneCompleteYAMLDocument(t *testing.T) {
	valid := "name: document\nphases: [{action: stop-all}]\n"
	for _, suffix := range []string{"", "# final comment\n", "...\n"} {
		if _, err := Parse([]byte(valid + suffix)); err != nil {
			t.Fatalf("valid suffix %q rejected: %v", suffix, err)
		}
	}
	for _, suffix := range []string{"---\n", "---\n" + valid, "---\n[broken\n", "...\ninvalid trailing text\n"} {
		if _, err := Parse([]byte(valid + suffix)); err == nil {
			t.Fatalf("trailing document/data %q accepted", suffix)
		}
	}
	if _, err := Parse([]byte(valid + "---\n" + valid)); err == nil || !strings.Contains(err.Error(), "exactly one YAML document") {
		t.Fatalf("missing multiple-document diagnostic: %v", err)
	}
}
