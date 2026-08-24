// Package config loads and validates local TMR configuration documents.
package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	"gopkg.in/yaml.v3"
)

const maxMigrationManifestBytes = 1 << 20

type migrationDocument struct {
	APIVersion string                    `yaml:"apiVersion"`
	Kind       string                    `yaml:"kind"`
	Metadata   migrationMetadataDocument `yaml:"metadata"`
	Spec       migrationSpecDocument     `yaml:"spec"`
}

type migrationMetadataDocument struct {
	Name string `yaml:"name"`
}

type migrationSpecDocument struct {
	Description string           `yaml:"description"`
	Changes     []changeDocument `yaml:"changes"`
}

type changeDocument struct {
	ID     string            `yaml:"id"`
	Kind   domain.ChangeKind `yaml:"kind"`
	Domain domain.Domain     `yaml:"domain"`
	Metric string            `yaml:"metric"`
	From   symbolDocument    `yaml:"from"`
	To     *symbolDocument   `yaml:"to"`
}

type symbolDocument struct {
	Metric    string `yaml:"metric"`
	Label     string `yaml:"label"`
	Attribute string `yaml:"attribute"`
}

// LoadMigration reads and validates one migration manifest from path.
func LoadMigration(ctx context.Context, path string) (domain.Migration, error) {
	if err := ctx.Err(); err != nil {
		return domain.Migration{}, fmt.Errorf("load migration %q: %w", path, err)
	}

	file, err := os.Open(path)
	if err != nil {
		return domain.Migration{}, fmt.Errorf("open migration %q: %w", path, err)
	}
	defer file.Close()

	migration, err := ParseMigration(file)
	if err != nil {
		return domain.Migration{}, fmt.Errorf("load migration %q: %w", path, err)
	}
	if err := ctx.Err(); err != nil {
		return domain.Migration{}, fmt.Errorf("load migration %q: %w", path, err)
	}

	return migration, nil
}

// ParseMigration decodes and validates exactly one migration YAML document.
func ParseMigration(reader io.Reader) (domain.Migration, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maxMigrationManifestBytes+1))
	if err != nil {
		return domain.Migration{}, fmt.Errorf("read migration manifest: %w", err)
	}
	if len(contents) > maxMigrationManifestBytes {
		return domain.Migration{}, fmt.Errorf(
			"migration manifest exceeds the %d-byte size limit",
			maxMigrationManifestBytes,
		)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)

	var document migrationDocument
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return domain.Migration{}, errors.New("migration manifest is empty")
		}
		return domain.Migration{}, fmt.Errorf("decode migration manifest: %w", err)
	}

	var additionalDocument any
	if err := decoder.Decode(&additionalDocument); err == nil {
		return domain.Migration{}, errors.New("migration manifest must contain exactly one YAML document")
	} else if !errors.Is(err, io.EOF) {
		return domain.Migration{}, fmt.Errorf("decode trailing migration document: %w", err)
	}

	migration := document.toDomain()
	issues := &ValidationError{}
	if err := ValidateMigration(migration); err != nil {
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) {
			return domain.Migration{}, err
		}
		issues.append(validationErr)
	}
	issues.append(validateDocumentShape(document))
	if err := issues.errOrNil(); err != nil {
		return domain.Migration{}, err
	}

	return migration, nil
}

func (document migrationDocument) toDomain() domain.Migration {
	changes := make([]domain.Change, 0, len(document.Spec.Changes))
	for _, change := range document.Spec.Changes {
		symbolKind := expectedSymbolKind(change.Kind)
		from := change.From.toDomain(change.Domain, symbolKind, change.Metric)

		var to *domain.Symbol
		if change.To != nil {
			destination := change.To.toDomain(change.Domain, symbolKind, change.Metric)
			to = &destination
		}

		changes = append(changes, domain.Change{
			ID:     change.ID,
			Kind:   change.Kind,
			Domain: change.Domain,
			From:   from,
			To:     to,
		})
	}

	return domain.Migration{
		APIVersion: document.APIVersion,
		Kind:       document.Kind,
		Metadata: domain.MigrationMetadata{
			Name: document.Metadata.Name,
		},
		Description: document.Spec.Description,
		Changes:     changes,
	}
}

func (document symbolDocument) toDomain(
	domainValue domain.Domain,
	kind domain.SymbolKind,
	parentMetric string,
) domain.Symbol {
	name := document.Metric
	parent := ""
	switch kind {
	case domain.SymbolKindLabel:
		name = document.Label
		parent = parentMetric
	case domain.SymbolKindSpanAttribute, domain.SymbolKindResourceAttribute:
		name = document.Attribute
	}

	return domain.Symbol{
		Domain: domainValue,
		Kind:   kind,
		Name:   name,
		Parent: parent,
	}
}

func validateDocumentShape(document migrationDocument) *ValidationError {
	issues := &ValidationError{}
	for index, change := range document.Spec.Changes {
		path := fmt.Sprintf("spec.changes[%d]", index)

		switch change.Kind {
		case domain.ChangeKindMetricRename, domain.ChangeKindMetricRemove:
			if change.Metric != "" {
				issues.add(path+".metric", "is only valid for a label change")
			}
			if change.From.Label != "" {
				issues.add(path+".from.label", "is only valid for a label change")
			}
			if change.To != nil && change.To.Label != "" {
				issues.add(path+".to.label", "is only valid for a label change")
			}
			if change.From.Attribute != "" {
				issues.add(path+".from.attribute", "is only valid for an attribute change")
			}
			if change.To != nil && change.To.Attribute != "" {
				issues.add(path+".to.attribute", "is only valid for an attribute change")
			}

		case domain.ChangeKindLabelRename, domain.ChangeKindLabelRemove:
			if change.From.Metric != "" {
				issues.add(path+".from.metric", "is only valid for a metric change")
			}
			if change.To != nil && change.To.Metric != "" {
				issues.add(path+".to.metric", "is only valid for a metric change")
			}
			if change.From.Attribute != "" {
				issues.add(path+".from.attribute", "is only valid for an attribute change")
			}
			if change.To != nil && change.To.Attribute != "" {
				issues.add(path+".to.attribute", "is only valid for an attribute change")
			}

		case domain.ChangeKindSpanAttributeRename,
			domain.ChangeKindSpanAttributeRemove,
			domain.ChangeKindResourceAttributeRename,
			domain.ChangeKindResourceAttributeRemove:
			if change.Metric != "" {
				issues.add(path+".metric", "is only valid for a label change")
			}
			if change.From.Metric != "" {
				issues.add(path+".from.metric", "is only valid for a metric change")
			}
			if change.From.Label != "" {
				issues.add(path+".from.label", "is only valid for a label change")
			}
			if change.To != nil && change.To.Metric != "" {
				issues.add(path+".to.metric", "is only valid for a metric change")
			}
			if change.To != nil && change.To.Label != "" {
				issues.add(path+".to.label", "is only valid for a label change")
			}
		}
	}

	return issues
}
