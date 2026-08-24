package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
)

func TestParseMigrationValidFixture(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("testdata/valid/all-change-kinds.yaml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	migration, err := ParseMigration(strings.NewReader(string(contents)))
	if err != nil {
		t.Fatalf("ParseMigration() error = %v", err)
	}

	if got, want := migration.Metadata.Name, "checkout-http-migration"; got != want {
		t.Errorf("Metadata.Name = %q, want %q", got, want)
	}
	if got, want := len(migration.Changes), 4; got != want {
		t.Fatalf("len(Changes) = %d, want %d", got, want)
	}

	labelChange := migration.Changes[2]
	if got, want := labelChange.From.Kind, domain.SymbolKindLabel; got != want {
		t.Errorf("label From.Kind = %q, want %q", got, want)
	}
	if got, want := labelChange.From.Parent, "checkout_server_request_duration_seconds"; got != want {
		t.Errorf("label From.Parent = %q, want %q", got, want)
	}
	if labelChange.To == nil {
		t.Fatal("label To is nil, want destination")
	}
	if got, want := labelChange.To.Name, "http_request_method"; got != want {
		t.Errorf("label To.Name = %q, want %q", got, want)
	}
	if migration.Changes[1].To != nil {
		t.Error("metric removal To is non-nil, want nil")
	}
}

func TestParseMigrationInvalidGoldenFixtures(t *testing.T) {
	t.Parallel()

	fixtures, err := filepath.Glob("testdata/invalid/*.yaml")
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no invalid fixtures found")
	}

	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			t.Parallel()

			contents, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			_, parseErr := ParseMigration(strings.NewReader(string(contents)))
			if parseErr == nil {
				t.Fatal("ParseMigration() error = nil, want error")
			}

			goldenPath := strings.TrimSuffix(fixture, filepath.Ext(fixture)) + ".golden"
			golden, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden file: %v", err)
			}
			if got, want := parseErr.Error()+"\n", string(golden); got != want {
				t.Errorf("error mismatch\n--- got ---\n%s--- want ---\n%s", got, want)
			}
		})
	}
}

func TestParseMigrationRejectsMultipleDocuments(t *testing.T) {
	t.Parallel()

	manifest := validManifestYAML() + "\n---\n" + validManifestYAML()
	_, err := ParseMigration(strings.NewReader(manifest))
	assertErrorContains(t, err, "must contain exactly one YAML document")
}

func TestParseMigrationSupportsTraceAttributeChanges(t *testing.T) {
	t.Parallel()

	migration, err := ParseMigration(strings.NewReader(`apiVersion: telemetry-migration/v1alpha1
kind: Migration
metadata:
  name: trace-attributes
spec:
  changes:
    - id: span-method
      kind: span_attribute_rename
      domain: opentelemetry
      from:
        attribute: http.method
      to:
        attribute: http.request.method
    - id: resource-zone
      kind: resource_attribute_remove
      domain: tempo
      from:
        attribute: cloud.availability_zone
`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(migration.Changes), 2; got != want {
		t.Fatalf("changes = %d, want %d", got, want)
	}
	if migration.Changes[0].From.Kind != domain.SymbolKindSpanAttribute || migration.Changes[0].To == nil ||
		migration.Changes[0].To.Name != "http.request.method" {
		t.Fatalf("span change = %#v", migration.Changes[0])
	}
	if migration.Changes[1].From.Domain != domain.DomainTempo || migration.Changes[1].From.Kind != domain.SymbolKindResourceAttribute ||
		migration.Changes[1].To != nil {
		t.Fatalf("resource change = %#v", migration.Changes[1])
	}
}

func TestParseMigrationRejectsEmptyInput(t *testing.T) {
	t.Parallel()

	_, err := ParseMigration(strings.NewReader(""))
	assertErrorContains(t, err, "migration manifest is empty")
}

func validManifestYAML() string {
	return `apiVersion: telemetry-migration/v1alpha1
kind: Migration
metadata:
  name: checkout
spec:
  changes:
    - id: duration
      kind: metric_rename
      domain: prometheus
      from:
        metric: old_metric
      to:
        metric: new_metric
`
}

func assertErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want error containing %q", err, want)
	}
}
