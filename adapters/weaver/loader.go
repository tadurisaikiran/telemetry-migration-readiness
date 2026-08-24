package weaver

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/config"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
)

// ImportedChange records whether one Weaver change was mapped, explicitly
// ignored, or still requires an explicit backend mapping.
type ImportedChange struct {
	Source          SourceChange   `json:"source"`
	RequiresMapping bool           `json:"requiresMapping"`
	Ignored         bool           `json:"ignored,omitempty"`
	IgnoreReason    string         `json:"ignoreReason,omitempty"`
	Change          *domain.Change `json:"change,omitempty"`
}

// ImportResult is a deterministic, inspectable Weaver conversion result.
type ImportResult struct {
	Format   DiffFormat       `json:"format"`
	Baseline string           `json:"baseline"`
	Head     string           `json:"head"`
	Changes  []ImportedChange `json:"changes"`
}

// MappingRequiredError reports exact source changes that cannot be analyzed
// safely without an explicit backend mapping.
type MappingRequiredError struct {
	Changes []SourceChange
}

// Error implements error.
func (e *MappingRequiredError) Error() string {
	parts := make([]string, 0, len(e.Changes))
	for _, change := range e.Changes {
		name := change.From
		if change.To != "" {
			name += " -> " + change.To
		}
		parts = append(parts, fmt.Sprintf("%s %s %s", change.Kind, change.Type, name))
	}
	return fmt.Sprintf(
		"Weaver import is incomplete: requiresMapping=true for %d change(s): %s",
		len(parts), strings.Join(parts, "; "),
	)
}

// Convert applies exact, explicit mappings to a normalized Weaver diff.
func Convert(diff Diff, mapping Mapping) (ImportResult, error) {
	if len(mapping.Entries) != 0 {
		if err := validateMapping(MappingAPIVersion, MappingKind, mapping); err != nil {
			return ImportResult{}, err
		}
	}
	entries := make(map[string]MappingEntry, len(mapping.Entries))
	for _, entry := range mapping.Entries {
		entries[entry.Weaver.key()] = entry
	}
	matched := make(map[string]struct{}, len(mapping.Entries))
	seenChanges := make(map[string]struct{}, len(diff.Changes))
	result := ImportResult{
		Format:   diff.Format,
		Baseline: diff.Baseline,
		Head:     diff.Head,
		Changes:  make([]ImportedChange, 0, len(diff.Changes)),
	}

	for _, source := range diff.Changes {
		key := source.selector().key()
		if _, exists := seenChanges[key]; exists {
			return ImportResult{}, fmt.Errorf(
				"Weaver diff contains duplicate actionable change %s %s %q",
				source.Kind, source.Type, source.From,
			)
		}
		seenChanges[key] = struct{}{}

		entry, exists := entries[key]
		if !exists {
			result.Changes = append(result.Changes, ImportedChange{
				Source:          source,
				RequiresMapping: true,
			})
			continue
		}
		matched[key] = struct{}{}
		if entry.Ignore != "" {
			result.Changes = append(result.Changes, ImportedChange{
				Source:       source,
				Ignored:      true,
				IgnoreReason: entry.Ignore,
			})
			continue
		}

		change := canonicalChange(entry, source, diff)
		result.Changes = append(result.Changes, ImportedChange{
			Source: source,
			Change: &change,
		})
	}

	var unmatched []string
	for _, entry := range mapping.Entries {
		if _, exists := matched[entry.Weaver.key()]; !exists {
			unmatched = append(unmatched, entry.ID)
		}
	}
	if len(unmatched) != 0 {
		sort.Strings(unmatched)
		return ImportResult{}, fmt.Errorf(
			"Weaver mapping entries do not match the diff: %s",
			strings.Join(unmatched, ", "),
		)
	}
	return result, nil
}

