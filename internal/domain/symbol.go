// Package domain contains the canonical telemetry migration model.
package domain

// Domain identifies the telemetry system in which a symbol exists.
// Domains are intentionally explicit: names from different telemetry systems
// must not be treated as equivalent without a mapping.
type Domain string

const (
	// DomainPrometheus identifies Prometheus metric and label symbols.
	DomainPrometheus Domain = "prometheus"
	// DomainOpenTelemetry identifies attributes in the OpenTelemetry data model.
	DomainOpenTelemetry Domain = "opentelemetry"
	// DomainTempo identifies attributes as indexed and queried by Tempo.
	DomainTempo Domain = "tempo"
)

// SymbolKind identifies the kind of telemetry contract element.
type SymbolKind string

const (
	// SymbolKindMetric identifies a Prometheus metric.
	SymbolKindMetric SymbolKind = "metric"
	// SymbolKindLabel identifies a label attached to a Prometheus metric.
	SymbolKindLabel SymbolKind = "label"
	// SymbolKindSpanAttribute identifies an attribute attached to a span.
	SymbolKindSpanAttribute SymbolKind = "span_attribute"
	// SymbolKindResourceAttribute identifies an attribute attached to a span's resource.
	SymbolKindResourceAttribute SymbolKind = "resource_attribute"
)

// Symbol is a telemetry contract element affected by a migration.
// Parent is required for labels and contains the parent metric name. It is
// empty for metrics and trace attributes.
type Symbol struct {
	Domain Domain     `json:"domain"`
	Kind   SymbolKind `json:"kind"`
	Name   string     `json:"name"`
	Parent string     `json:"parent,omitempty"`
}
