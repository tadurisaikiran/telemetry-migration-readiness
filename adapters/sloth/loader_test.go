package sloth

import (
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
)

func TestParseDiscoversCriticalSLOReferences(t *testing.T) {
	t.Parallel()

	specification := `version: prometheus/v1
service: checkout
slos:
  - name: availability
    objective: 99.9
    sli:
      events:
        error_query: sum(rate(checkout_requests_total{status=~"5.."}[5m]))
        total_query: sum(rate(checkout_requests_total[5m]))
`
	discovery, err := (Loader{Required: true}).Parse("checkout.yaml", strings.NewReader(specification))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(discovery.Consumers), 1; got != want {
		t.Fatalf("len(Consumers) = %d, want %d", got, want)
	}
	if discovery.Consumers[0].Criticality != domain.CriticalityCritical {
		t.Errorf("Criticality = %q, want critical", discovery.Consumers[0].Criticality)
	}
	if len(discovery.References) == 0 || len(discovery.Diagnostics) != 0 {
		t.Fatalf("unexpected discovery: %+v", discovery)
	}
}

func TestParseResolvesSlothWindowTemplateDeterministically(t *testing.T) {
	t.Parallel()

	specification := `version: prometheus/v1
service: checkout
slos:
  - name: availability
    objective: 99.9
    sli:
      events:
        error_query: sum(rate(checkout_requests_total{status=~"5.."}[{{ .window }}]))
        total_query: sum(rate(checkout_requests_total[{{.window}}]))
`
	discovery, err := (Loader{Required: true}).Parse("checkout.yaml", strings.NewReader(specification))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(discovery.Diagnostics) != 0 || len(discovery.References) == 0 {
		t.Fatalf("window template was not resolved safely: %+v", discovery)
	}
	if discovery.Consumers[0].Unresolved {
		t.Fatal("known Sloth window template made the consumer unresolved")
	}
}
