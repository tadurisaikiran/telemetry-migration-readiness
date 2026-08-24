package domain

// EvidenceMethod describes how a dependency was established.
type EvidenceMethod string

const (
	EvidenceMethodPromQLAST          EvidenceMethod = "promql_ast"
	EvidenceMethodStaticConfig       EvidenceMethod = "static_config"
	EvidenceMethodGeneratedRule      EvidenceMethod = "generated_rule"
	EvidenceMethodExplicitMapping    EvidenceMethod = "explicit_mapping"
	EvidenceMethodUsageAPI           EvidenceMethod = "usage_api"
	EvidenceMethodRuntimeQuery       EvidenceMethod = "runtime_query"
	EvidenceMethodTemplateResolution EvidenceMethod = "template_resolution"
	EvidenceMethodAIInference        EvidenceMethod = "ai_inference"
	EvidenceMethodManual             EvidenceMethod = "manual"
)

// Confidence describes the strength of evidence for a reference.
type Confidence string

const (
	ConfidenceConfirmed Confidence = "confirmed"
	ConfidenceHigh      Confidence = "high"
	ConfidenceMedium    Confidence = "medium"
	ConfidenceLow       Confidence = "low"
	ConfidenceUnknown   Confidence = "unknown"
)

// Evidence preserves the provenance behind a reference.
type Evidence struct {
	Method      EvidenceMethod `json:"method"`
	Confidence  Confidence     `json:"confidence"`
	Source      SourceLocation `json:"source"`
	Expression  string         `json:"expression,omitempty"`
	Explanation string         `json:"explanation,omitempty"`
}
