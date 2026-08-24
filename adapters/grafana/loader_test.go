package grafana

import (
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
)

func TestParseDiscoversNestedPanelsAndFailsClosedOnTemplate(t *testing.T) {
	t.Parallel()

	dashboard := `{
  "uid": "checkout",
  "title": "Checkout",
  "tags": ["critical", "team:checkout", "critical"],
  "panels": [{
    "id": 1,
    "title": "row",
    "panels": [{
      "id": 2,
      "title": "latency",
      "datasource": {"type":"prometheus"},
      "targets": [
        {"refId":"A", "expr":"rate(checkout_requests_total{method=\"GET\"}[5m])"},
        {"refId":"B", "expr":"rate(${service}_requests_total[5m])"}
      ]
    }]
  }]
}`
	discovery, err := (Loader{Required: true}).Parse("checkout.json", strings.NewReader(dashboard))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(discovery.Consumers), 2; got != want {
		t.Fatalf("len(Consumers) = %d, want %d", got, want)
	}
	if got, want := discovery.Consumers[0].Criticality, domain.CriticalityCritical; got != want {
		t.Errorf("Criticality = %q, want %q", got, want)
	}
	if got, want := discovery.Consumers[0].Metadata["dashboard_tags"], `["critical","team:checkout"]`; got != want {
		t.Errorf("dashboard_tags = %q, want %q", got, want)
	}
	if got, want := len(discovery.Diagnostics), 1; got != want {
		t.Fatalf("len(Diagnostics) = %d, want %d", got, want)
	}
	if !discovery.Diagnostics[0].Required || !discovery.Consumers[1].Unresolved {
		t.Error("templated query did not become required unresolved evidence")
	}
	if !hasGrafanaReference(discovery.References, domain.SymbolKindMetric, "checkout_requests_total") {
		t.Error("metric reference was not discovered")
	}
	if !hasGrafanaReference(discovery.References, domain.SymbolKindLabel, "method") {
		t.Error("label reference was not discovered")
	}
}

func TestParseSkipsKnownNonPrometheusDatasource(t *testing.T) {
	t.Parallel()

	dashboard := `{"title":"Logs","panels":[{"id":1,"title":"logs","datasource":{"type":"loki"},"targets":[{"expr":"{app=\"checkout\"}"}]}]}`
	discovery, err := (Loader{Required: true}).Parse("logs.json", strings.NewReader(dashboard))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(discovery.Consumers) != 0 || len(discovery.Diagnostics) != 0 {
		t.Fatalf("known Loki target was not skipped: %+v", discovery)
	}
}

func TestParseRejectsCorruptJSON(t *testing.T) {
	t.Parallel()

	if _, err := (Loader{Required: true}).Parse("corrupt.json", strings.NewReader(`{"panels": [`)); err == nil {
		t.Fatal("Parse() accepted corrupt Grafana JSON")
	}
}

func hasGrafanaReference(references []domain.Reference, kind domain.SymbolKind, name string) bool {
	for _, reference := range references {
		if reference.Symbol.Kind == kind && reference.Symbol.Name == name {
			return true
		}
	}
	return false
}
