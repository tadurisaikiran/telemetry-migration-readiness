package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/graph"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/readiness"
)

func TestRenderersPreserveStatusEvidenceAndPaths(t *testing.T) {
	t.Parallel()

	result := fixtureResult()
	contents, err := JSON(result)
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	var decoded readiness.Result
	if err := json.Unmarshal(contents, &decoded); err != nil {
		t.Fatalf("JSON output is invalid: %v", err)
	}
	if decoded.SchemaVersion != readiness.ResultSchemaVersion {
		t.Errorf("schemaVersion = %q", decoded.SchemaVersion)
	}

	for name, render := range map[string]func(*bytes.Buffer, readiness.Result) error{
		"console":  func(output *bytes.Buffer, result readiness.Result) error { return Console(output, result) },
		"markdown": func(output *bytes.Buffer, result readiness.Result) error { return Markdown(output, result) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			if err := render(&output, result); err != nil {
				t.Fatalf("render error = %v", err)
			}
			for _, expected := range []string{"BLOCKED", "CheckoutLatencyHigh", "Checkout Platform", "checkout_request_duration_seconds", "checkout:p95_latency"} {
				if !strings.Contains(output.String(), expected) {
					t.Errorf("output does not contain %q:\n%s", expected, output.String())
				}
			}
		})
	}
}

func TestGraphJSONIsStable(t *testing.T) {
	t.Parallel()

	target := graph.New()
	symbol := domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: "requests_total"}
	consumer := domain.Consumer{ID: "alert:a", Kind: domain.ConsumerKindAlertRule, Name: "A"}
	if err := target.AddNode(graph.Node{ID: "consumer:alert:a", Kind: graph.NodeKindConsumer, Name: "A", Consumer: &consumer}); err != nil {
		t.Fatal(err)
	}
	if err := target.AddNode(graph.Node{ID: graph.SymbolNodeID(symbol), Kind: graph.NodeKindSymbol, Name: symbol.Name, Symbol: &symbol}); err != nil {
		t.Fatal(err)
	}
	if err := target.AddEdge(graph.Edge{From: graph.SymbolNodeID(symbol), To: "consumer:alert:a", Kind: graph.EdgeReferences}); err != nil {
		t.Fatal(err)
	}

	first, err := GraphJSON(target)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GraphJSON(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("GraphJSON output is not deterministic")
	}
}

func fixtureResult() readiness.Result {
	consumer := domain.Consumer{
		ID:          "alert:checkout",
		Kind:        domain.ConsumerKindAlertRule,
		Name:        "CheckoutLatencyHigh",
		Source:      domain.SourceLocation{File: "rules/checkout.yaml", Line: 12},
		Criticality: domain.CriticalityCritical,
		Owner:       &domain.Owner{Name: "Checkout Platform", Email: "checkout@example.com"},
	}
	oldSymbol := domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: "checkout_request_duration_seconds"}
	newSymbol := domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: "checkout_server_request_duration_seconds"}
	change := domain.Change{ID: "duration", Kind: domain.ChangeKindMetricRename, Domain: domain.DomainPrometheus, From: oldSymbol, To: &newSymbol}
	return readiness.Result{
		SchemaVersion: readiness.ResultSchemaVersion,
		Migration: domain.Migration{
			APIVersion: domain.MigrationAPIVersion,
			Kind:       domain.MigrationKind,
			Metadata:   domain.MigrationMetadata{Name: "checkout"},
			Changes:    []domain.Change{change},
		},
		Summary: readiness.Summary{Status: readiness.StatusBlocked, TotalConsumers: 1, LegacyOnly: 1},
		Changes: []readiness.ChangeResult{{
			Change: change,
			Status: readiness.StatusBlocked,
			Consumers: []readiness.ConsumerResult{{
				Consumer:       consumer,
				Classification: readiness.ClassificationLegacyOnly,
				References: []domain.Reference{{
					ConsumerID: consumer.ID,
					Symbol:     oldSymbol,
					Evidence: domain.Evidence{
						Method:     domain.EvidenceMethodPromQLAST,
						Confidence: domain.ConfidenceConfirmed,
						Source:     consumer.Source,
					},
				}},
				Paths: []graph.Path{{Nodes: []string{"checkout_request_duration_seconds", "checkout:p95_latency", "CheckoutLatencyHigh"}}},
			}},
		}},
	}
}
