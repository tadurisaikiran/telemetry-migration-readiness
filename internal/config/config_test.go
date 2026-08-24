package config

import (
	"strings"
	"testing"
)

func TestParseConfigSupportsScalarAndMappedSources(t *testing.T) {
	t.Parallel()

	configuration, err := ParseConfig(strings.NewReader(`apiVersion: tmr/v1alpha1
sources:
  prometheusRules:
    - ./monitoring/**/*.yaml
  grafana:
    - path: ./grafana/*.json
      required: false
`))
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	if !configuration.Sources.PrometheusRules[0].Required {
		t.Error("scalar source Required = false, want default true")
	}
	if configuration.Sources.Grafana[0].Required {
		t.Error("mapped source Required = true, want false")
	}
	if !configuration.Analysis.IncludeTransitiveDependencies {
		t.Error("IncludeTransitiveDependencies = false, want default true")
	}
	if got, want := configuration.Policy.MinimumBlockingCriticality, "high"; got != want {
		t.Errorf("MinimumBlockingCriticality = %q, want %q", got, want)
	}
}

func TestParseConfigRejectsUnknownSourceField(t *testing.T) {
	t.Parallel()

	_, err := ParseConfig(strings.NewReader(`apiVersion: tmr/v1alpha1
sources:
  grafana:
    - path: ./grafana/*.json
      surprise: true
`))
	if err == nil {
		t.Fatal("ParseConfig() error = nil, want unknown-field error")
	}
}

func TestParseConfigSupportsPersesUsageSource(t *testing.T) {
	t.Parallel()

	configuration, err := ParseConfig(strings.NewReader(`apiVersion: tmr/v1alpha1
sources:
  persesUsage:
    - url: https://metrics-usage.example.test/base/
      bearerTokenEnv: TMR_PERSES_TOKEN
`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(configuration.Sources.PersesUsage), 1; got != want {
		t.Fatalf("PersesUsage sources = %d, want %d", got, want)
	}
	source := configuration.Sources.PersesUsage[0]
	if source.URL != "https://metrics-usage.example.test/base" || !source.Required || source.Timeout != "10s" {
		t.Fatalf("source = %#v", source)
	}
}

func TestParseConfigRejectsUnsafePersesUsageSource(t *testing.T) {
	t.Parallel()

	_, err := ParseConfig(strings.NewReader(`apiVersion: tmr/v1alpha1
sources:
  persesUsage:
    - url: https://user:password@metrics-usage.example.test/api?token=secret
      timeout: 3m
      bearerTokenEnv: NOT-A-NAME
`))
	if err == nil {
		t.Fatal("ParseConfig() error = nil")
	}
	for _, expected := range []string{
		"must not contain user information",
		"must not contain a query or fragment",
		"must be a positive duration no greater than 2m",
		"must be a valid environment variable name",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("error = %q, want %q", err, expected)
		}
	}
}

func TestParseConfigSupportsOwnershipDiscovery(t *testing.T) {
	t.Parallel()

	configuration, err := ParseConfig(strings.NewReader(`apiVersion: tmr/v1alpha1
sources:
  grafana: [./grafana/*.json]
ownership:
  repositoryRoot: ./repository
  metadata:
    - path: .tmr/ownership.yaml
  codeowners:
    path: .github/CODEOWNERS
`))
	if err != nil {
		t.Fatal(err)
	}
	ownership := configuration.Ownership
	if !ownership.Enabled || ownership.RepositoryRoot != "./repository" || !ownership.DashboardTags {
		t.Fatalf("ownership = %#v", ownership)
	}
	if !ownership.Codeowners.Enabled || ownership.Codeowners.Path != ".github/CODEOWNERS" {
		t.Fatalf("codeowners = %#v", ownership.Codeowners)
	}
	if len(ownership.Metadata) != 1 || ownership.Metadata[0].Pattern != ".tmr/ownership.yaml" {
		t.Fatalf("metadata = %#v", ownership.Metadata)
	}
}

func TestParseConfigOwnershipIsOptIn(t *testing.T) {
	t.Parallel()

	configuration, err := ParseConfig(strings.NewReader(`apiVersion: tmr/v1alpha1
sources:
  grafana: [./grafana/*.json]
`))
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Ownership.Enabled {
		t.Fatalf("ownership = %#v, want disabled", configuration.Ownership)
	}
}

func TestParseConfigRejectsOwnershipPathsOutsideRepository(t *testing.T) {
	t.Parallel()

	_, err := ParseConfig(strings.NewReader(`apiVersion: tmr/v1alpha1
sources:
  grafana: [./grafana/*.json]
ownership:
  metadata:
    - ../outside.yaml
  codeowners:
    path: /tmp/CODEOWNERS
`))
	if err == nil {
		t.Fatal("ParseConfig() error = nil")
	}
	for _, expected := range []string{"ownership.metadata[0].path", "ownership.codeowners.path"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("error = %q, want %q", err, expected)
		}
	}
}

