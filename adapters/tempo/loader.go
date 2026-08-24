// Package tempo imports bounded TraceQL consumer manifests and validates each
// query through Tempo's official parser without linking Tempo into TMR.
package tempo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	tmrtraceql "github.com/tadurisaikiran/telemetry-migration-readiness/pkg/traceql"
	"gopkg.in/yaml.v3"
)

const (
	QueryManifestAPIVersion = "tmr.tempo/v1alpha1"
	QueryManifestKind       = "TraceQueries"
	maxManifestBytes        = 8 << 20
	maxQueries              = 10_000
	maxExpressionBytes      = 128 << 10
	maxTextBytes            = 4096
)

var queryIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// AttributeMapping explicitly relates one OpenTelemetry attribute to Tempo.
type AttributeMapping struct {
	Scope         tmrtraceql.Scope
	OpenTelemetry string
	Tempo         string
}

// Loader imports one strict query manifest.
type Loader struct {
	Required           bool
	DefaultCriticality domain.Criticality
	Validator          Validator
	TempoURL           string
	Mappings           []AttributeMapping
}

type queryManifest struct {
	APIVersion string          `yaml:"apiVersion"`
	Kind       string          `yaml:"kind"`
	Queries    []queryDocument `yaml:"queries"`
}

type queryDocument struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Expression  string `yaml:"expression"`
	Criticality string `yaml:"criticality"`
}

// LoadFile reads, validates, and normalizes one local TraceQL manifest.
func (loader Loader) LoadFile(ctx context.Context, path string) (domain.Discovery, error) {
	if err := ctx.Err(); err != nil {
		return domain.Discovery{}, fmt.Errorf("load Tempo queries %q: %w", path, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("open Tempo queries %q: %w", path, err)
	}
	defer file.Close()
	discovery, err := loader.Parse(ctx, filepath.Clean(path), file)
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("load Tempo queries %q: %w", path, err)
	}
	return discovery, nil
}

// Parse strictly decodes a manifest and validates every expression through
// Tempo before extracting scoped attributes.
func (loader Loader) Parse(ctx context.Context, source string, reader io.Reader) (domain.Discovery, error) {
	if loader.Validator == nil {
		return domain.Discovery{}, fmt.Errorf("Tempo TraceQL validator is required")
	}
	contents, err := io.ReadAll(io.LimitReader(reader, maxManifestBytes+1))
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("read Tempo query manifest: %w", err)
	}
	if len(contents) > maxManifestBytes {
		return domain.Discovery{}, fmt.Errorf("Tempo query manifest exceeds the %d-byte size limit", maxManifestBytes)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	var document queryManifest
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return domain.Discovery{}, fmt.Errorf("Tempo query manifest is empty")
		}
		return domain.Discovery{}, fmt.Errorf("decode Tempo query manifest: %w", err)
	}
	var additional any
	if err := decoder.Decode(&additional); err == nil {
		return domain.Discovery{}, fmt.Errorf("Tempo query manifest must contain exactly one YAML document")
	} else if !errors.Is(err, io.EOF) {
		return domain.Discovery{}, fmt.Errorf("decode trailing Tempo query document: %w", err)
	}
	if err := validateManifest(document); err != nil {
		return domain.Discovery{}, err
	}

	defaultCriticality := loader.DefaultCriticality
	if defaultCriticality == "" {
		defaultCriticality = domain.CriticalityHigh
	}
	var discovery domain.Discovery
	for _, query := range document.Queries {
		if err := ctx.Err(); err != nil {
			return domain.Discovery{}, err
		}
		criticality := defaultCriticality
		if query.Criticality != "" {
			criticality = domain.Criticality(query.Criticality)
		}
		consumer := domain.Consumer{
			ID:          queryConsumerID(source, query.ID),
			Kind:        domain.ConsumerKindQuery,
			Name:        query.Name,
			Source:      domain.SourceLocation{File: source, URL: strings.TrimRight(loader.TempoURL, "/")},
			Criticality: criticality,
			Expression:  query.Expression,
			Metadata: map[string]string{
				"query_id":   query.ID,
				"query_kind": "traceql",
			},
		}
		if err := loader.Validator.Validate(ctx, query.Expression); err != nil {
			consumer.Unresolved = true
			discovery.Consumers = append(discovery.Consumers, consumer)
			discovery.Diagnostics = append(discovery.Diagnostics, loader.diagnostic(consumer.Source, fmt.Sprintf("query %q: %v", query.ID, err)))
			continue
		}
		analysis := tmrtraceql.Analyze(query.Expression)
		if len(analysis.Unresolved) != 0 {
			consumer.Unresolved = true
			for _, reason := range analysis.Unresolved {
				discovery.Diagnostics = append(discovery.Diagnostics, loader.diagnostic(consumer.Source, fmt.Sprintf("query %q: %s", query.ID, reason)))
			}
		}
		for _, reference := range analysis.References {
			loader.appendReference(consumer, reference, &discovery)
		}
		discovery.Consumers = append(discovery.Consumers, consumer)
	}
	return discovery, nil
}

