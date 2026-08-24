package persesusage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	tmrpromql "github.com/tadurisaikiran/telemetry-migration-readiness/pkg/promql"
)

type evidenceOrigin string

const (
	originMetrics evidenceOrigin = "metrics"
	originPartial evidenceOrigin = "partial_metrics"
	originPending evidenceOrigin = "pending_usages"
)

type consumerState struct {
	consumer   domain.Consumer
	references map[string]domain.Reference
	origins    map[string]struct{}
	production *domain.Production
}

type discoveryBuilder struct {
	source      string
	required    bool
	sourceID    string
	consumers   map[string]*consumerState
	diagnostics []domain.Diagnostic
}

func newDiscoveryBuilder(source string, required bool) *discoveryBuilder {
	digest := sha256.Sum256([]byte(source))
	return &discoveryBuilder{
		source:    source,
		required:  required,
		sourceID:  hex.EncodeToString(digest[:8]),
		consumers: make(map[string]*consumerState),
	}
}

func (builder *discoveryBuilder) addMetrics(metrics map[string]*metricDocument) {
	for _, metric := range sortedKeys(metrics) {
		document := metrics[metric]
		if strings.TrimSpace(metric) == "" {
			builder.addDiagnostic(originMetrics, "response contains an empty metric name")
			continue
		}
		if document == nil {
			builder.addDiagnostic(originMetrics, fmt.Sprintf("metric %q has a null document", metric))
			continue
		}
		if document.Usage != nil {
			builder.addUsage(metric, document.Usage, originMetrics, false)
		}
	}
}

func (builder *discoveryBuilder) addPartialMetrics(metrics map[string]*partialMetricDocument) {
	for _, pattern := range sortedKeys(metrics) {
		document := metrics[pattern]
		if strings.TrimSpace(pattern) == "" {
			builder.addDiagnostic(originPartial, "response contains an empty partial metric pattern")
			continue
		}
		if document == nil {
			builder.addDiagnostic(originPartial, fmt.Sprintf("partial metric %q has a null document", pattern))
			continue
		}
		if document.Usage != nil {
			builder.addUsage(pattern, document.Usage, originPartial, true)
			seen := make(map[string]struct{}, len(document.MatchingMetrics))
			for _, metric := range document.MatchingMetrics {
				metric = strings.TrimSpace(metric)
				if metric == "" {
					builder.addDiagnostic(originPartial, fmt.Sprintf("partial metric %q contains an empty matching metric", pattern))
					continue
				}
				seen[metric] = struct{}{}
			}
			for _, metric := range sortedKeys(seen) {
				builder.addUsage(metric, document.Usage, originPartial, false)
			}
		}
	}
}

func (builder *discoveryBuilder) addPendingUsage(metrics map[string]*usageDocument) {
	for _, metric := range sortedKeys(metrics) {
		usage := metrics[metric]
		if strings.TrimSpace(metric) == "" {
			builder.addDiagnostic(originPending, "response contains an empty pending metric name")
			continue
		}
		if usage == nil {
			builder.addDiagnostic(originPending, fmt.Sprintf("pending metric %q has null usage", metric))
			continue
		}
		builder.addUsage(metric, usage, originPending, false)
	}
}

func (builder *discoveryBuilder) addUsage(metric string, usage *usageDocument, origin evidenceOrigin, partial bool) {
	for index, dashboard := range usage.Dashboards {
		identity := firstNonBlank(dashboard.id(), dashboard.URL, dashboard.name())
		name := firstNonBlank(dashboard.name(), dashboard.id(), dashboard.URL)
		if identity == "" || name == "" {
			builder.addDiagnostic(origin, fmt.Sprintf("dashboard usage %d for %q has no identity", index, metric))
			continue
		}
		consumerID := fmt.Sprintf("perses_usage:%s:dashboard:%s", builder.sourceID, identity)
		sourceURL := firstNonBlank(dashboard.URL, builder.source)
		state := builder.state(domain.Consumer{
			ID:          consumerID,
			Kind:        domain.ConsumerKindDashboard,
			Name:        name,
			Source:      domain.SourceLocation{URL: sourceURL},
			Criticality: domain.CriticalityMedium,
			Metadata: map[string]string{
				"adapter":       "perses_metrics_usage",
				"dashboard_uid": dashboard.id(),
				"dashboard_url": dashboard.URL,
				"usage_api":     builder.source,
			},
		})
		builder.addUsageReference(state, metric, origin, partial)
		if !partial {
			builder.addLabelUncertainty(state, metric, origin)
		}
	}

	for index, rule := range usage.RecordingRules {
		builder.addRule(metric, rule, origin, partial, domain.ConsumerKindRecordingRule, index)
	}
	for index, rule := range usage.AlertRules {
		builder.addRule(metric, rule, origin, partial, domain.ConsumerKindAlertRule, index)
	}
}

