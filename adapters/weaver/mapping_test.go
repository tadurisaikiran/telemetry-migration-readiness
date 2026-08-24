package weaver

import (
	"os"
	"strings"
	"testing"
)

func TestParseMapping(t *testing.T) {
	t.Parallel()

	file, err := os.Open("testdata/mapping.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	mapping, err := ParseMapping(file)
	if err != nil {
		t.Fatal(err)
	}
	if mapping.Name != "http-semconv-migration" || len(mapping.Entries) != 3 {
		t.Fatalf("mapping = %#v", mapping)
	}
	if mapping.Entries[0].Prometheus == nil || mapping.Entries[2].Ignore == "" {
		t.Fatalf("mapping entries = %#v", mapping.Entries)
	}
}

func TestParseMappingRequiresExplicitResolution(t *testing.T) {
	t.Parallel()

	_, err := ParseMapping(strings.NewReader(`
apiVersion: tmr.weaver/v1alpha1
kind: WeaverMapping
metadata:
  name: invalid
spec:
  mappings:
    - id: unresolved
      weaver:
        kind: metric
        type: renamed
        from: old.metric
        to: new.metric
`))
	if err == nil || !strings.Contains(err.Error(), "exactly one of prometheus or ignore") {
		t.Fatalf("error = %v, want explicit-resolution error", err)
	}
}

func TestParseMappingRejectsAttributeToMetricAssumption(t *testing.T) {
	t.Parallel()

	_, err := ParseMapping(strings.NewReader(`
apiVersion: tmr.weaver/v1alpha1
kind: WeaverMapping
metadata:
  name: invalid
spec:
  mappings:
    - id: wrong-domain-assumption
      weaver:
        kind: attribute
        type: renamed
        from: http.method
        to: http.request.method
      prometheus:
        kind: metric_rename
        from:
          metric: http_method
        to:
          metric: http_request_method
`))
	if err == nil || !strings.Contains(err.Error(), "attribute source must map to a label change") {
		t.Fatalf("error = %v, want domain-boundary error", err)
	}
}

func TestParseMappingRejectsUnknownField(t *testing.T) {
	t.Parallel()

	_, err := ParseMapping(strings.NewReader(`
apiVersion: tmr.weaver/v1alpha1
kind: WeaverMapping
metadata:
  name: invalid
future: true
spec:
  mappings: []
`))
	if err == nil || !strings.Contains(err.Error(), "field future not found") {
		t.Fatalf("error = %v, want unknown-field error", err)
	}
}

func FuzzParseMappingDoesNotPanic(f *testing.F) {
	contents, err := os.ReadFile("testdata/mapping.yaml")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(contents)
	f.Add([]byte("apiVersion: ["))
	f.Fuzz(func(t *testing.T, contents []byte) {
		_, _ = ParseMapping(strings.NewReader(string(contents)))
	})
}
