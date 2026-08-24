package readiness

import (
	"testing"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/impact"
)

func TestEvaluateBlocksTransitiveLegacyConsumer(t *testing.T) {
	t.Parallel()

	discovery := transitiveDiscovery("checkout_request_duration_seconds_bucket")
	result := evaluateForTest(t, metricRenameMigration(), discovery)
	if got, want := result.Summary.Status, StatusBlocked; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
	if got, want := result.Summary.LegacyOnly, 2; got != want {
		t.Errorf("LegacyOnly = %d, want %d", got, want)
	}
	alert := findConsumerResult(t, result, "alert")
	if alert.Classification != ClassificationLegacyOnly {
		t.Errorf("alert classification = %q, want LEGACY_ONLY", alert.Classification)
	}
	if len(alert.Paths) == 0 || len(alert.Paths[0].Nodes) != 4 {
		t.Errorf("alert path = %+v, want full transitive path", alert.Paths)
	}
}

func TestEvaluateReadyAfterConsumerMigration(t *testing.T) {
	t.Parallel()

	discovery := transitiveDiscovery("checkout_server_request_duration_seconds_bucket")
	result := evaluateForTest(t, metricRenameMigration(), discovery)
	if got, want := result.Summary.Status, StatusReady; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
	if got, want := result.Summary.Migrated, 2; got != want {
		t.Errorf("Migrated = %d, want %d", got, want)
	}
}

func TestEvaluateCriticalUncertaintyIsIncomplete(t *testing.T) {
	t.Parallel()

	discovery := domain.Discovery{
		Consumers: []domain.Consumer{{
			ID:          "dashboard",
			Kind:        domain.ConsumerKindDashboardPanel,
			Name:        "Critical dashboard",
			Criticality: domain.CriticalityCritical,
			Unresolved:  true,
		}},
	}
	result := evaluateForTest(t, metricRenameMigration(), discovery)
	if got, want := result.Summary.Status, StatusIncomplete; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
	if got, want := result.Summary.Uncertain, 1; got != want {
		t.Errorf("Uncertain = %d, want %d", got, want)
	}
}

func TestEvaluateRequiredDiagnosticIsIncomplete(t *testing.T) {
	t.Parallel()

	discovery := domain.Discovery{
		Diagnostics: []domain.Diagnostic{{Adapter: "grafana", Message: "corrupt JSON", Required: true}},
	}
	result := evaluateForTest(t, metricRenameMigration(), discovery)
	if got, want := result.Summary.Status, StatusIncomplete; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
}

func TestEvaluateScopesMissingLabelEvidenceToLabelChanges(t *testing.T) {
	t.Parallel()

	metric := domain.Symbol{
		Domain: domain.DomainPrometheus,
		Kind:   domain.SymbolKindMetric,
		Name:   "checkout_server_request_duration_seconds_count",
	}
	discovery := domain.Discovery{
		Consumers: []domain.Consumer{{
			ID:          "remote-dashboard",
			Kind:        domain.ConsumerKindDashboard,
			Name:        "Remote dashboard",
			Criticality: domain.CriticalityCritical,
		}},
		References: []domain.Reference{
			{ConsumerID: "remote-dashboard", Symbol: metric},
			{
				ConsumerID:         "remote-dashboard",
				Symbol:             metric,
				RequiresResolution: true,
				ResolutionScope:    domain.ResolutionScopeLabels,
			},
		},
	}

	metricResult := evaluateForTest(t, metricRenameMigration(), discovery)
	if got, want := metricResult.Summary.Status, StatusReady; got != want {
		t.Fatalf("metric status = %q, want %q", got, want)
	}
	if got := findConsumerResult(t, metricResult, "remote-dashboard").Classification; got != ClassificationMigrated {
		t.Fatalf("metric classification = %q, want MIGRATED", got)
	}

	labelDestination := domain.Symbol{
		Domain: domain.DomainPrometheus,
		Kind:   domain.SymbolKindLabel,
		Name:   "http_request_method",
		Parent: "checkout_server_request_duration_seconds",
	}
	labelMigration := domain.Migration{
		APIVersion: domain.MigrationAPIVersion,
		Kind:       domain.MigrationKind,
		Metadata:   domain.MigrationMetadata{Name: "labels"},
		Changes: []domain.Change{{
			ID:     "method",
			Kind:   domain.ChangeKindLabelRename,
			Domain: domain.DomainPrometheus,
			From: domain.Symbol{
				Domain: domain.DomainPrometheus,
				Kind:   domain.SymbolKindLabel,
				Name:   "http_method",
				Parent: "checkout_server_request_duration_seconds",
			},
			To: &labelDestination,
		}},
	}
	labelResult := evaluateForTest(t, labelMigration, discovery)
	if got, want := labelResult.Summary.Status, StatusIncomplete; got != want {
		t.Fatalf("label status = %q, want %q", got, want)
	}
	if got := findConsumerResult(t, labelResult, "remote-dashboard").Classification; got != ClassificationUncertain {
		t.Fatalf("label classification = %q, want UNCERTAIN", got)
	}
}

