package weaver

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	"gopkg.in/yaml.v3"
)

const (
	// MappingAPIVersion is the schema version for explicit Weaver backend mappings.
	MappingAPIVersion = "tmr.weaver/v1alpha1"
	// MappingKind is the required mapping document kind.
	MappingKind     = "WeaverMapping"
	maxMappingBytes = 1 << 20
)

// Mapping binds exact Weaver changes to explicit Prometheus changes or an
// explicit, documented ignore decision.
type Mapping struct {
	Name    string
	Entries []MappingEntry
}

// MappingEntry resolves one exact Weaver source change.
type MappingEntry struct {
	ID         string
	Weaver     SourceSelector
	Prometheus *PrometheusChange
	Ignore     string
}

// SourceSelector identifies one exact normalized Weaver change.
type SourceSelector struct {
	Kind SourceKind
	Type string
	From string
	To   string
}

// PrometheusChange uses the same supported shape as a canonical migration
// manifest change.
type PrometheusChange struct {
	Kind   domain.ChangeKind
	Metric string
	From   MappingSymbol
	To     *MappingSymbol
}

// MappingSymbol contains either a metric or label name.
type MappingSymbol struct {
	Metric string
	Label  string
}

type mappingDocument struct {
	APIVersion string                  `yaml:"apiVersion"`
	Kind       string                  `yaml:"kind"`
	Metadata   mappingMetadataDocument `yaml:"metadata"`
	Spec       mappingSpecDocument     `yaml:"spec"`
}

type mappingMetadataDocument struct {
	Name string `yaml:"name"`
}

type mappingSpecDocument struct {
	Mappings []mappingEntryDocument `yaml:"mappings"`
}

type mappingEntryDocument struct {
	ID         string                    `yaml:"id"`
	Weaver     sourceSelectorDocument    `yaml:"weaver"`
	Prometheus *prometheusChangeDocument `yaml:"prometheus"`
	Ignore     string                    `yaml:"ignore"`
}

type sourceSelectorDocument struct {
	Kind SourceKind `yaml:"kind"`
	Type string     `yaml:"type"`
	From string     `yaml:"from"`
	To   string     `yaml:"to"`
}

type prometheusChangeDocument struct {
	Kind   domain.ChangeKind `yaml:"kind"`
	Metric string            `yaml:"metric"`
	From   mappingSymbolDoc  `yaml:"from"`
	To     *mappingSymbolDoc `yaml:"to"`
}

type mappingSymbolDoc struct {
	Metric string `yaml:"metric"`
	Label  string `yaml:"label"`
}

// ParseMapping strictly decodes and validates an explicit Weaver-to-
// Prometheus mapping document.
func ParseMapping(reader io.Reader) (Mapping, error) {
	contents, err := readBounded(reader, maxMappingBytes, "Weaver mapping")
	if err != nil {
		return Mapping{}, err
	}

	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	var document mappingDocument
	if err := decoder.Decode(&document); err != nil {
		return Mapping{}, fmt.Errorf("decode Weaver mapping: %w", err)
	}
	var additional any
	if err := decoder.Decode(&additional); err == nil {
		return Mapping{}, errors.New("Weaver mapping must contain exactly one YAML document")
	} else if !errors.Is(err, io.EOF) {
		return Mapping{}, fmt.Errorf("decode trailing Weaver mapping document: %w", err)
	}

	mapping := Mapping{Name: strings.TrimSpace(document.Metadata.Name)}
	mapping.Entries = make([]MappingEntry, 0, len(document.Spec.Mappings))
	for _, entry := range document.Spec.Mappings {
		converted := MappingEntry{
			ID: strings.TrimSpace(entry.ID),
			Weaver: SourceSelector{
				Kind: entry.Weaver.Kind,
				Type: strings.TrimSpace(entry.Weaver.Type),
				From: strings.TrimSpace(entry.Weaver.From),
				To:   strings.TrimSpace(entry.Weaver.To),
			},
			Ignore: strings.TrimSpace(entry.Ignore),
		}
		if entry.Prometheus != nil {
			converted.Prometheus = &PrometheusChange{
				Kind:   entry.Prometheus.Kind,
				Metric: strings.TrimSpace(entry.Prometheus.Metric),
				From: MappingSymbol{
					Metric: strings.TrimSpace(entry.Prometheus.From.Metric),
					Label:  strings.TrimSpace(entry.Prometheus.From.Label),
				},
			}
			if entry.Prometheus.To != nil {
				converted.Prometheus.To = &MappingSymbol{
					Metric: strings.TrimSpace(entry.Prometheus.To.Metric),
					Label:  strings.TrimSpace(entry.Prometheus.To.Label),
				}
			}
		}
		mapping.Entries = append(mapping.Entries, converted)
	}

	if err := validateMapping(document.APIVersion, document.Kind, mapping); err != nil {
		return Mapping{}, err
	}
	return mapping, nil
}

