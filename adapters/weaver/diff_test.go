package weaver

import (
	"os"
	"strings"
	"testing"
)

func TestParseDiffSupportsCurrentWeaverFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path     string
		format   DiffFormat
		baseline string
		head     string
	}{
		{
			path:     "testdata/diff-v1.json",
			format:   DiffFormatV1,
			baseline: "v1.39.0",
			head:     "v1.40.0",
		},
		{
			path:     "testdata/diff-v2.json",
			format:   DiffFormatV2,
			baseline: "https://opentelemetry.io/schemas/1.39.0",
			head:     "https://opentelemetry.io/schemas/1.40.0",
		},
	}

	for _, test := range tests {
		t.Run(string(test.format), func(t *testing.T) {
			t.Parallel()
			file, err := os.Open(test.path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()

			diff, err := ParseDiff(file)
			if err != nil {
				t.Fatal(err)
			}
			if diff.Format != test.format || diff.Baseline != test.baseline || diff.Head != test.head {
				t.Fatalf("diff identity = %#v", diff)
			}
			if got, want := len(diff.Changes), 3; got != want {
				t.Fatalf("changes = %d, want %d", got, want)
			}
			if diff.Changes[0].Kind != SourceKindAttribute || diff.Changes[0].Type != "renamed" {
				t.Fatalf("first change = %#v", diff.Changes[0])
			}
			if diff.Changes[1].Kind != SourceKindMetric || diff.Changes[2].Type != "obsoleted" {
				t.Fatalf("metric changes = %#v", diff.Changes[1:])
			}
		})
	}
}

func TestParseDiffRejectsUnknownSchema(t *testing.T) {
	t.Parallel()

	_, err := ParseDiff(strings.NewReader(`{
  "head_schema_url":{"url":"https://example.test/2"},
  "baseline_schema_url":{"url":"https://example.test/1"},
  "registry":{
    "attribute_changes":[],"attribute_group_changes":[],"entity_changes":[],
    "event_changes":[],"metric_changes":[],"span_changes":[]
  },
  "future_schema_field":true
}`))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v, want unknown-field rejection", err)
	}
}

func TestParseDiffRejectsMissingRequiredV2Array(t *testing.T) {
	t.Parallel()

	_, err := ParseDiff(strings.NewReader(`{
  "head_schema_url":{"url":"https://example.test/2"},
  "baseline_schema_url":{"url":"https://example.test/1"},
  "registry":{
    "attribute_changes":[],"attribute_group_changes":[],"entity_changes":[],
    "event_changes":[],"metric_changes":[]
  }
}`))
	if err == nil || !strings.Contains(err.Error(), "span_changes is required") {
		t.Fatalf("error = %v, want required-array error", err)
	}
}

func TestParseDiffRejectsUpdatedChange(t *testing.T) {
	t.Parallel()

	_, err := ParseDiff(strings.NewReader(`{
  "head":{"semconv_version":"v2"},
  "baseline":{"semconv_version":"v1"},
  "changes":{"registry_attributes":[],"metrics":[{"type":"updated"}],"events":[],"spans":[],"entities":[]}
}`))
	if err == nil || !strings.Contains(err.Error(), "field-level mapping information is unavailable") {
		t.Fatalf("error = %v, want fail-closed updated error", err)
	}
}

func TestParseDiffRejectsMalformedRename(t *testing.T) {
	t.Parallel()

	_, err := ParseDiff(strings.NewReader(`{
  "head":{"semconv_version":"v2"},
  "baseline":{"semconv_version":"v1"},
  "changes":{"registry_attributes":[{"type":"renamed","old_name":"old","note":"missing destination"}],"metrics":[],"events":[],"spans":[],"entities":[]}
}`))
	if err == nil || !strings.Contains(err.Error(), "new_name is required") {
		t.Fatalf("error = %v, want required-field error", err)
	}
}

func FuzzParseDiffDoesNotPanic(f *testing.F) {
	for _, path := range []string{"testdata/diff-v1.json", "testdata/diff-v2.json"} {
		contents, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(contents)
	}
	f.Add([]byte(`{"registry":null}`))
	f.Fuzz(func(t *testing.T, contents []byte) {
		_, _ = ParseDiff(strings.NewReader(string(contents)))
	})
}
