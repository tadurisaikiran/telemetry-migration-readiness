package domain

// UsageType describes how a telemetry symbol participates in an expression.
type UsageType string

const (
	UsageSelector       UsageType = "selector"
	UsageFilter         UsageType = "filter"
	UsageGrouping       UsageType = "grouping"
	UsageAggregation    UsageType = "aggregation"
	UsageVectorMatching UsageType = "vector_matching"
	UsageGeneratedName  UsageType = "generated_name"
	UsageTemplate       UsageType = "template"
	UsagePattern        UsageType = "pattern"
	UsageUnknown        UsageType = "unknown"
)

// ResolutionScope limits which change kinds an unresolved reference can
// affect. The empty value is conservative and applies to every change.
type ResolutionScope string

const (
	// ResolutionScopeLabels means the metric is known but label usage is not.
	ResolutionScopeLabels ResolutionScope = "labels"
)

// Reference connects a consumer to a telemetry symbol. Pattern references are
// deliberately unresolved and can add risk, but cannot prove safety.
type Reference struct {
	ConsumerID         string          `json:"consumerId,omitempty"`
	Symbol             Symbol          `json:"symbol"`
	Evidence           Evidence        `json:"evidence"`
	Usage              UsageType       `json:"usage"`
	Pattern            string          `json:"pattern,omitempty"`
	RequiresResolution bool            `json:"requiresResolution,omitempty"`
	ResolutionScope    ResolutionScope `json:"resolutionScope,omitempty"`
}

// Production records the telemetry symbol emitted by a recording rule or
// another derived-telemetry consumer.
type Production struct {
	ConsumerID string `json:"consumerId"`
	Symbol     Symbol `json:"symbol"`
}

// Diagnostic records an adapter or analysis problem. Required diagnostics
// prevent a future READY decision.
type Diagnostic struct {
	Adapter  string         `json:"adapter"`
	Source   SourceLocation `json:"source"`
	Message  string         `json:"message"`
	Required bool           `json:"required"`
}

// Discovery is the normalized output shared by consumer adapters.
type Discovery struct {
	Consumers   []Consumer   `json:"consumers"`
	References  []Reference  `json:"references"`
	Productions []Production `json:"productions,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

// Append merges another adapter discovery result while preserving adapter
// order and evidence provenance.
func (discovery *Discovery) Append(additional Discovery) {
	discovery.Consumers = append(discovery.Consumers, additional.Consumers...)
	discovery.References = append(discovery.References, additional.References...)
	discovery.Productions = append(discovery.Productions, additional.Productions...)
	discovery.Diagnostics = append(discovery.Diagnostics, additional.Diagnostics...)
}
