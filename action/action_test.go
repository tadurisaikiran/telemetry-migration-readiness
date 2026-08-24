package action

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestActionMetadataAndScript(t *testing.T) {
	t.Parallel()

	root := filepath.Clean("..")
	contents, err := os.ReadFile(filepath.Join(root, "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Name    string                 `yaml:"name"`
		Inputs  map[string]any         `yaml:"inputs"`
		Outputs map[string]any         `yaml:"outputs"`
		Runs    map[string]interface{} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(contents, &document); err != nil {
		t.Fatalf("decode action.yml: %v", err)
	}
	if document.Name == "" || document.Runs["using"] != "composite" {
		t.Fatalf("invalid action metadata: %+v", document)
	}
	for _, input := range []string{"config", "migration"} {
		if _, exists := document.Inputs[input]; !exists {
			t.Errorf("missing %q input", input)
		}
	}
	for _, output := range []string{"status", "exit-code", "report"} {
		if _, exists := document.Outputs[output]; !exists {
			t.Errorf("missing %q output", output)
		}
	}

	script, err := os.ReadFile(filepath.Join("run-action.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"set -euo pipefail", "--format markdown", "GITHUB_STEP_SUMMARY", "GITHUB_OUTPUT"} {
		if !strings.Contains(string(script), expected) {
			t.Errorf("run-action.sh does not contain %q", expected)
		}
	}
}