func (builder *discoveryBuilder) addRule(
	metric string,
	rule ruleUsage,
	origin evidenceOrigin,
	partial bool,
	kind domain.ConsumerKind,
	index int,
) {
	name := strings.TrimSpace(rule.Name)
	if name == "" {
		builder.addDiagnostic(origin, fmt.Sprintf("%s usage %d for %q has no name", kind, index, metric))
		return
	}
	consumerID := fmt.Sprintf(
		"perses_usage:%s:%s:%s:%s:%s",
		builder.sourceID, kind, rule.PromLink, rule.GroupName, name,
	)
	criticality := domain.CriticalityMedium
	if kind == domain.ConsumerKindAlertRule {
		criticality = domain.CriticalityHigh
	}
	state := builder.state(domain.Consumer{
		ID:          consumerID,
		Kind:        kind,
		Name:        name,
		Source:      domain.SourceLocation{URL: firstNonBlank(rule.PromLink, builder.source)},
		Criticality: criticality,
		Expression:  strings.TrimSpace(rule.Expression),
		Metadata: map[string]string{
			"adapter":    "perses_metrics_usage",
			"group_name": strings.TrimSpace(rule.GroupName),
			"prom_link":  strings.TrimSpace(rule.PromLink),
			"usage_api":  builder.source,
		},
	})
	if state.consumer.Expression != strings.TrimSpace(rule.Expression) {
		first := state.consumer.Expression
		second := strings.TrimSpace(rule.Expression)
		if second < first {
			state.consumer.Expression = second
		}
		state.consumer.Unresolved = true
		builder.addDiagnostic(origin, fmt.Sprintf("%s %q has conflicting expressions", kind, name))
	}
	builder.addUsageReference(state, metric, origin, partial)
	if kind == domain.ConsumerKindRecordingRule {
		state.production = &domain.Production{
			ConsumerID: consumerID,
			Symbol: domain.Symbol{
				Domain: domain.DomainPrometheus,
				Kind:   domain.SymbolKindMetric,
				Name:   name,
			},
		}
	}
}

func (builder *discoveryBuilder) state(consumer domain.Consumer) *consumerState {
	if existing, exists := builder.consumers[consumer.ID]; exists {
		return existing
	}
	state := &consumerState{
		consumer:   consumer,
		references: make(map[string]domain.Reference),
		origins:    make(map[string]struct{}),
	}
	builder.consumers[consumer.ID] = state
	return state
}

func (builder *discoveryBuilder) addUsageReference(
	state *consumerState,
	metric string,
	origin evidenceOrigin,
	partial bool,
) {
	state.origins[string(origin)] = struct{}{}
	reference := domain.Reference{
		ConsumerID: state.consumer.ID,
		Symbol: domain.Symbol{
			Domain: domain.DomainPrometheus,
			Kind:   domain.SymbolKindMetric,
			Name:   metric,
		},
		Evidence: domain.Evidence{
			Method:      domain.EvidenceMethodUsageAPI,
			Confidence:  domain.ConfidenceConfirmed,
			Source:      domain.SourceLocation{URL: endpointURL(builder.source, origin)},
			Explanation: "Perses metrics-usage association",
		},
		Usage: domain.UsageSelector,
	}
	if partial {
		reference.Usage = domain.UsagePattern
		reference.Pattern = metric
		reference.RequiresResolution = true
		reference.Evidence.Confidence = domain.ConfidenceUnknown
		reference.Evidence.Explanation = "Perses partial metric usage requires resolution"
	}
	addReference(state, reference)
}

func (builder *discoveryBuilder) addLabelUncertainty(
	state *consumerState,
	metric string,
	origin evidenceOrigin,
) {
	addReference(state, domain.Reference{
		ConsumerID: state.consumer.ID,
		Symbol: domain.Symbol{
			Domain: domain.DomainPrometheus,
			Kind:   domain.SymbolKindMetric,
			Name:   metric,
		},
		Evidence: domain.Evidence{
			Method:      domain.EvidenceMethodUsageAPI,
			Confidence:  domain.ConfidenceUnknown,
			Source:      domain.SourceLocation{URL: endpointURL(builder.source, origin)},
			Explanation: "metrics-usage dashboard records do not include query label usage",
		},
		Usage:              domain.UsageUnknown,
		Pattern:            "label usage unavailable",
		RequiresResolution: true,
		ResolutionScope:    domain.ResolutionScopeLabels,
	})
}