// Migration returns the canonical Prometheus migration. It fails with
// MappingRequiredError if any actionable Weaver change lacks an explicit
// mapping or ignore decision.
func (result ImportResult) Migration(name string) (domain.Migration, error) {
	migration := domain.Migration{
		APIVersion: domain.MigrationAPIVersion,
		Kind:       domain.MigrationKind,
		Metadata:   domain.MigrationMetadata{Name: name},
		Description: fmt.Sprintf(
			"Imported from Weaver %s diff %s to %s.",
			result.Format, result.Baseline, result.Head,
		),
	}
	var missing []SourceChange
	for _, imported := range result.Changes {
		if imported.RequiresMapping {
			missing = append(missing, imported.Source)
		}
		if imported.Change != nil {
			migration.Changes = append(migration.Changes, *imported.Change)
		}
	}
	if len(missing) != 0 {
		return domain.Migration{}, &MappingRequiredError{Changes: missing}
	}
	if len(migration.Changes) == 0 {
		return domain.Migration{}, fmt.Errorf("Weaver import produced no Prometheus changes; every actionable change was ignored")
	}
	if err := config.ValidateMigration(migration); err != nil {
		return domain.Migration{}, fmt.Errorf("validate imported Weaver migration: %w", err)
	}
	return migration, nil
}

// LoadMigration reads a Weaver diff and explicit mapping from local files and
// returns the canonical migration plus the inspectable import result.
func LoadMigration(
	ctx context.Context,
	diffPath string,
	mappingPath string,
) (domain.Migration, ImportResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.Migration{}, ImportResult{}, err
	}
	diffFile, err := os.Open(diffPath)
	if err != nil {
		return domain.Migration{}, ImportResult{}, fmt.Errorf("open Weaver diff %q: %w", diffPath, err)
	}
	diff, parseErr := ParseDiff(diffFile)
	closeErr := diffFile.Close()
	if parseErr != nil {
		return domain.Migration{}, ImportResult{}, fmt.Errorf("load Weaver diff %q: %w", diffPath, parseErr)
	}
	if closeErr != nil {
		return domain.Migration{}, ImportResult{}, fmt.Errorf("close Weaver diff %q: %w", diffPath, closeErr)
	}

	mappingFile, err := os.Open(mappingPath)
	if err != nil {
		return domain.Migration{}, ImportResult{}, fmt.Errorf("open Weaver mapping %q: %w", mappingPath, err)
	}
	mapping, parseErr := ParseMapping(mappingFile)
	closeErr = mappingFile.Close()
	if parseErr != nil {
		return domain.Migration{}, ImportResult{}, fmt.Errorf("load Weaver mapping %q: %w", mappingPath, parseErr)
	}
	if closeErr != nil {
		return domain.Migration{}, ImportResult{}, fmt.Errorf("close Weaver mapping %q: %w", mappingPath, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return domain.Migration{}, ImportResult{}, err
	}

	result, err := Convert(diff, mapping)
	if err != nil {
		return domain.Migration{}, ImportResult{}, err
	}
	migration, err := result.Migration(mapping.Name)
	if err != nil {
		return domain.Migration{}, result, err
	}
	return migration, result, nil
}

func canonicalChange(entry MappingEntry, source SourceChange, diff Diff) domain.Change {
	target := *entry.Prometheus
	change := domain.Change{
		ID:     entry.ID,
		Kind:   target.Kind,
		Domain: domain.DomainPrometheus,
		Metadata: map[string]string{
			"source.adapter":  "weaver",
			"source.format":   string(diff.Format),
			"source.baseline": diff.Baseline,
			"source.head":     diff.Head,
			"source.kind":     string(source.Kind),
			"source.type":     source.Type,
			"source.from":     source.From,
		},
	}
	if source.To != "" {
		change.Metadata["source.to"] = source.To
	}
	if source.Note != "" {
		change.Metadata["source.note"] = source.Note
	}

	if target.Kind == domain.ChangeKindMetricRename || target.Kind == domain.ChangeKindMetricRemove {
		change.From = domain.Symbol{
			Domain: domain.DomainPrometheus,
			Kind:   domain.SymbolKindMetric,
			Name:   target.From.Metric,
		}
		if target.To != nil {
			change.To = &domain.Symbol{
				Domain: domain.DomainPrometheus,
				Kind:   domain.SymbolKindMetric,
				Name:   target.To.Metric,
			}
		}
		return change
	}

	change.From = domain.Symbol{
		Domain: domain.DomainPrometheus,
		Kind:   domain.SymbolKindLabel,
		Name:   target.From.Label,
		Parent: target.Metric,
	}
	if target.To != nil {
		change.To = &domain.Symbol{
			Domain: domain.DomainPrometheus,
			Kind:   domain.SymbolKindLabel,
			Name:   target.To.Label,
			Parent: target.Metric,
		}
	}
	return change
}
