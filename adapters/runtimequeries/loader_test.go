package runtimequeries

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
)

func TestPrometheusQueryLogAggregatesBoundedRuntimeEvidence(t *testing.T) {
	t.Parallel()

	input := `{"params":{"query":"sum by (method) (rate(old_metric{method=\"GET\"}[5m]))"},"ts":"2026-08-24T10:00:00Z","httpRequest":{"clientIP":"sensitive-client-ip","method":"GET","path":"/api/v1/query_range"}}
{"params":{"query":"sum by (method) (rate(old_metric{method=\"GET\"}[5m]))"},"ts":"2026-08-24T11:00:00Z","ruleGroup":{"file":"rules/checkout.yml","name":"checkout"}}
{"params":{"query":"new_metric"},"ts":"2026-08-24T12:00:00Z","httpRequest":{"method":"POST","path":"/api/v1/query"}}
`
	loader := Loader{
		Required:    true,
		Format:      FormatPrometheusQueryLog,
		Window:      2 * time.Hour,
		Criticality: domain.CriticalityHigh,
	}
	discovery, err := loader.Parse(context.Background(), "query.log", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", discovery.Diagnostics)
	}
	if got, want := len(discovery.Consumers), 2; got != want {
		t.Fatalf("consumers = %d, want %d", got, want)
	}
	old := findRuntimeConsumer(t, discovery, "old_metric")
	if old.Runtime == nil {
		t.Fatal("runtime evidence = nil")
	}
	if old.Runtime.ExecutionCount != 2 || old.Runtime.FirstSeen != "2026-08-24T10:00:00Z" || old.Runtime.LastSeen != "2026-08-24T11:00:00Z" {
		t.Fatalf("runtime evidence = %#v", old.Runtime)
	}
	if old.Runtime.Window != "2h0m0s" || old.Runtime.WindowStart != "2026-08-24T10:00:00Z" || old.Runtime.WindowAnchor != "2026-08-24T12:00:00Z" || old.Runtime.ExecutionsPerDay != "24.000000" {
		t.Fatalf("window evidence = %#v", old.Runtime)
	}
	if got, want := old.Runtime.Origins, []string{"prometheus_api", "prometheus_rule_group"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("origins = %v, want %v", got, want)
	}
	if old.Source.Line != 2 || old.Criticality != domain.CriticalityHigh {
		t.Fatalf("consumer = %#v", old)
	}
	if !hasRuntimeReference(discovery.References, old.ID, domain.SymbolKindMetric, "old_metric") ||
		!hasRuntimeReference(discovery.References, old.ID, domain.SymbolKindLabel, "method") {
		t.Fatalf("references = %#v", discovery.References)
	}
	for _, reference := range discovery.References {
		if reference.ConsumerID == old.ID && reference.Evidence.Method != domain.EvidenceMethodRuntimeQuery {
			t.Fatalf("evidence method = %q", reference.Evidence.Method)
		}
	}
	encoded, err := json.Marshal(discovery)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "sensitive-client-ip") {
		t.Fatal("client IP leaked into normalized evidence")
	}
}