func validateMapping(apiVersion, kind string, mapping Mapping) error {
	var issues []string
	add := func(path, message string) {
		issues = append(issues, path+": "+message)
	}
	if apiVersion != MappingAPIVersion {
		add("apiVersion", fmt.Sprintf("must be %q", MappingAPIVersion))
	}
	if kind != MappingKind {
		add("kind", fmt.Sprintf("must be %q", MappingKind))
	}
	if mapping.Name == "" {
		add("metadata.name", "is required")
	}
	if len(mapping.Entries) == 0 {
		add("spec.mappings", "must contain at least one mapping")
	}

	seenIDs := map[string]int{}
	seenSelectors := map[string]int{}
	for index, entry := range mapping.Entries {
		path := fmt.Sprintf("spec.mappings[%d]", index)
		if entry.ID == "" {
			add(path+".id", "is required")
		} else if previous, exists := seenIDs[entry.ID]; exists {
			add(path+".id", fmt.Sprintf("duplicates spec.mappings[%d].id", previous))
		} else {
			seenIDs[entry.ID] = index
		}
		switch entry.Weaver.Kind {
		case SourceKindAttribute,
			SourceKindAttributeGroup,
			SourceKindEntity,
			SourceKindEvent,
			SourceKindMetric,
			SourceKindSpan:
		default:
			add(path+".weaver.kind", "must be attribute, attribute_group, entity, event, metric, or span")
		}
		switch entry.Weaver.Type {
		case "renamed", "removed", "obsoleted", "uncategorized":
		default:
			add(path+".weaver.type", "must be renamed, removed, obsoleted, or uncategorized")
		}
		if entry.Weaver.From == "" {
			add(path+".weaver.from", "is required")
		}
		if entry.Weaver.Type == "renamed" {
			if entry.Weaver.To == "" {
				add(path+".weaver.to", "is required for a rename")
			}
		} else if entry.Weaver.To != "" {
			add(path+".weaver.to", "must be omitted unless type is renamed")
		}

		selectorKey := entry.Weaver.key()
		if previous, exists := seenSelectors[selectorKey]; exists {
			add(path+".weaver", fmt.Sprintf("duplicates spec.mappings[%d].weaver", previous))
		} else {
			seenSelectors[selectorKey] = index
		}

		hasTarget := entry.Prometheus != nil
		hasIgnore := entry.Ignore != ""
		if hasTarget == hasIgnore {
			add(path, "must set exactly one of prometheus or ignore")
		}
		if entry.Weaver.Type == "uncategorized" && hasTarget {
			add(path+".prometheus", "uncategorized changes may only be explicitly ignored")
		}
		if hasTarget {
			validatePrometheusTarget(add, path+".prometheus", entry.Weaver, *entry.Prometheus)
		}
	}
	if len(issues) != 0 {
		return fmt.Errorf("Weaver mapping is invalid:\n  - %s", strings.Join(issues, "\n  - "))
	}
	return nil
}

func validatePrometheusTarget(
	add func(string, string),
	path string,
	source SourceSelector,
	target PrometheusChange,
) {
	isRename := source.Type == "renamed"
	if source.Kind != SourceKindAttribute && source.Kind != SourceKindMetric {
		add(path, fmt.Sprintf("a %s source may only be explicitly ignored", source.Kind))
	}
	switch target.Kind {
	case domain.ChangeKindMetricRename, domain.ChangeKindMetricRemove:
		if source.Kind != SourceKindMetric {
			add(path+".kind", "an attribute source must map to a label change")
		}
		if target.From.Metric == "" {
			add(path+".from.metric", "is required")
		}
		if target.From.Label != "" {
			add(path+".from.label", "is only valid for a label change")
		}
		if target.Metric != "" {
			add(path+".metric", "is only valid for a label change")
		}
	case domain.ChangeKindLabelRename, domain.ChangeKindLabelRemove:
		if source.Kind != SourceKindAttribute {
			add(path+".kind", "a metric source must map to a metric change")
		}
		if target.Metric == "" {
			add(path+".metric", "parent metric is required for a label change")
		}
		if target.From.Label == "" {
			add(path+".from.label", "is required")
		}
		if target.From.Metric != "" {
			add(path+".from.metric", "is only valid for a metric change")
		}
	default:
		add(path+".kind", "must be metric_rename, metric_remove, label_rename, or label_remove")
	}

	targetRename := target.Kind == domain.ChangeKindMetricRename || target.Kind == domain.ChangeKindLabelRename
	if targetRename != isRename {
		add(path+".kind", "must preserve the Weaver rename or removal action")
	}
	if targetRename {
		if target.To == nil {
			add(path+".to", "is required for a rename")
			return
		}
		if target.Kind == domain.ChangeKindMetricRename {
			if target.To.Metric == "" {
				add(path+".to.metric", "is required")
			}
			if target.To.Label != "" {
				add(path+".to.label", "is only valid for a label change")
			}
		} else {
			if target.To.Label == "" {
				add(path+".to.label", "is required")
			}
			if target.To.Metric != "" {
				add(path+".to.metric", "is only valid for a metric change")
			}
		}
	} else if target.To != nil {
		add(path+".to", "must be omitted for a removal")
	}
}

func (selector SourceSelector) key() string {
	return strings.Join([]string{string(selector.Kind), selector.Type, selector.From, selector.To}, "\x00")
}

func (change SourceChange) selector() SourceSelector {
	return SourceSelector{Kind: change.Kind, Type: change.Type, From: change.From, To: change.To}
}
