package domain

// ConsumerKind identifies a downstream telemetry consumer.
type ConsumerKind string

const (
	ConsumerKindDashboard      ConsumerKind = "dashboard"
	ConsumerKindDashboardPanel ConsumerKind = "dashboard_panel"
	ConsumerKindAlertRule      ConsumerKind = "alert_rule"
	ConsumerKindRecordingRule  ConsumerKind = "recording_rule"
	ConsumerKindSLO            ConsumerKind = "slo"
	ConsumerKindCollector      ConsumerKind = "collector_config"
	ConsumerKindQuery          ConsumerKind = "query"
	ConsumerKindSourceCode     ConsumerKind = "source_code"
	ConsumerKindRunbook        ConsumerKind = "runbook"
)

// Criticality controls whether a legacy or unresolved consumer blocks a
// migration. Values are ordered from lowest to highest severity.
type Criticality string

const (
	CriticalityLow      Criticality = "low"
	CriticalityMedium   Criticality = "medium"
	CriticalityHigh     Criticality = "high"
	CriticalityCritical Criticality = "critical"
)

// Consumer is a normalized downstream dependency on telemetry.
type Consumer struct {
	ID          string            `json:"id"`
	Kind        ConsumerKind      `json:"kind"`
	Name        string            `json:"name"`
	Source      SourceLocation    `json:"source"`
	Criticality Criticality       `json:"criticality"`
	Owner       *Owner            `json:"owner,omitempty"`
	Runtime     *RuntimeEvidence  `json:"runtime,omitempty"`
	Expression  string            `json:"expression,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Unresolved  bool              `json:"unresolved,omitempty"`
}
