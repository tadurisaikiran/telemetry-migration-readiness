package remediation

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-migration-readiness/adapters/grafana"
	"github.com/tadurisaikiran/telemetry-migration-readiness/adapters/prometheusrules"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/impact"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/readiness"
)

func TestValidatePrometheusYAMLCandidateReparsesAndReanalyzes(t *testing.T) {
	t.Parallel()

	fixture := newRuleFixture(t, false)
	request, err := BuildRequest(fixture.result)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(request.Targets), 1; got != want {
		t.Fatalf("targets = %d, want %d: %#v", got, want, request.Targets)
	}
	target := request.Targets[0]
	if target.ConsumerName != "checkout:rate1m" || target.ArtifactKind != ArtifactKindPrometheusYAML {
		t.Fatalf("target = %#v", target)
	}
	response := validResponse(target, "rate(new_metric[1m])")
	validated, err := Validate(
		context.Background(), request, response, fixture.migration, fixture.discovery, fixture.policy,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(validated), 1; got != want {
		t.Fatalf("validated = %d, want %d", got, want)
	}
	candidate := validated[0]
	if candidate.Locator.Line == 0 || candidate.Locator.JSONPointer != "" {
		t.Fatalf("YAML locator = %#v", candidate.Locator)
	}
	if candidate.Validation.CurrentStatus != readiness.StatusBlocked ||
		candidate.Validation.SimulatedStatus != readiness.StatusReady ||
		candidate.Validation.SimulatedClass != readiness.ClassificationMigrated {
		t.Fatalf("validation = %#v", candidate.Validation)
	}
	contents, err := os.ReadFile(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(contents, []byte("rate(old_metric[1m])")) {
		t.Fatal("source file was modified")
	}
}

func TestValidateGrafanaCandidateUsesExactJSONPointer(t *testing.T) {
	t.Parallel()

	fixture := newGrafanaFixture(t)
	request, err := BuildRequest(fixture.result)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Targets) != 1 {
		t.Fatalf("targets = %#v", request.Targets)
	}
	target := request.Targets[0]
	response := validResponse(target, `sum(rate(new_metric{method="GET"}[5m]))`)
	validated, err := Validate(
		context.Background(), request, response, fixture.migration, fixture.discovery, fixture.policy,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := validated[0].Locator.JSONPointer, "/panels/0/targets/0/expr"; got != want {
		t.Fatalf("JSON pointer = %q, want %q", got, want)
	}
	if validated[0].Validation.SimulatedStatus != readiness.StatusReady {
		t.Fatalf("validation = %#v", validated[0].Validation)
	}
	contents, err := os.ReadFile(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(contents, []byte("old_metric")) {
		t.Fatal("source dashboard was modified")
	}
}

func TestValidateLabelRenameCandidate(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "labels.yaml")
	contents := `groups:
  - name: labels
    rules:
      - record: checkout:requests
        expr: sum(rate(new_metric{old_label="GET"}[1m]))
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	discovery, err := (prometheusrules.Loader{Required: true}).LoadFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	newLabel := domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindLabel, Name: "new_label", Parent: "new_metric"}
	migration := domain.Migration{
		APIVersion: domain.MigrationAPIVersion,
		Kind:       domain.MigrationKind,
		Metadata:   domain.MigrationMetadata{Name: "labels"},
		Changes: []domain.Change{{
			ID:     "label",
			Kind:   domain.ChangeKindLabelRename,
			Domain: domain.DomainPrometheus,
			From:   domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindLabel, Name: "old_label", Parent: "new_metric"},
			To:     &newLabel,
		}},
	}
	policy := readiness.Policy{FailOnCriticalLegacyConsumer: true, FailOnCriticalUnknown: true, MinimumBlockingCriticality: domain.CriticalityHigh, IncludeTransitive: true}
	graph, err := impact.BuildGraph(discovery)
	if err != nil {
		t.Fatal(err)
	}
	result, err := readiness.Evaluate(migration, discovery, graph, policy)
	if err != nil {
		t.Fatal(err)
	}
	request, err := BuildRequest(result)
	if err != nil {
		t.Fatal(err)
	}
	validated, err := Validate(
		context.Background(),
		request,
		validResponse(request.Targets[0], `sum(rate(new_metric{new_label="GET"}[1m]))`),
		migration,
		discovery,
		policy,
	)
	if err != nil {
		t.Fatal(err)
	}
	if validated[0].Validation.SimulatedClass != readiness.ClassificationMigrated {
		t.Fatalf("validation = %#v", validated[0].Validation)
	}
}

func TestValidateRejectsUnsafeOrUnprovenCandidates(t *testing.T) {
	t.Parallel()

	fixture := newRuleFixture(t, false)
	request, err := BuildRequest(fixture.result)
	if err != nil {
		t.Fatal(err)
	}
	target := request.Targets[0]
	tests := []struct {
		name  string
		after string
		want  string
	}{
		{name: "invalid PromQL", after: "rate(", want: "invalid"},
		{name: "legacy retained", after: "old_metric or new_metric", want: "still references legacy"},
		{name: "destination missing", after: "other_metric", want: "does not reference destination"},
		{name: "secret-like text", after: `new_metric{token="provider-secret"}`, want: "secret-like"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Validate(
				context.Background(),
				request,
				validResponse(target, test.after),
				fixture.migration,
				fixture.discovery,
				fixture.policy,
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateRejectsAmbiguousArtifactScalar(t *testing.T) {
	t.Parallel()

	fixture := newRuleFixture(t, true)
	request, err := BuildRequest(fixture.result)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Validate(
		context.Background(),
		request,
		validResponse(request.Targets[0], "rate(new_metric[1m])"),
		fixture.migration,
		fixture.discovery,
		fixture.policy,
	)
	if err == nil || !strings.Contains(err.Error(), "matched 2 scalar values") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateRejectsForgedTargetSourceOrMigration(t *testing.T) {
	t.Parallel()

	fixture := newRuleFixture(t, false)
	originalRequest, err := BuildRequest(fixture.result)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Target){
		func(target *Target) { target.Source.File = filepath.Join(t.TempDir(), "invented.yaml") },
		func(target *Target) { target.To.Name = "invented_metric" },
	} {
		request := originalRequest
		request.Targets = append([]Target(nil), originalRequest.Targets...)
		mutate(&request.Targets[0])
		_, err := Validate(
			context.Background(),
			request,
			validResponse(request.Targets[0], "rate(new_metric[1m])"),
			fixture.migration,
			fixture.discovery,
			fixture.policy,
		)
		if err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("error = %v", err)
		}
	}
}

func TestBuildRequestExcludesTransitiveAndRemovalTargets(t *testing.T) {
	t.Parallel()

	fixture := newRuleFixture(t, false)
	request, err := BuildRequest(fixture.result)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range request.Targets {
		if target.ConsumerName == "TrafficStopped" {
			t.Fatal("transitive-only alert was offered as a direct patch target")
		}
	}
	removal := fixture.migration
	removal.Changes[0].Kind = domain.ChangeKindMetricRemove
	removal.Changes[0].To = nil
	graph, err := impact.BuildGraph(fixture.discovery)
	if err != nil {
		t.Fatal(err)
	}
	result, err := readiness.Evaluate(removal, fixture.discovery, graph, fixture.policy)
	if err != nil {
		t.Fatal(err)
	}
	request, err = BuildRequest(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Targets) != 0 {
		t.Fatalf("removal targets = %#v", request.Targets)
	}
}

func TestRenderLabelsCandidateAndPreservesCurrentStatus(t *testing.T) {
	t.Parallel()

	fixture := newRuleFixture(t, false)
	request, _ := BuildRequest(fixture.result)
	validated, err := Validate(
		context.Background(), request, validResponse(request.Targets[0], "rate(new_metric[1m])"),
		fixture.migration, fixture.discovery, fixture.policy,
	)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Render(&output, request, validated); err != nil {
		t.Fatal(err)
	}
	rendered := output.String()
	for _, expected := range []string{
		"VALIDATED CANDIDATE", "NO FILES WERE MODIFIED", "Simulated status", "Current authoritative status remains: BLOCKED",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("output missing %q:\n%s", expected, rendered)
		}
	}
}

type remediationFixture struct {
	path      string
	migration domain.Migration
	discovery domain.Discovery
	result    readiness.Result
	policy    readiness.Policy
}

func newRuleFixture(t *testing.T, ambiguous bool) remediationFixture {
	t.Helper()
	annotation := ""
	if ambiguous {
		annotation = "\n        annotations:\n          query: rate(old_metric[1m])"
	}
	contents := `groups:
  - name: checkout
    rules:
      - record: checkout:rate1m
        expr: rate(old_metric[1m])` + annotation + `
      - alert: TrafficStopped
        expr: checkout:rate1m == 0
        labels:
          severity: critical
`
	path := filepath.Join(t.TempDir(), "rules.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	discovery, err := (prometheusrules.Loader{Required: true}).LoadFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return evaluateFixture(t, path, discovery)
}

func newGrafanaFixture(t *testing.T) remediationFixture {
	t.Helper()
	document := map[string]any{
		"uid":   "checkout",
		"title": "Checkout",
		"tags":  []string{"critical"},
		"panels": []any{map[string]any{
			"id":    1,
			"title": "Requests",
			"targets": []any{map[string]any{
				"refId": "A",
				"expr":  `sum(rate(old_metric{method="GET"}[5m]))`,
			}},
		}},
	}
	contents, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "dashboard.json")
	if err := os.WriteFile(path, append(contents, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	discovery, err := (grafana.Loader{Required: true}).LoadFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return evaluateFixture(t, path, discovery)
}

func evaluateFixture(t *testing.T, path string, discovery domain.Discovery) remediationFixture {
	t.Helper()
	newMetric := domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: "new_metric"}
	migration := domain.Migration{
		APIVersion: domain.MigrationAPIVersion,
		Kind:       domain.MigrationKind,
		Metadata:   domain.MigrationMetadata{Name: "candidate"},
		Changes: []domain.Change{{
			ID:     "rename",
			Kind:   domain.ChangeKindMetricRename,
			Domain: domain.DomainPrometheus,
			From:   domain.Symbol{Domain: domain.DomainPrometheus, Kind: domain.SymbolKindMetric, Name: "old_metric"},
			To:     &newMetric,
		}},
	}
	policy := readiness.Policy{
		FailOnCriticalLegacyConsumer: true,
		FailOnCriticalUnknown:        true,
		MinimumBlockingCriticality:   domain.CriticalityHigh,
		IncludeTransitive:            true,
	}
	graph, err := impact.BuildGraph(discovery)
	if err != nil {
		t.Fatal(err)
	}
	result, err := readiness.Evaluate(migration, discovery, graph, policy)
	if err != nil {
		t.Fatal(err)
	}
	return remediationFixture{path: path, migration: migration, discovery: discovery, result: result, policy: policy}
}

func validResponse(target Target, after string) Response {
	return Response{
		SchemaVersion: ResponseSchemaVersion,
		Candidates: []Candidate{{
			ID:               "candidate-1",
			TargetID:         target.ID,
			BeforeExpression: target.BeforeExpression,
			AfterExpression:  after,
			Rationale:        "Replace the confirmed legacy selector with its explicit migration destination.",
		}},
	}
}
