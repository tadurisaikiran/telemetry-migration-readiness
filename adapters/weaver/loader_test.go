package weaver

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
)

func TestLoadMigrationMapsV1AndV2Identically(t *testing.T) {
	t.Parallel()

	for _, diffPath := range []string{"testdata/diff-v1.json", "testdata/diff-v2.json"} {
		diffPath := diffPath
		t.Run(diffPath, func(t *testing.T) {
			t.Parallel()
			migration, result, err := LoadMigration(context.Background(), diffPath, "testdata/mapping.yaml")
			if err != nil {
				t.Fatal(err)
			}
			if got, want := len(migration.Changes), 2; got != want {
				t.Fatalf("canonical changes = %d, want %d", got, want)
			}
			if got, want := len(result.Changes), 3; got != want {
				t.Fatalf("imported changes = %d, want %d", got, want)
			}
			if migration.Changes[0].Kind != domain.ChangeKindLabelRename ||
				migration.Changes[0].From.Name != "http_method" ||
				migration.Changes[0].Metadata["source.adapter"] != "weaver" {
				t.Fatalf("label change = %#v", migration.Changes[0])
			}
			if migration.Changes[1].Kind != domain.ChangeKindMetricRename ||
				migration.Changes[1].From.Name != "http_server_duration_seconds" {
				t.Fatalf("metric change = %#v", migration.Changes[1])
			}
			if !result.Changes[2].Ignored || result.Changes[2].IgnoreReason == "" {
				t.Fatalf("ignored change = %#v", result.Changes[2])
			}
		})
	}
}

func TestConvertMapsMetricAndLabelRemovals(t *testing.T) {
	t.Parallel()

	diff := Diff{
		Format:   DiffFormatV2,
		Baseline: "https://example.test/1",
		Head:     "https://example.test/2",
		Changes: []SourceChange{
			{Kind: SourceKindMetric, Type: "removed", From: "legacy.metric", Format: DiffFormatV2},
			{Kind: SourceKindAttribute, Type: "obsoleted", From: "legacy.attribute", Format: DiffFormatV2},
		},
	}
	mapping := Mapping{
		Name: "removals",
		Entries: []MappingEntry{
			{
				ID:     "legacy-metric",
				Weaver: SourceSelector{Kind: SourceKindMetric, Type: "removed", From: "legacy.metric"},
				Prometheus: &PrometheusChange{
					Kind: domain.ChangeKindMetricRemove,
					From: MappingSymbol{Metric: "legacy_metric_total"},
				},
			},
			{
				ID:     "legacy-label",
				Weaver: SourceSelector{Kind: SourceKindAttribute, Type: "obsoleted", From: "legacy.attribute"},
				Prometheus: &PrometheusChange{
					Kind:   domain.ChangeKindLabelRemove,
					Metric: "requests_total",
					From:   MappingSymbol{Label: "legacy_attribute"},
				},
			},
		},
	}
	result, err := Convert(diff, mapping)
	if err != nil {
		t.Fatal(err)
	}
	migration, err := result.Migration(mapping.Name)
	if err != nil {
		t.Fatal(err)
	}
	if migration.Changes[0].Kind != domain.ChangeKindMetricRemove || migration.Changes[0].To != nil {
		t.Fatalf("metric removal = %#v", migration.Changes[0])
	}
	if migration.Changes[1].Kind != domain.ChangeKindLabelRemove ||
		migration.Changes[1].From.Parent != "requests_total" || migration.Changes[1].To != nil {
		t.Fatalf("label removal = %#v", migration.Changes[1])
	}
}

func TestConvertRequiresMappingForEveryActionableChange(t *testing.T) {
	t.Parallel()

	diff := loadTestDiff(t, "testdata/diff-v2.json")
	mapping := loadTestMapping(t, "testdata/mapping.yaml")
	mapping.Entries = mapping.Entries[:2]
	result, err := Convert(diff, mapping)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changes[2].RequiresMapping {
		t.Fatalf("third change = %#v, want requiresMapping=true", result.Changes[2])
	}
	_, err = result.Migration(mapping.Name)
	var mappingErr *MappingRequiredError
	if !errors.As(err, &mappingErr) || !strings.Contains(err.Error(), "requiresMapping=true") {
		t.Fatalf("error = %v, want MappingRequiredError", err)
	}
}

func TestUnsupportedSignalKindRequiresExplicitIgnore(t *testing.T) {
	t.Parallel()

	diff, err := ParseDiff(strings.NewReader(`{
  "head_schema_url":{"url":"https://example.test/2"},
  "baseline_schema_url":{"url":"https://example.test/1"},
  "registry":{
    "attribute_changes":[],"attribute_group_changes":[],"entity_changes":[],
    "event_changes":[{"type":"removed","name":"session.started"}],
    "metric_changes":[],"span_changes":[]
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Convert(diff, Mapping{Name: "event-change"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 1 || result.Changes[0].Source.Kind != SourceKindEvent ||
		!result.Changes[0].RequiresMapping {
		t.Fatalf("result = %#v", result)
	}
	_, err = result.Migration("event-change")
	var mappingErr *MappingRequiredError
	if !errors.As(err, &mappingErr) {
		t.Fatalf("error = %v, want MappingRequiredError", err)
	}
}

func TestConvertRejectsStaleMappingEntry(t *testing.T) {
	t.Parallel()

	diff := loadTestDiff(t, "testdata/diff-v2.json")
	mapping := loadTestMapping(t, "testdata/mapping.yaml")
	mapping.Entries[0].Weaver.From = "wrong.attribute"
	_, err := Convert(diff, mapping)
	if err == nil || !strings.Contains(err.Error(), "do not match the diff") {
		t.Fatalf("error = %v, want unmatched-entry error", err)
	}
}

func TestConvertRejectsUnvalidatedProgrammaticMapping(t *testing.T) {
	t.Parallel()

	diff := loadTestDiff(t, "testdata/diff-v2.json")
	_, err := Convert(diff, Mapping{
		Name: "invalid",
		Entries: []MappingEntry{{
			ID: "missing-resolution",
			Weaver: SourceSelector{
				Kind: SourceKindAttribute,
				Type: "renamed",
				From: "http.method",
				To:   "http.request.method",
			},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "exactly one of prometheus or ignore") {
		t.Fatalf("error = %v, want validation error", err)
	}
}

func TestLoadMigrationHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := LoadMigration(ctx, "testdata/diff-v2.json", "testdata/mapping.yaml")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

func loadTestDiff(t *testing.T, path string) Diff {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	diff, err := ParseDiff(file)
	if err != nil {
		t.Fatal(err)
	}
	return diff
}

func loadTestMapping(t *testing.T, path string) Mapping {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	mapping, err := ParseMapping(file)
	if err != nil {
		t.Fatal(err)
	}
	return mapping
}
