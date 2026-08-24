package tempo

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	tmrtraceql "github.com/tadurisaikiran/telemetry-migration-readiness/pkg/traceql"
)

type validatorFunc func(context.Context, string) error

func (function validatorFunc) Validate(ctx context.Context, expression string) error {
	return function(ctx, expression)
}

func TestLoaderImportsValidatedScopedTraceQLWithExplicitMapping(t *testing.T) {
	t.Parallel()

	var validated []string
	manifest := `apiVersion: tmr.tempo/v1alpha1
kind: TraceQueries
queries:
  - id: checkout-traces
    name: Checkout trace search
    criticality: critical
    expression: '{ span.http.method = "GET" && resource.service.name = "checkout" }'
`
	discovery, err := (Loader{
		Required:           true,
		DefaultCriticality: domain.CriticalityHigh,
		TempoURL:           "https://tempo.example.com",
		Validator: validatorFunc(func(_ context.Context, expression string) error {
			validated = append(validated, expression)
			return nil
		}),
		Mappings: []AttributeMapping{
			{Scope: tmrtraceql.ScopeSpan, OpenTelemetry: "http.request.method", Tempo: "http.method"},
			{Scope: tmrtraceql.ScopeResource, OpenTelemetry: "service.name", Tempo: "service.name"},
		},
	}).Parse(context.Background(), "queries.yaml", strings.NewReader(manifest))
	if err != nil {
		t.Fatal(err)
	}
	if len(validated) != 1 || len(discovery.Consumers) != 1 || len(discovery.Diagnostics) != 0 {
		t.Fatalf("validated = %#v discovery = %#v", validated, discovery)
	}
	consumer := discovery.Consumers[0]
	if consumer.Criticality != domain.CriticalityCritical || consumer.Source.URL != "https://tempo.example.com" || consumer.Metadata["query_id"] != "checkout-traces" {
		t.Fatalf("consumer = %#v", consumer)
	}
	for _, expected := range []domain.Symbol{
		{Domain: domain.DomainTempo, Kind: domain.SymbolKindSpanAttribute, Name: "http.method"},
		{Domain: domain.DomainOpenTelemetry, Kind: domain.SymbolKindSpanAttribute, Name: "http.request.method"},
		{Domain: domain.DomainTempo, Kind: domain.SymbolKindResourceAttribute, Name: "service.name"},
		{Domain: domain.DomainOpenTelemetry, Kind: domain.SymbolKindResourceAttribute, Name: "service.name"},
	} {
		if !hasSymbol(discovery.References, expected) {
			t.Errorf("missing symbol %#v in %#v", expected, discovery.References)
		}
	}
}

func TestLoaderFailsClosedOnValidationAndConservativeExtraction(t *testing.T) {
	t.Parallel()

	manifest := `apiVersion: tmr.tempo/v1alpha1
kind: TraceQueries
queries:
  - id: rejected
    name: Rejected query
    expression: '{ span.old = "x" }'
  - id: unscoped
    name: Unscoped query
    expression: '{ .old = "x" }'
`
	discovery, err := (Loader{
		Required: true,
		Validator: validatorFunc(func(_ context.Context, expression string) error {
			if strings.Contains(expression, "span.old") {
				return errors.New("official parser rejection")
			}
			return nil
		}),
	}).Parse(context.Background(), "queries.yaml", strings.NewReader(manifest))
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Consumers) != 2 || len(discovery.Diagnostics) != 2 || len(discovery.References) != 0 {
		t.Fatalf("discovery = %#v", discovery)
	}
	for _, consumer := range discovery.Consumers {
		if !consumer.Unresolved {
			t.Fatalf("consumer = %#v, want unresolved", consumer)
		}
	}
	for _, diagnostic := range discovery.Diagnostics {
		if !diagnostic.Required {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}
}

func TestLoaderStrictManifestValidation(t *testing.T) {
	t.Parallel()

	validator := validatorFunc(func(context.Context, string) error { return nil })
	for _, test := range []struct {
		name     string
		manifest string
		want     string
	}{
		{name: "unknown field", manifest: "apiVersion: tmr.tempo/v1alpha1\nkind: TraceQueries\nunknown: true\nqueries: []\n", want: "field unknown"},
		{name: "wrong version", manifest: "apiVersion: wrong\nkind: TraceQueries\nqueries: [{id: q, name: Query, expression: '{}'}]\n", want: "apiVersion"},
		{name: "duplicate", manifest: "apiVersion: tmr.tempo/v1alpha1\nkind: TraceQueries\nqueries: [{id: q, name: One, expression: '{}'}, {id: q, name: Two, expression: '{}'}]\n", want: "duplicates"},
		{name: "bad criticality", manifest: "apiVersion: tmr.tempo/v1alpha1\nkind: TraceQueries\nqueries: [{id: q, name: Query, expression: '{}', criticality: urgent}]\n", want: "criticality"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := (Loader{Validator: validator}).Parse(context.Background(), "queries.yaml", strings.NewReader(test.manifest))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func hasSymbol(references []domain.Reference, symbol domain.Symbol) bool {
	for _, reference := range references {
		if reference.Symbol == symbol {
			return true
		}
	}
	return false
}