func TestParseConfigRejectsUnknownOwnershipField(t *testing.T) {
	t.Parallel()

	_, err := ParseConfig(strings.NewReader(`apiVersion: tmr/v1alpha1
sources:
  grafana: [./grafana/*.json]
ownership:
  repositoryRoot: .
  inferAnything: true
`))
	if err == nil || !strings.Contains(err.Error(), "inferAnything") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseConfigSupportsRuntimeQuerySources(t *testing.T) {
	t.Parallel()

	configuration, err := ParseConfig(strings.NewReader(`apiVersion: tmr/v1alpha1
sources:
  runtimeQueries:
    - path: ./runtime/prometheus-query.log
      format: prometheus_query_log
      window: 720h
    - path: ./runtime/history.jsonl
      format: tmr_query_history
      required: false
      criticality: critical
`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(configuration.Sources.RuntimeQueries), 2; got != want {
		t.Fatalf("runtime sources = %d, want %d", got, want)
	}
	first := configuration.Sources.RuntimeQueries[0]
	if !first.Required || first.Window != "720h" || first.Criticality != "high" || first.Format != RuntimeQueryFormatPrometheusLog {
		t.Fatalf("first runtime source = %#v", first)
	}
	second := configuration.Sources.RuntimeQueries[1]
	if second.Required || second.Window != "0s" || second.Criticality != "critical" || second.Format != RuntimeQueryFormatTMRHistory {
		t.Fatalf("second runtime source = %#v", second)
	}
}

func TestParseConfigRejectsUnsafeRuntimeQuerySources(t *testing.T) {
	t.Parallel()

	_, err := ParseConfig(strings.NewReader(`apiVersion: tmr/v1alpha1
sources:
  runtimeQueries:
    - path: ""
      format: invented
      window: -1s
      criticality: urgent
    - path: huge.log
      format: prometheus_query_log
      window: 8761h
`))
	if err == nil {
		t.Fatal("ParseConfig() error = nil")
	}
	for _, expected := range []string{
		"sources.runtimeQueries[0].path",
		"sources.runtimeQueries[0].format",
		"sources.runtimeQueries[0].window",
		"sources.runtimeQueries[0].criticality",
		"sources.runtimeQueries[1].window",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("error = %q, want %q", err, expected)
		}
	}
}

func TestParseConfigSupportsTempoQueriesAndExplicitTraceMappings(t *testing.T) {
	t.Parallel()

	configuration, err := ParseConfig(strings.NewReader(`apiVersion: tmr/v1alpha1
sources:
  tempoQueries:
    - url: https://tempo.example.com/base/
      path: ./trace-queries/*.yaml
      bearerTokenEnv: TEMPO_TOKEN
mappings:
  traceAttributes:
    - scope: span
      opentelemetry: http.request.method
      tempo: http.method
    - scope: resource
      opentelemetry: service.name
      tempo: service.name
`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(configuration.Sources.TempoQueries), 1; got != want {
		t.Fatalf("Tempo sources = %d, want %d", got, want)
	}
	source := configuration.Sources.TempoQueries[0]
	if source.URL != "https://tempo.example.com/base" || source.Pattern != "./trace-queries/*.yaml" ||
		!source.Required || source.Timeout != "60s" || source.Criticality != "high" || source.BearerTokenEnv != "TEMPO_TOKEN" {
		t.Fatalf("Tempo source = %#v", source)
	}
	if got, want := len(configuration.Mappings.TraceAttributes), 2; got != want {
		t.Fatalf("trace mappings = %d, want %d", got, want)
	}
}

func TestParseConfigRejectsUnsafeTempoAndTraceMappings(t *testing.T) {
	t.Parallel()

	_, err := ParseConfig(strings.NewReader(`apiVersion: tmr/v1alpha1
sources:
  tempoQueries:
    - url: ftp://tempo.example.com?token=secret
      path: ""
      timeout: 3m
      bearerTokenEnv: bad-name
      criticality: urgent
mappings:
  traceAttributes:
    - scope: trace
      opentelemetry: ""
      tempo: old
    - scope: trace
      opentelemetry: ""
      tempo: old
`))
	if err == nil {
		t.Fatal("ParseConfig() error = nil")
	}
	for _, expected := range []string{
		"sources.tempoQueries[0].url",
		"sources.tempoQueries[0].path",
		"sources.tempoQueries[0].timeout",
		"sources.tempoQueries[0].bearerTokenEnv",
		"sources.tempoQueries[0].criticality",
		"mappings.traceAttributes[0].scope",
		"mappings.traceAttributes[0].opentelemetry",
		"mappings.traceAttributes[1].opentelemetry: duplicates",
		"mappings.traceAttributes[1].tempo: duplicates",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("error = %q, want %q", err, expected)
		}
	}
}
