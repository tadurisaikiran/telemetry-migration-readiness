package persesusage

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
)

func TestBuilderProducesDeterministicConsumersAndProvenance(t *testing.T) {
	t.Parallel()

	metrics := mustDecodeMetricsFixture(t, "testdata/metrics.json")
	partial := mustDecodePartialFixture(t, "testdata/partial_metrics.json")
	pending := mustDecodePendingFixture(t, "testdata/pending_usages.json")

	build := func() domain.Discovery {
		builder := newDiscoveryBuilder("https://usage.example.test/base", true)
		builder.addPendingUsage(pending)
		builder.addPartialMetrics(partial)
		builder.addMetrics(metrics)
		return builder.build()
	}
	first := build()
	second := build()
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("discovery is nondeterministic:\n%s\n%s", firstJSON, secondJSON)
	}
	if got, want := len(first.Consumers), 6; got != want {
		t.Fatalf("consumers = %d, want %d", got, want)
	}
	if got, want := len(first.Productions), 1; got != want {
		t.Fatalf("productions = %d, want %d", got, want)
	}
	if got, want := first.Productions[0].Symbol.Name, "checkout:requests:rate1m"; got != want {
		t.Fatalf("production = %q, want %q", got, want)
	}

	recording := findConsumer(t, first, domain.ConsumerKindRecordingRule, "checkout:requests:rate1m")
	if recording.Metadata["usage_origins"] != "metrics" {
		t.Fatalf("recording origins = %q", recording.Metadata["usage_origins"])
	}
	methods := map[domain.EvidenceMethod]bool{}
	for _, reference := range first.References {
		if reference.ConsumerID == recording.ID && reference.Symbol.Name == "checkout_request_duration_seconds_count" {
			methods[reference.Evidence.Method] = true
		}
	}
	if !methods[domain.EvidenceMethodUsageAPI] || !methods[domain.EvidenceMethodPromQLAST] {
		t.Fatalf("recording evidence methods = %#v", methods)
	}

	dashboard := findConsumer(t, first, domain.ConsumerKindDashboard, "Checkout Latency")
	var scopedLabelUncertainty bool
	for _, reference := range first.References {
		if reference.ConsumerID == dashboard.ID && reference.ResolutionScope == domain.ResolutionScopeLabels {
			scopedLabelUncertainty = reference.RequiresResolution &&
				reference.Evidence.Confidence == domain.ConfidenceUnknown
		}
	}
	if !scopedLabelUncertainty {
		t.Fatal("dashboard is missing label-scoped uncertainty evidence")
	}

	templated := findConsumer(t, first, domain.ConsumerKindDashboard, "Templated Latency")
	var unresolvedPattern bool
	var matchedExactMetric bool
	for _, reference := range first.References {
		if reference.ConsumerID == templated.ID && reference.Usage == domain.UsagePattern {
			unresolvedPattern = reference.RequiresResolution && reference.Pattern != ""
		}
		if reference.ConsumerID == templated.ID &&
			reference.Symbol.Name == "checkout_request_duration_seconds_count" &&
			reference.Usage == domain.UsageSelector {
			matchedExactMetric = !reference.RequiresResolution
		}
	}
	if !unresolvedPattern {
		t.Fatal("partial metric usage is not unresolved")
	}
	if !matchedExactMetric {
		t.Fatal("partial metric's current exact match was not imported")
	}
}

func findConsumer(t *testing.T, discovery domain.Discovery, kind domain.ConsumerKind, name string) domain.Consumer {
	t.Helper()
	for _, consumer := range discovery.Consumers {
		if consumer.Kind == kind && consumer.Name == name {
			return consumer
		}
	}
	t.Fatalf("consumer %s %q not found", kind, name)
	return domain.Consumer{}
}

func mustDecodeMetricsFixture(t *testing.T, path string) map[string]*metricDocument {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	result, err := decodeMetrics(file)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustDecodePartialFixture(t *testing.T, path string) map[string]*partialMetricDocument {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	result, err := decodePartialMetrics(file)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustDecodePendingFixture(t *testing.T, path string) map[string]*usageDocument {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	result, err := decodePendingUsage(file)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
