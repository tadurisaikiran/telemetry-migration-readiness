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