func TestEvaluateMetricRemovalBlocksReference(t *testing.T) {
	t.Parallel()

	migration := metricRenameMigration()
	migration.Changes[0].Kind = domain.ChangeKindMetricRemove
	migration.Changes[0].To = nil
	result := evaluateForTest(t, migration, transitiveDiscovery("checkout_request_duration_seconds_count"))
	if got, want := result.Summary.Status, StatusBlocked; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
}

func TestTraceAttributeMatchingIsExactAndDomainScoped(t *testing.T) {
	t.Parallel()

	changed := domain.Symbol{Domain: domain.DomainOpenTelemetry, Kind: domain.SymbolKindSpanAttribute, Name: "http.method"}
	for _, test := range []struct {
		name      string
		reference domain.Symbol
		want      bool
	}{
		{name: "exact", reference: changed, want: true},
		{name: "different scope", reference: domain.Symbol{Domain: domain.DomainOpenTelemetry, Kind: domain.SymbolKindResourceAttribute, Name: "http.method"}},
		{name: "different domain", reference: domain.Symbol{Domain: domain.DomainTempo, Kind: domain.SymbolKindSpanAttribute, Name: "http.method"}},
		{name: "metric suffix is not applied", reference: domain.Symbol{Domain: domain.DomainOpenTelemetry, Kind: domain.SymbolKindSpanAttribute, Name: "http.method_count"}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := symbolsMatch(test.reference, changed); got != test.want {
				t.Fatalf("symbolsMatch() = %t, want %t", got, test.want)
			}
		})
	}
}

func evaluateForTest(t *testing.T, migration domain.Migration, discovery domain.Discovery) Result {
	t.Helper()
	target, err := impact.BuildGraph(discovery)
	if err != nil {
		t.Fatalf("BuildGraph() error = %v", err)
	}
	result, err := Evaluate(migration, discovery, target, Policy{
		FailOnCriticalLegacyConsumer: true,
		FailOnCriticalUnknown:        true,
		MinimumBlockingCriticality:   domain.CriticalityHigh,
		IncludeTransitive:            true,
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	return result
}

func transitiveDiscovery(rawName string) domain.Discovery {
	raw := domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: rawName}
	derived := domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: "checkout:p95_latency"}
	return domain.Discovery{
		Consumers: []domain.Consumer{
			{ID: "recording", Kind: domain.ConsumerKindRecordingRule, Name: "checkout:p95_latency", Criticality: domain.CriticalityHigh},
			{ID: "alert", Kind: domain.ConsumerKindAlertRule, Name: "CheckoutLatencyHigh", Criticality: domain.CriticalityCritical},
		},
		References: []domain.Reference{
			{ConsumerID: "recording", Symbol: raw},
			{ConsumerID: "alert", Symbol: derived},
		},
		Productions: []domain.Production{{ConsumerID: "recording", Symbol: derived}},
	}
}

func metricRenameMigration() domain.Migration {
	destination := domain.Symbol{
		Domain: domain.DomainPrometheus,
		Kind:   domain.SymbolKindMetric,
		Name:   "checkout_server_request_duration_seconds",
	}
	return domain.Migration{
		APIVersion: domain.MigrationAPIVersion,
		Kind:       domain.MigrationKind,
		Metadata:   domain.MigrationMetadata{Name: "checkout"},
		Changes: []domain.Change{{
			ID:     "duration",
			Kind:   domain.ChangeKindMetricRename,
			Domain: domain.DomainPrometheus,
			From: domain.Symbol{
				Domain: domain.DomainPrometheus,
				Kind:   domain.SymbolKindMetric,
				Name:   "checkout_request_duration_seconds",
			},
			To: &destination,
		}},
	}
}

func findConsumerResult(t *testing.T, result Result, id string) ConsumerResult {
	t.Helper()
	for _, consumer := range result.Changes[0].Consumers {
		if consumer.Consumer.ID == id {
			return consumer
		}
	}
	t.Fatalf("consumer %q not found", id)
	return ConsumerResult{}
}
