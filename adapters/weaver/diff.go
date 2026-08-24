// Package weaver imports structured OpenTelemetry Weaver registry diffs.
// It never infers how OpenTelemetry identifiers map to backend symbols.
package weaver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const maxDiffBytes = 4 << 20

// DiffFormat identifies the supported Weaver registry diff envelope.
type DiffFormat string

const (
	// DiffFormatV1 is Weaver's manifest-and-changes diff representation.
	DiffFormatV1 DiffFormat = "v1"
	// DiffFormatV2 is Weaver's schema-URL-and-registry diff representation.
	DiffFormatV2 DiffFormat = "v2"
)

// SourceKind identifies the Weaver registry item being changed.
type SourceKind string

const (
	// SourceKindAttribute identifies an OpenTelemetry registry attribute.
	SourceKindAttribute SourceKind = "attribute"
	// SourceKindAttributeGroup identifies an OpenTelemetry attribute group.
	SourceKindAttributeGroup SourceKind = "attribute_group"
	// SourceKindEntity identifies an OpenTelemetry entity.
	SourceKindEntity SourceKind = "entity"
	// SourceKindEvent identifies an OpenTelemetry event.
	SourceKindEvent SourceKind = "event"
	// SourceKindMetric identifies an OpenTelemetry registry metric.
	SourceKindMetric SourceKind = "metric"
	// SourceKindSpan identifies an OpenTelemetry span.
	SourceKindSpan SourceKind = "span"
)

// SourceChange is one actionable top-level change from Weaver.
type SourceChange struct {
	Kind   SourceKind `json:"kind"`
	Type   string     `json:"type"`
	From   string     `json:"from"`
	To     string     `json:"to,omitempty"`
	Note   string     `json:"note,omitempty"`
	Index  int        `json:"index"`
	Format DiffFormat `json:"format"`
}

// UnsupportedChangeError reports a valid Weaver change whose source data is
// insufficient for deterministic backend mapping.
type UnsupportedChangeError struct {
	Change SourceChange
	Reason string
}

// Error implements error.
func (e *UnsupportedChangeError) Error() string {
	return fmt.Sprintf(
		"Weaver import is incomplete: %s %s change at index %d cannot be mapped: %s",
		e.Change.Kind, e.Change.Type, e.Change.Index, e.Reason,
	)
}

// Diff is the normalized actionable subset of a Weaver diff. Additions are not
// included because they cannot leave a legacy consumer behind. Unsupported
// signal kinds remain present and therefore require an explicit ignore decision.
type Diff struct {
	Format   DiffFormat     `json:"format"`
	Baseline string         `json:"baseline"`
	Head     string         `json:"head"`
	Changes  []SourceChange `json:"changes"`
}

type formatProbe struct {
	Head     json.RawMessage `json:"head"`
	Changes  json.RawMessage `json:"changes"`
	Registry json.RawMessage `json:"registry"`
}

type v1Document struct {
	Head     v1Manifest `json:"head"`
	Baseline v1Manifest `json:"baseline"`
	Changes  v1Changes  `json:"changes"`
}

type v1Manifest struct {
	SemconvVersion string `json:"semconv_version"`
}

type v1Changes struct {
	RegistryAttributes []itemChange `json:"registry_attributes"`
	Metrics            []itemChange `json:"metrics"`
	Events             []itemChange `json:"events"`
	Spans              []itemChange `json:"spans"`
	Entities           []itemChange `json:"entities"`
}

type v2Document struct {
	HeadSchemaURL     schemaURL         `json:"head_schema_url"`
	BaselineSchemaURL schemaURL         `json:"baseline_schema_url"`
	Registry          v2RegistryChanges `json:"registry"`
}

type schemaURL struct {
	URL string `json:"url"`
}

type v2RegistryChanges struct {
	AttributeChanges      []itemChange `json:"attribute_changes"`
	AttributeGroupChanges []itemChange `json:"attribute_group_changes"`
	EntityChanges         []itemChange `json:"entity_changes"`
	EventChanges          []itemChange `json:"event_changes"`
	MetricChanges         []itemChange `json:"metric_changes"`
	SpanChanges           []itemChange `json:"span_changes"`
}

