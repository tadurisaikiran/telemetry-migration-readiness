package domain

// RuntimeEvidence summarizes actual query executions observed in one bounded,
// deterministic evidence window. A nil Consumer.Runtime means the consumer was
// discovered from configuration or another non-runtime source.
type RuntimeEvidence struct {
	Format           string   `json:"format"`
	ExecutionCount   int      `json:"executionCount"`
	FirstSeen        string   `json:"firstSeen"`
	LastSeen         string   `json:"lastSeen"`
	Window           string   `json:"window"`
	WindowStart      string   `json:"windowStart,omitempty"`
	WindowAnchor     string   `json:"windowAnchor"`
	ExecutionsPerDay string   `json:"executionsPerDay,omitempty"`
	Origins          []string `json:"origins"`
	OriginDetails    []string `json:"originDetails,omitempty"`
}
