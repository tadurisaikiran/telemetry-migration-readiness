package analysis

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/config"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/readiness"
)

func TestCheckoutFixtureIsBlockedAndTransitive(t *testing.T) {
	t.Parallel()

	repositoryRoot := filepath.Clean(filepath.Join("..", ".."))
	configuration, err := config.LoadConfig(context.Background(), filepath.Join(repositoryRoot, "examples", "checkout-migration", "tmr.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	absolutizePatterns(&configuration, repositoryRoot)
	migration, err := config.LoadMigration(context.Background(), filepath.Join(repositoryRoot, "examples", "checkout-migration", "migration.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	result, _, _, err := Run(context.Background(), configuration, migration)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Status != readiness.StatusBlocked {
		t.Fatalf("status = %s, want BLOCKED", result.Summary.Status)
	}
	if result.Summary.LegacyOnly == 0 || result.Summary.Uncertain == 0 {
		t.Fatalf("summary = %+v, want legacy and uncertain consumers", result.Summary)
	}

	var transitivePath bool
	for _, change := range result.Changes {
		for _, consumer := range change.Consumers {
			if consumer.Consumer.Name != "CheckoutLatencyHigh" {
				continue
			}
			for _, path := range consumer.Paths {
				joined := strings.Join(path.Nodes, " ")
				if strings.Contains(joined, "checkout:p95_latency") && len(path.Edges) > 1 {
					transitivePath = true
				}
			}
		}
	}
	if !transitivePath {
		t.Fatal("missing raw metric -> recording rule -> alert transitive path")
	}
}

func TestRequiredMissingSourceIsIncomplete(t *testing.T) {
	t.Parallel()

	configuration := config.Config{
		APIVersion: config.ConfigAPIVersion,
		Sources: config.Sources{PrometheusRules: []config.SourcePattern{{
			Pattern:  filepath.Join(t.TempDir(), "missing", "*.yaml"),
			Required: true,
		}}},
		Analysis: config.AnalysisConfig{IncludeTransitiveDependencies: true, UnresolvedReferencePolicy: "error"},
		Policy:   config.PolicyConfig{FailOnCriticalLegacyConsumer: true, FailOnCriticalUnknown: true, MinimumBlockingCriticality: "high"},
		Output:   config.OutputConfig{Formats: []string{"json"}},
	}
	migration, err := config.ParseMigration(strings.NewReader(`
apiVersion: telemetry-migration/v1alpha1
kind: Migration
metadata: {name: missing-source}
spec:
  changes:
    - id: remove
      kind: metric_remove
      domain: prometheus
      from: {metric: old_metric}
`))
	if err != nil {
		t.Fatal(err)
	}
	result, _, _, err := Run(context.Background(), configuration, migration)
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Status != readiness.StatusIncomplete {
		t.Fatalf("status = %s, want INCOMPLETE", result.Summary.Status)
	}
}

func TestPersesUsageFailureHonorsRequiredPolicy(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	migration := mustParseRemovalMigration(t)

	for _, test := range []struct {
		name     string
		required bool
		status   readiness.Status
	}{
		{name: "required", required: true, status: readiness.StatusIncomplete},
		{name: "optional", required: false, status: readiness.StatusReady},
	} {
		t.Run(test.name, func(t *testing.T) {
			configuration := testConfiguration(config.Sources{PersesUsage: []config.PersesUsageSource{{
				URL:      server.URL,
				Required: test.required,
				Timeout:  "1s",
			}}})
			result, _, _, err := Run(context.Background(), configuration, migration)
			if err != nil {
				t.Fatal(err)
			}
			if result.Summary.Status != test.status {
				t.Fatalf("status = %s, want %s", result.Summary.Status, test.status)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Required != test.required {
				t.Fatalf("diagnostics = %#v", result.Diagnostics)
			}
		})
	}
}

func TestPersesUsageMissingBearerTokenIsIncomplete(t *testing.T) {
	t.Setenv("TMR_TEST_DEFINITELY_UNSET_TOKEN", "")
	configuration := testConfiguration(config.Sources{PersesUsage: []config.PersesUsageSource{{
		URL:            "https://usage.example.test",
		Required:       true,
		Timeout:        "1s",
		BearerTokenEnv: "TMR_TEST_DEFINITELY_UNSET_TOKEN",
	}}})
	result, _, _, err := Run(context.Background(), configuration, mustParseRemovalMigration(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Status != readiness.StatusIncomplete {
		t.Fatalf("status = %s, want INCOMPLETE", result.Summary.Status)
	}
	if len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, "unset or empty") {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
}

func testConfiguration(sources config.Sources) config.Config {
	return config.Config{
		APIVersion: config.ConfigAPIVersion,
		Sources:    sources,
		Analysis: config.AnalysisConfig{
			IncludeTransitiveDependencies: true,
			UnresolvedReferencePolicy:     "error",
		},
		Policy: config.PolicyConfig{
			FailOnCriticalLegacyConsumer: true,
			FailOnCriticalUnknown:        true,
			MinimumBlockingCriticality:   "high",
		},
		Output: config.OutputConfig{Formats: []string{"json"}},
	}
}

func mustParseRemovalMigration(t *testing.T) domain.Migration {
	t.Helper()
	migration, err := config.ParseMigration(strings.NewReader(`
apiVersion: telemetry-migration/v1alpha1
kind: Migration
metadata: {name: remote-source}
spec:
  changes:
    - id: remove
      kind: metric_remove
      domain: prometheus
      from: {metric: old_metric}
`))
	if err != nil {
		t.Fatal(err)
	}
	return migration
}

func absolutizePatterns(configuration *config.Config, root string) {
	groups := [][]config.SourcePattern{
		configuration.Sources.PrometheusRules,
		configuration.Sources.Grafana,
		configuration.Sources.Sloth,
		configuration.Sources.Pyrra,
	}
	for _, group := range groups {
		for index := range group {
			group[index].Pattern = filepath.Join(root, strings.TrimPrefix(group[index].Pattern, "./"))
		}
	}
}