func TestRuntimeWindowAnchorsToDataNotWallClock(t *testing.T) {
	t.Parallel()

	input := `{"params":{"query":"stale_metric"},"ts":"2000-01-01T00:00:00Z"}
{"params":{"query":"recent_metric"},"ts":"2000-01-03T00:00:00Z"}
`
	loader := Loader{Format: FormatPrometheusQueryLog, Window: 24 * time.Hour}
	first, err := loader.Parse(context.Background(), "query.log", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	second, err := loader.Parse(context.Background(), "query.log", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("runtime evidence is nondeterministic:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if len(first.Consumers) != 1 || first.Consumers[0].Expression != "recent_metric" {
		t.Fatalf("consumers = %#v", first.Consumers)
	}
	if first.Consumers[0].Runtime.WindowAnchor != "2000-01-03T00:00:00Z" {
		t.Fatalf("runtime = %#v", first.Consumers[0].Runtime)
	}
}

func TestTMRQueryHistoryIsStrictAndKeepsValidRecords(t *testing.T) {
	t.Parallel()

	input := `{"schemaVersion":"tmr-runtime-query/v1alpha1","timestamp":"2026-08-24T12:00:00Z","query":"checkout_requests_total","origin":"grafana_query_history","source":"grafana-prod"}
{"schemaVersion":"tmr-runtime-query/v1alpha1","timestamp":"2026-08-24T12:01:00Z","query":"ignored_metric","origin":"grafana_query_history","unknown":true}
{"schemaVersion":"wrong","timestamp":"2026-08-24T12:02:00Z","query":"ignored_metric","origin":"backend"}
{"schemaVersion":"tmr-runtime-query/v1alpha1","timestamp":"2026-08-24T12:03:00Z","query":"ignored_metric","origin":"bad origin"}
`
	discovery, err := (Loader{Required: true, Format: FormatTMRQueryHistory}).Parse(
		context.Background(),
		"history.jsonl",
		strings.NewReader(input),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Consumers) != 1 || discovery.Consumers[0].Expression != "checkout_requests_total" {
		t.Fatalf("consumers = %#v", discovery.Consumers)
	}
	if got, want := len(discovery.Diagnostics), 3; got != want {
		t.Fatalf("diagnostics = %d, want %d: %#v", got, want, discovery.Diagnostics)
	}
	for _, diagnostic := range discovery.Diagnostics {
		if !diagnostic.Required || diagnostic.Source.Line < 2 {
			t.Fatalf("diagnostic = %#v", diagnostic)
		}
	}
	runtimeEvidence := discovery.Consumers[0].Runtime
	if !reflect.DeepEqual(runtimeEvidence.Origins, []string{"grafana_query_history"}) ||
		!reflect.DeepEqual(runtimeEvidence.OriginDetails, []string{"grafana-prod"}) {
		t.Fatalf("runtime = %#v", runtimeEvidence)
	}
}

func TestUnresolvedObservedQueryFailsClosedWhenRequired(t *testing.T) {
	t.Parallel()

	input := `{"params":{"query":"rate(${service}_requests_total[5m])"},"ts":"2026-08-24T12:00:00Z"}`
	discovery, err := (Loader{Required: true, Format: FormatPrometheusQueryLog}).Parse(
		context.Background(),
		"query.log",
		strings.NewReader(input),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Consumers) != 1 || !discovery.Consumers[0].Unresolved {
		t.Fatalf("consumers = %#v", discovery.Consumers)
	}
	if len(discovery.Diagnostics) != 1 || !discovery.Diagnostics[0].Required {
		t.Fatalf("diagnostics = %#v", discovery.Diagnostics)
	}
}

func TestRuntimeQueryParserEnforcesBoundsAndFormat(t *testing.T) {
	t.Parallel()

	tooLong := strings.Repeat("x", maxQueryBytes+1)
	line, err := json.Marshal(map[string]any{
		"params": map[string]string{"query": tooLong},
		"ts":     "2026-08-24T12:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := (Loader{Required: true, Format: FormatPrometheusQueryLog}).Parse(
		context.Background(),
		"query.log",
		strings.NewReader(string(line)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Consumers) != 0 || len(discovery.Diagnostics) != 1 ||
		!strings.Contains(discovery.Diagnostics[0].Message, "query exceeds") {
		t.Fatalf("discovery = %#v", discovery)
	}
	oversizedLine, err := (Loader{Required: true, Format: FormatPrometheusQueryLog}).Parse(
		context.Background(),
		"query.log",
		strings.NewReader(strings.Repeat("x", maxRuntimeQueryLineBytes+1)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(oversizedLine.Diagnostics) != 1 || !strings.Contains(oversizedLine.Diagnostics[0].Message, "line limit") {
		t.Fatalf("oversized line discovery = %#v", oversizedLine)
	}
	if _, err := (Loader{Format: "unknown"}).Parse(context.Background(), "query.log", strings.NewReader("")); err == nil {
		t.Fatal("unknown format error = nil")
	}
}

func TestEmptyRuntimeSourceIsNotNegativeEvidence(t *testing.T) {
	t.Parallel()

	discovery, err := (Loader{Required: true, Format: FormatPrometheusQueryLog}).Parse(
		context.Background(),
		"query.log",
		strings.NewReader("\n"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Consumers) != 0 || len(discovery.References) != 0 || len(discovery.Diagnostics) != 0 {
		t.Fatalf("empty source created evidence: %#v", discovery)
	}
}

func FuzzDecodePrometheusQueryLog(f *testing.F) {
	f.Add(`{"params":{"query":"up"},"ts":"2026-08-24T12:00:00Z"}`)
	f.Add(`{"params":{"query":"${metric}"},"ts":"bad"}`)
	f.Fuzz(func(t *testing.T, input string) {
		_, _ = decodeObservedEvent(FormatPrometheusQueryLog, []byte(input), 1)
	})
}

func FuzzDecodeTMRQueryHistory(f *testing.F) {
	f.Add(`{"schemaVersion":"tmr-runtime-query/v1alpha1","timestamp":"2026-08-24T12:00:00Z","query":"up","origin":"grafana"}`)
	f.Fuzz(func(t *testing.T, input string) {
		_, _ = decodeObservedEvent(FormatTMRQueryHistory, []byte(input), 1)
	})
}

func findRuntimeConsumer(t *testing.T, discovery domain.Discovery, contains string) domain.Consumer {
	t.Helper()
	for _, consumer := range discovery.Consumers {
		if strings.Contains(consumer.Expression, contains) {
			return consumer
		}
	}
	t.Fatalf("no consumer expression contains %q: %#v", contains, discovery.Consumers)
	return domain.Consumer{}
}

func hasRuntimeReference(references []domain.Reference, consumerID string, kind domain.SymbolKind, name string) bool {
	for _, reference := range references {
		if reference.ConsumerID == consumerID && reference.Symbol.Kind == kind && reference.Symbol.Name == name {
			return true
		}
	}
	return false
}
