package domain

// ChangeKind describes a supported telemetry contract change.
type ChangeKind string

const (
	// ChangeKindMetricRename renames a Prometheus metric.
	ChangeKindMetricRename ChangeKind = "metric_rename"
	// ChangeKindMetricRemove removes a Prometheus metric without replacement.
	ChangeKindMetricRemove ChangeKind = "metric_remove"
	// ChangeKindLabelRename renames a label on a Prometheus metric.
	ChangeKindLabelRename ChangeKind = "label_rename"
	// ChangeKindLabelRemove removes a label without replacement.
	ChangeKindLabelRemove ChangeKind = "label_remove"
)

// Change describes one telemetry contract transition. To is nil for removal
// changes and required for rename changes.
type Change struct {
	ID       string            `json:"id"`
	Kind     ChangeKind        `json:"kind"`
	Domain   Domain            `json:"domain"`
	From     Symbol            `json:"from"`
	To       *Symbol           `json:"to,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}