type itemChange struct {
	Type    string  `json:"type"`
	Name    *string `json:"name,omitempty"`
	OldName *string `json:"old_name,omitempty"`
	NewName *string `json:"new_name,omitempty"`
	Note    *string `json:"note,omitempty"`
}

// ParseDiff strictly decodes a Weaver V1 or V2 registry diff. Unknown
// top-level or change fields are rejected so a new upstream schema cannot be
// silently interpreted as the current one.
func ParseDiff(reader io.Reader) (Diff, error) {
	contents, err := readBounded(reader, maxDiffBytes, "Weaver diff")
	if err != nil {
		return Diff{}, err
	}

	var probe formatProbe
	if err := json.Unmarshal(contents, &probe); err != nil {
		return Diff{}, fmt.Errorf("decode Weaver diff: %w", err)
	}

	hasV1 := len(probe.Head) != 0 || len(probe.Changes) != 0
	hasV2 := len(probe.Registry) != 0
	if hasV1 == hasV2 {
		return Diff{}, errors.New("decode Weaver diff: document must match exactly one supported V1 or V2 envelope")
	}

	if hasV1 {
		var document v1Document
		if err := decodeStrictJSON(contents, &document); err != nil {
			return Diff{}, fmt.Errorf("decode Weaver V1 diff: %w", err)
		}
		top, err := requireObjectFields(contents, "Weaver V1 diff", "head", "baseline", "changes")
		if err != nil {
			return Diff{}, err
		}
		if _, err := requireObjectFields(top["changes"], "Weaver V1 changes",
			"registry_attributes", "metrics", "events", "spans", "entities"); err != nil {
			return Diff{}, err
		}
		if strings.TrimSpace(document.Head.SemconvVersion) == "" ||
			strings.TrimSpace(document.Baseline.SemconvVersion) == "" {
			return Diff{}, errors.New("decode Weaver V1 diff: head and baseline semconv_version are required")
		}
		changes, err := normalizeChanges(DiffFormatV1,
			changeGroup{kind: SourceKindAttribute, items: document.Changes.RegistryAttributes},
			changeGroup{kind: SourceKindEntity, items: document.Changes.Entities},
			changeGroup{kind: SourceKindEvent, items: document.Changes.Events},
			changeGroup{kind: SourceKindMetric, items: document.Changes.Metrics},
			changeGroup{kind: SourceKindSpan, items: document.Changes.Spans},
		)
		if err != nil {
			return Diff{}, err
		}
		return Diff{
			Format:   DiffFormatV1,
			Baseline: document.Baseline.SemconvVersion,
			Head:     document.Head.SemconvVersion,
			Changes:  changes,
		}, nil
	}

	var document v2Document
	if err := decodeStrictJSON(contents, &document); err != nil {
		return Diff{}, fmt.Errorf("decode Weaver V2 diff: %w", err)
	}
	top, err := requireObjectFields(contents, "Weaver V2 diff", "head_schema_url", "baseline_schema_url", "registry")
	if err != nil {
		return Diff{}, err
	}
	if _, err := requireObjectFields(top["registry"], "Weaver V2 registry",
		"attribute_changes", "attribute_group_changes", "entity_changes",
		"event_changes", "metric_changes", "span_changes"); err != nil {
		return Diff{}, err
	}
	if strings.TrimSpace(document.HeadSchemaURL.URL) == "" ||
		strings.TrimSpace(document.BaselineSchemaURL.URL) == "" {
		return Diff{}, errors.New("decode Weaver V2 diff: head_schema_url.url and baseline_schema_url.url are required")
	}
	changes, err := normalizeChanges(DiffFormatV2,
		changeGroup{kind: SourceKindAttribute, items: document.Registry.AttributeChanges},
		changeGroup{kind: SourceKindAttributeGroup, items: document.Registry.AttributeGroupChanges},
		changeGroup{kind: SourceKindEntity, items: document.Registry.EntityChanges},
		changeGroup{kind: SourceKindEvent, items: document.Registry.EventChanges},
		changeGroup{kind: SourceKindMetric, items: document.Registry.MetricChanges},
		changeGroup{kind: SourceKindSpan, items: document.Registry.SpanChanges},
	)
	if err != nil {
		return Diff{}, err
	}
	return Diff{
		Format:   DiffFormatV2,
		Baseline: document.BaselineSchemaURL.URL,
		Head:     document.HeadSchemaURL.URL,
		Changes:  changes,
	}, nil
}

