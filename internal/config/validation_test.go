package config

import (
	"testing"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
)

func TestValidateMigrationRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*domain.Migration)
		wantErr string
	}{
		{
			name: "unsupported API version",
			mutate: func(migration *domain.Migration) {
				migration.APIVersion = "telemetry-migration/v2"
			},
			wantErr: "apiVersion: must be",
		},
		{
			name: "unsupported document kind",
			mutate: func(migration *domain.Migration) {
				migration.Kind = "Plan"
			},
			wantErr: `kind: must be "Migration"`,
		},
		{
			name: "missing migration name",
			mutate: func(migration *domain.Migration) {
				migration.Metadata.Name = ""
			},
			wantErr: "metadata.name: is required",
		},
		{
			name: "empty changes",
			mutate: func(migration *domain.Migration) {
				migration.Changes = nil
			},
			wantErr: "spec.changes: must contain at least one change",
		},
		{
			name: "missing change ID",
			mutate: func(migration *domain.Migration) {
				migration.Changes[0].ID = ""
			},
			wantErr: "spec.changes[0].id: is required",
		},
		{
			name: "duplicate change ID",
			mutate: func(migration *domain.Migration) {
				migration.Changes = append(migration.Changes, migration.Changes[0])
			},
			wantErr: "spec.changes[1].id: duplicates spec.changes[0].id",
		},
		{
			name: "unsupported change kind",
			mutate: func(migration *domain.Migration) {
				migration.Changes[0].Kind = "unit_change"
			},
			wantErr: `unsupported change kind "unit_change"`,
		},
		{
			name: "unsupported domain",
			mutate: func(migration *domain.Migration) {
				migration.Changes[0].Domain = "unknown"
			},
			wantErr: `unsupported domain "unknown"`,
		},
		{
			name: "domain does not support change kind",
			mutate: func(migration *domain.Migration) {
				migration.Changes[0].Domain = domain.DomainOpenTelemetry
			},
			wantErr: `does not support change kind "metric_rename"`,
		},
		{
			name: "rename without destination",
			mutate: func(migration *domain.Migration) {
				migration.Changes[0].To = nil
			},
			wantErr: "spec.changes[0].to: is required for a rename",
		},
		{
			name: "removal with destination",
			mutate: func(migration *domain.Migration) {
				migration.Changes[0].Kind = domain.ChangeKindMetricRemove
			},
			wantErr: "spec.changes[0].to: must be omitted for a removal",
		},
		{
			name: "empty source metric",
			mutate: func(migration *domain.Migration) {
				migration.Changes[0].From.Name = ""
			},
			wantErr: "spec.changes[0].from.metric: is required",
		},
		{
			name: "label change without parent metric",
			mutate: func(migration *domain.Migration) {
				migration.Changes[0] = labelRename()
				migration.Changes[0].From.Parent = ""
				migration.Changes[0].To.Parent = ""
			},
			wantErr: "spec.changes[0].metric: parent metric is required",
		},
		{
			name: "same source and destination",
			mutate: func(migration *domain.Migration) {
				migration.Changes[0].To.Name = migration.Changes[0].From.Name
			},
			wantErr: "spec.changes[0].to: must differ from the source",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			migration := validMigration()
			test.mutate(&migration)
			err := ValidateMigration(migration)
			assertErrorContains(t, err, test.wantErr)
		})
	}
}

func TestValidateMigrationSupportsScopedTraceAttributeChanges(t *testing.T) {
	t.Parallel()

	for _, change := range []domain.Change{
		traceAttributeChange(domain.ChangeKindSpanAttributeRename, domain.SymbolKindSpanAttribute),
		traceAttributeChange(domain.ChangeKindResourceAttributeRename, domain.SymbolKindResourceAttribute),
	} {
		migration := validMigration()
		migration.Changes = []domain.Change{change}
		if err := ValidateMigration(migration); err != nil {
			t.Fatalf("ValidateMigration(%s) error = %v", change.Kind, err)
		}
	}
}

func traceAttributeChange(kind domain.ChangeKind, symbolKind domain.SymbolKind) domain.Change {
	destination := domain.Symbol{Domain: domain.DomainOpenTelemetry, Kind: symbolKind, Name: "new.attribute"}
	return domain.Change{
		ID:     "trace-attribute",
		Kind:   kind,
		Domain: domain.DomainOpenTelemetry,
		From:   domain.Symbol{Domain: domain.DomainOpenTelemetry, Kind: symbolKind, Name: "old.attribute"},
		To:     &destination,
	}
}

func validMigration() domain.Migration {
	destination := domain.Symbol{
		Domain: domain.DomainPrometheus,
		Kind:   domain.SymbolKindMetric,
		Name:   "new_metric",
	}

	return domain.Migration{
		APIVersion: domain.MigrationAPIVersion,
		Kind:       domain.MigrationKind,
		Metadata: domain.MigrationMetadata{
			Name: "checkout",
		},
		Changes: []domain.Change{
			{
				ID:     "metric-change",
				Kind:   domain.ChangeKindMetricRename,
				Domain: domain.DomainPrometheus,
				From: domain.Symbol{
					Domain: domain.DomainPrometheus,
					Kind:   domain.SymbolKindMetric,
					Name:   "old_metric",
				},
				To: &destination,
			},
		},
	}
}

func labelRename() domain.Change {
	destination := domain.Symbol{
		Domain: domain.DomainPrometheus,
		Kind:   domain.SymbolKindLabel,
		Name:   "http_request_method",
		Parent: "request_duration_seconds",
	}
	return domain.Change{
		ID:     "label-change",
		Kind:   domain.ChangeKindLabelRename,
		Domain: domain.DomainPrometheus,
		From: domain.Symbol{
			Domain: domain.DomainPrometheus,
			Kind:   domain.SymbolKindLabel,
			Name:   "http_method",
			Parent: "request_duration_seconds",
		},
		To: &destination,
	}
}