func (builder *discoveryBuilder) build() domain.Discovery {
	ids := make([]string, 0, len(builder.consumers))
	for id := range builder.consumers {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	discovery := domain.Discovery{Diagnostics: builder.diagnostics}
	for _, id := range ids {
		state := builder.consumers[id]
		if state.consumer.Kind == domain.ConsumerKindAlertRule ||
			state.consumer.Kind == domain.ConsumerKindRecordingRule {
			builder.analyzeRule(state)
		}
		origins := make([]string, 0, len(state.origins))
		for origin := range state.origins {
			origins = append(origins, origin)
		}
		sort.Strings(origins)
		state.consumer.Metadata["usage_origins"] = strings.Join(origins, ",")
		discovery.Consumers = append(discovery.Consumers, state.consumer)

		references := make([]domain.Reference, 0, len(state.references))
		for _, reference := range state.references {
			references = append(references, reference)
		}
		sort.Slice(references, func(i, j int) bool {
			return referenceKey(references[i]) < referenceKey(references[j])
		})
		discovery.References = append(discovery.References, references...)
		if state.production != nil {
			discovery.Productions = append(discovery.Productions, *state.production)
		}
	}
	discovery.Diagnostics = builder.diagnostics
	return discovery
}

func (builder *discoveryBuilder) analyzeRule(state *consumerState) {
	origin := primaryOrigin(state)
	if state.consumer.Expression == "" {
		state.consumer.Unresolved = true
		builder.addDiagnostic(origin, fmt.Sprintf("%s %q has an empty expression", state.consumer.Kind, state.consumer.Name))
		return
	}
	analysis, err := tmrpromql.Analyze(state.consumer.Expression)
	if err != nil || len(analysis.Unresolved) != 0 {
		state.consumer.Unresolved = true
		message := "PromQL expression is unresolved"
		if err != nil {
			message = err.Error()
		} else {
			message = analysis.Unresolved[0].Reason
		}
		builder.addDiagnostic(origin, fmt.Sprintf("%s %q: %s", state.consumer.Kind, state.consumer.Name, message))
		return
	}
	for _, reference := range analysis.References {
		reference.ConsumerID = state.consumer.ID
		reference.Evidence.Source = state.consumer.Source
		addReference(state, reference)
	}
}

func (builder *discoveryBuilder) addDiagnostic(origin evidenceOrigin, message string) {
	builder.diagnostics = append(builder.diagnostics, domain.Diagnostic{
		Adapter:  "perses_metrics_usage",
		Source:   domain.SourceLocation{URL: endpointURL(builder.source, origin)},
		Message:  message,
		Required: builder.required,
	})
}

func (builder *discoveryBuilder) addEndpointDiagnostic(origin evidenceOrigin, err error) {
	builder.addDiagnostic(origin, err.Error())
}

func addReference(state *consumerState, reference domain.Reference) {
	key := referenceKey(reference)
	if _, exists := state.references[key]; !exists {
		state.references[key] = reference
	}
}

func referenceKey(reference domain.Reference) string {
	return strings.Join([]string{
		reference.ConsumerID,
		string(reference.Symbol.Domain),
		string(reference.Symbol.Kind),
		reference.Symbol.Parent,
		reference.Symbol.Name,
		string(reference.Usage),
		reference.Pattern,
		fmt.Sprint(reference.RequiresResolution),
		string(reference.ResolutionScope),
		string(reference.Evidence.Method),
		reference.Evidence.Source.File,
		reference.Evidence.Source.URL,
		reference.Evidence.Expression,
	}, "\x00")
}

func primaryOrigin(state *consumerState) evidenceOrigin {
	origins := make([]string, 0, len(state.origins))
	for origin := range state.origins {
		origins = append(origins, origin)
	}
	sort.Strings(origins)
	if len(origins) == 0 {
		return originMetrics
	}
	return evidenceOrigin(origins[0])
}

func endpointURL(source string, origin evidenceOrigin) string {
	return strings.TrimRight(source, "/") + "/api/v1/" + string(origin)
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