type changeGroup struct {
	kind  SourceKind
	items []itemChange
}

func normalizeChanges(format DiffFormat, groups ...changeGroup) ([]SourceChange, error) {
	total := 0
	for _, group := range groups {
		total += len(group.items)
	}
	result := make([]SourceChange, 0, total)
	for _, group := range groups {
		for index, item := range group.items {
			change, include, err := normalizeItem(format, group.kind, index, item)
			if err != nil {
				return nil, err
			}
			if include {
				result = append(result, change)
			}
		}
	}
	return result, nil
}

func normalizeItem(format DiffFormat, kind SourceKind, index int, item itemChange) (SourceChange, bool, error) {
	base := SourceChange{Kind: kind, Type: item.Type, Index: index, Format: format}
	switch item.Type {
	case "added":
		if err := requireFields(kind, index, item.Name, "name"); err != nil {
			return SourceChange{}, false, err
		}
		return SourceChange{}, false, nil
	case "renamed":
		if err := requireFields(kind, index, item.OldName, "old_name"); err != nil {
			return SourceChange{}, false, err
		}
		if err := requireFields(kind, index, item.NewName, "new_name"); err != nil {
			return SourceChange{}, false, err
		}
		if item.Note == nil {
			return SourceChange{}, false, itemFieldError(kind, index, "note", "is required")
		}
		base.From = strings.TrimSpace(*item.OldName)
		base.To = strings.TrimSpace(*item.NewName)
		base.Note = strings.TrimSpace(*item.Note)
	case "removed", "obsoleted", "uncategorized":
		if err := requireFields(kind, index, item.Name, "name"); err != nil {
			return SourceChange{}, false, err
		}
		if (item.Type == "obsoleted" || item.Type == "uncategorized") && item.Note == nil {
			return SourceChange{}, false, itemFieldError(kind, index, "note", "is required")
		}
		base.From = strings.TrimSpace(*item.Name)
		if item.Note != nil {
			base.Note = strings.TrimSpace(*item.Note)
		}
	case "updated":
		return SourceChange{}, false, &UnsupportedChangeError{
			Change: base,
			Reason: "field-level mapping information is unavailable",
		}
	default:
		return SourceChange{}, false, fmt.Errorf(
			"Weaver %s change %s[%d] has unsupported type %q",
			format, kind, index, item.Type,
		)
	}
	return base, true, nil
}

func requireFields(kind SourceKind, index int, value *string, name string) error {
	if value == nil || strings.TrimSpace(*value) == "" {
		return itemFieldError(kind, index, name, "is required")
	}
	return nil
}

func itemFieldError(kind SourceKind, index int, field, message string) error {
	return fmt.Errorf("Weaver %s_changes[%d].%s %s", kind, index, field, message)
}

func decodeStrictJSON(contents []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var additional any
	if err := decoder.Decode(&additional); err == nil {
		return errors.New("document must contain exactly one JSON value")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return nil
}

func readBounded(reader io.Reader, limit int64, description string) ([]byte, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", description, err)
	}
	if int64(len(contents)) > limit {
		return nil, fmt.Errorf("%s exceeds the %d-byte size limit", description, limit)
	}
	if len(bytes.TrimSpace(contents)) == 0 {
		return nil, fmt.Errorf("%s is empty", description)
	}
	return contents, nil
}

func requireObjectFields(contents []byte, description string, fields ...string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(contents, &object); err != nil {
		return nil, fmt.Errorf("decode %s object: %w", description, err)
	}
	if object == nil {
		return nil, fmt.Errorf("%s must be an object", description)
	}
	for _, field := range fields {
		value, exists := object[field]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, fmt.Errorf("%s.%s is required", description, field)
		}
	}
	return object, nil
}