func validateManifest(document queryManifest) error {
	if document.APIVersion != QueryManifestAPIVersion {
		return fmt.Errorf("Tempo query apiVersion must be %q", QueryManifestAPIVersion)
	}
	if document.Kind != QueryManifestKind {
		return fmt.Errorf("Tempo query kind must be %q", QueryManifestKind)
	}
	if len(document.Queries) == 0 {
		return fmt.Errorf("Tempo query manifest must contain at least one query")
	}
	if len(document.Queries) > maxQueries {
		return fmt.Errorf("Tempo query manifest exceeds the %d-query limit", maxQueries)
	}
	seen := make(map[string]struct{}, len(document.Queries))
	for index, query := range document.Queries {
		path := fmt.Sprintf("queries[%d]", index)
		if !queryIDPattern.MatchString(query.ID) || len(query.ID) > 128 {
			return fmt.Errorf("%s.id must be a bounded identifier containing letters, numbers, dot, dash, or underscore", path)
		}
		if _, exists := seen[query.ID]; exists {
			return fmt.Errorf("%s.id duplicates an earlier query", path)
		}
		seen[query.ID] = struct{}{}
		if strings.TrimSpace(query.Name) == "" || len(query.Name) > maxTextBytes {
			return fmt.Errorf("%s.name is required and must be no greater than %d bytes", path, maxTextBytes)
		}
		if strings.TrimSpace(query.Expression) == "" || len(query.Expression) > maxExpressionBytes {
			return fmt.Errorf("%s.expression is required and must be no greater than %d bytes", path, maxExpressionBytes)
		}
		if query.Criticality != "" && !validCriticality(query.Criticality) {
			return fmt.Errorf("%s.criticality must be low, medium, high, or critical", path)
		}
	}
	return nil
}

func (loader Loader) appendReference(consumer domain.Consumer, reference tmrtraceql.Reference, discovery *domain.Discovery) {
	kind := domain.SymbolKindSpanAttribute
	if reference.Scope == tmrtraceql.ScopeResource {
		kind = domain.SymbolKindResourceAttribute
	}
	evidence := domain.Evidence{
		Method:      domain.EvidenceMethodTempoValidated,
		Confidence:  domain.ConfidenceConfirmed,
		Source:      consumer.Source,
		Expression:  consumer.Expression,
		Explanation: "scoped attribute extracted after Tempo accepted the TraceQL expression",
	}
	discovery.References = append(discovery.References, domain.Reference{
		ConsumerID: consumer.ID,
		Symbol: domain.Symbol{
			Domain: domain.DomainTempo,
			Kind:   kind,
			Name:   reference.Name,
		},
		Evidence: evidence,
		Usage:    domain.UsageFilter,
	})
	for _, mapping := range loader.Mappings {
		if mapping.Scope != reference.Scope || mapping.Tempo != reference.Name {
			continue
		}
		mappedEvidence := evidence
		mappedEvidence.Method = domain.EvidenceMethodExplicitMapping
		mappedEvidence.Explanation = fmt.Sprintf(
			"explicit %s mapping from Tempo attribute %q to OpenTelemetry attribute %q",
			reference.Scope,
			mapping.Tempo,
			mapping.OpenTelemetry,
		)
		discovery.References = append(discovery.References, domain.Reference{
			ConsumerID: consumer.ID,
			Symbol: domain.Symbol{
				Domain: domain.DomainOpenTelemetry,
				Kind:   kind,
				Name:   mapping.OpenTelemetry,
			},
			Evidence: mappedEvidence,
			Usage:    domain.UsageFilter,
		})
		break
	}
}

func (loader Loader) diagnostic(source domain.SourceLocation, message string) domain.Diagnostic {
	return domain.Diagnostic{Adapter: "tempo", Source: source, Message: message, Required: loader.Required}
}

func queryConsumerID(source, id string) string {
	digest := sha256.Sum256([]byte(source + "\x00" + id))
	return "tempo_query:" + hex.EncodeToString(digest[:16])
}

func validCriticality(value string) bool {
	switch value {
	case "low", "medium", "high", "critical":
		return true
	default:
		return false
	}
}
