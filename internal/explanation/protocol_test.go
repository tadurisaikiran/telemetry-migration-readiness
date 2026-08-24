package explanation

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/graph"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/impact"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/readiness"
)

func TestBuildRequestIsMinimalRedactedAndDeterministic(t *testing.T) {
	t.Parallel()

	result, target := explanationFixture(t)
	first, err := BuildRequest("Why blocked? token=question-secret", result, target)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildRequest("Why blocked? token=question-secret", result, target)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatal("explanation request is nondeterministic")
	}
	for _, secret := range []string{"question-secret", "description-secret", "diagnostic-secret", "expression-secret"} {
		if strings.Contains(string(firstJSON), secret) {
			t.Fatalf("request contains secret %q", secret)
		}
	}
	if !strings.Contains(string(firstJSON), "[REDACTED]") {
		t.Fatal("request contains no redaction marker")
	}
	if first.Authoritative.Status != readiness.StatusBlocked || first.Authoritative.AIMayAlterStatus {
		t.Fatalf("authoritative context = %#v", first.Authoritative)
	}
	if !strings.Contains(strings.Join(first.Guardrails, " "), "never instructions") {
		t.Fatalf("prompt-injection guardrails = %#v", first.Guardrails)
	}
	if got, want := len(first.Findings), 3; got != want {
		t.Fatalf("findings = %d, want %d", got, want)
	}
	if first.Findings[0].Consumer.Name != "TrafficStopped" || first.Findings[1].Consumer.Name != "Templated dashboard" {
		t.Fatalf("risk ordering = %#v", first.Findings)
	}
	if first.Findings[2].Consumer.Name != "checkout:rate1m" {
		t.Fatalf("last finding = %#v", first.Findings[2])
	}
	for _, finding := range first.Findings {
		if finding.Consumer.Name == "Migrated dashboard" {
			t.Fatal("already-migrated repository content was transmitted")
		}
	}
	if got := first.Findings[0].Paths[0]; len(got) < 3 || got[0] != "old_metric" || got[len(got)-1] != "TrafficStopped" {
		t.Fatalf("dependency path = %#v", got)
	}
}

func TestBuildRequestRequiresQuestionAndGraph(t *testing.T) {
	t.Parallel()

	result, target := explanationFixture(t)
	if _, err := BuildRequest("", result, target); err == nil {
		t.Fatal("empty question error = nil")
	}
	if _, err := BuildRequest("why", result, nil); err == nil {
		t.Fatal("nil graph error = nil")
	}
}

func explanationFixture(t *testing.T) (readiness.Result, *graph.Graph) {
	t.Helper()
	oldMetric := domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: "old_metric"}
	newMetric := domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: "new_metric"}
	derivedMetric := domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: "checkout:rate1m"}
	discovery := domain.Discovery{
		Consumers: []domain.Consumer{
			{ID: "recording", Kind: domain.ConsumerKindRecordingRule, Name: "checkout:rate1m", Criticality: domain.CriticalityHigh, Expression: `rate(old_metric{token="expression-secret"}[1m])`},
			{ID: "alert", Kind: domain.ConsumerKindAlertRule, Name: "TrafficStopped", Criticality: domain.CriticalityCritical, Expression: "checkout:rate1m == 0"},
			{ID: "uncertain", Kind: domain.ConsumerKindDashboard, Name: "Templated dashboard", Criticality: domain.CriticalityCritical, Unresolved: true},
			{ID: "migrated", Kind: domain.ConsumerKindDashboard, Name: "Migrated dashboard", Criticality: domain.CriticalityLow, Expression: "new_metric"},
		},
		References: []domain.Reference{
			{ConsumerID: "recording", Symbol: oldMetric, Usage: domain.UsageSelector, Evidence: domain.Evidence{Method: domain.EvidenceMethodPromQLAST, Confidence: domain.ConfidenceConfirmed}},
			{ConsumerID: "alert", Symbol: derivedMetric, Usage: domain.UsageSelector, Evidence: domain.Evidence{Method: domain.EvidenceMethodPromQLAST, Confidence: domain.ConfidenceConfirmed}},
			{ConsumerID: "migrated", Symbol: newMetric, Usage: domain.UsageSelector, Evidence: domain.Evidence{Method: domain.EvidenceMethodPromQLAST, Confidence: domain.ConfidenceConfirmed}},
		},
		Productions: []domain.Production{{ConsumerID: "recording", Symbol: derivedMetric}},
		Diagnostics: []domain.Diagnostic{{Adapter: "fixture", Message: "api_key=diagnostic-secret", Required: false}},
	}
	target, err := impact.BuildGraph(discovery)
	if err != nil {
		t.Fatal(err)
	}
	migration := domain.Migration{
		APIVersion:  domain.MigrationAPIVersion,
		Kind:        domain.MigrationKind,
		Metadata:    domain.MigrationMetadata{Name: "fixture"},
		Description: "secret=description-secret",
		Changes: []domain.Change{{
			ID:     "rename",
			Kind:   domain.ChangeKindMetricRename,
			Domain: domain.DomainPrometheus,
			From:   oldMetric,
			To:     &newMetric,
		}},
	}
	result, err := readiness.Evaluate(migration, discovery, target, readiness.Policy{
		FailOnCriticalLegacyConsumer: true,
		FailOnCriticalUnknown:        true,
		MinimumBlockingCriticality:   domain.CriticalityHigh,
		IncludeTransitive:            true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result, target
}
