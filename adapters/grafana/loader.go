// Package grafana discovers PromQL consumers in exported Grafana dashboard
// JSON files.
package grafana

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	tmrpromql "github.com/tadurisaikiran/telemetry-migration-readiness/pkg/promql"
)

const maxDashboardBytes = 32 << 20

// Loader controls whether unresolved dashboard queries are required evidence.
type Loader struct {
	Required bool
}

// LoadFile reads one exported Grafana dashboard.
func (loader Loader) LoadFile(ctx context.Context, path string) (domain.Discovery, error) {
	if err := ctx.Err(); err != nil {
		return domain.Discovery{}, fmt.Errorf("load Grafana dashboard %q: %w", path, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("open Grafana dashboard %q: %w", path, err)
	}
	defer file.Close()
	discovery, err := loader.Parse(filepath.Clean(path), file)
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("load Grafana dashboard %q: %w", path, err)
	}
	return discovery, nil
}

// Parse discovers panel targets and dashboard variables containing PromQL.
func (loader Loader) Parse(source string, reader io.Reader) (domain.Discovery, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maxDashboardBytes+1))
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("read dashboard: %w", err)
	}
	if len(contents) > maxDashboardBytes {
		return domain.Discovery{}, fmt.Errorf("dashboard exceeds the %d-byte size limit", maxDashboardBytes)
	}

	var envelope struct {
		Dashboard json.RawMessage `json:"dashboard"`
	}
	if err := json.Unmarshal(contents, &envelope); err != nil {
		return domain.Discovery{}, fmt.Errorf("decode dashboard JSON: %w", err)
	}
	dashboardJSON := contents
	if len(envelope.Dashboard) != 0 && string(envelope.Dashboard) != "null" {
		dashboardJSON = envelope.Dashboard
	}

	var dashboard dashboardJSONDocument
	if err := json.Unmarshal(dashboardJSON, &dashboard); err != nil {
		return domain.Discovery{}, fmt.Errorf("decode dashboard document: %w", err)
	}
	if strings.TrimSpace(dashboard.Title) == "" {
		return domain.Discovery{}, fmt.Errorf("dashboard title is required")
	}
	if dashboard.UID == "" {
		dashboard.UID = strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	}

	criticality := dashboardCriticality(dashboard.Tags)
	var discovery domain.Discovery
	for _, panel := range dashboard.Panels {
		loader.discoverPanel(source, dashboard, panel, criticality, &discovery)
	}
	for index, variable := range dashboard.Templating.List {
		expression := variableExpression(variable.Query, variable.Definition)
		if strings.TrimSpace(expression) == "" {
			continue
		}
		dataSource := dataSourceType(variable.DataSource)
		if isKnownNonPrometheus(dataSource) {
			continue
		}
		metadata := map[string]string{
			"dashboard_uid":   dashboard.UID,
			"dashboard_title": dashboard.Title,
			"variable":        variable.Name,
		}
		addDashboardTags(metadata, dashboard.Tags)
		consumer := domain.Consumer{
			ID:          fmt.Sprintf("grafana:%s:%s:variable:%s:%d", source, dashboard.UID, variable.Name, index),
			Kind:        domain.ConsumerKindQuery,
			Name:        fmt.Sprintf("%s / variable %s", dashboard.Title, variable.Name),
			Source:      domain.SourceLocation{File: source},
			Criticality: criticality,
			Expression:  expression,
			Metadata:    metadata,
		}
		loader.analyzeConsumer(consumer, dataSource, &discovery)
	}
	return discovery, nil
}

type dashboardJSONDocument struct {
	UID        string          `json:"uid"`
	Title      string          `json:"title"`
	Tags       []string        `json:"tags"`
	DataSource json.RawMessage `json:"datasource"`
	Panels     []panelJSON     `json:"panels"`
	Templating struct {
		List []variableJSON `json:"list"`
	} `json:"templating"`
}

type panelJSON struct {
	ID         int             `json:"id"`
	Title      string          `json:"title"`
	DataSource json.RawMessage `json:"datasource"`
	Targets    []targetJSON    `json:"targets"`
	Panels     []panelJSON     `json:"panels"`
}

type targetJSON struct {
	RefID      string          `json:"refId"`
	Expr       string          `json:"expr"`
	Hide       bool            `json:"hide"`
	DataSource json.RawMessage `json:"datasource"`
}

type variableJSON struct {
	Name       string          `json:"name"`
	Query      json.RawMessage `json:"query"`
	Definition string          `json:"definition"`
	DataSource json.RawMessage `json:"datasource"`
}

func (loader Loader) discoverPanel(
	source string,
	dashboard dashboardJSONDocument,
	panel panelJSON,
	criticality domain.Criticality,
	discovery *domain.Discovery,
) {
	panelDataSource := dataSourceType(panel.DataSource)
	if panelDataSource == "" {
		panelDataSource = dataSourceType(dashboard.DataSource)
	}
	for targetIndex, target := range panel.Targets {
		if target.Hide || strings.TrimSpace(target.Expr) == "" {
			continue
		}
		dataSource := dataSourceType(target.DataSource)
		if dataSource == "" {
			dataSource = panelDataSource
		}
		if isKnownNonPrometheus(dataSource) {
			continue
		}
		refID := target.RefID
		if refID == "" {
			refID = strconv.Itoa(targetIndex)
		}
		metadata := map[string]string{
			"dashboard_uid":   dashboard.UID,
			"dashboard_title": dashboard.Title,
			"panel_id":        strconv.Itoa(panel.ID),
			"panel_title":     panel.Title,
			"ref_id":          refID,
		}
		addDashboardTags(metadata, dashboard.Tags)
		consumer := domain.Consumer{
			ID:          fmt.Sprintf("grafana:%s:%s:panel:%d:%s", source, dashboard.UID, panel.ID, refID),
			Kind:        domain.ConsumerKindDashboardPanel,
			Name:        fmt.Sprintf("%s / %s [%s]", dashboard.Title, panel.Title, refID),
			Source:      domain.SourceLocation{File: source},
			Criticality: criticality,
			Expression:  target.Expr,
			Metadata:    metadata,
		}
		loader.analyzeConsumer(consumer, dataSource, discovery)
	}
	for _, nested := range panel.Panels {
		loader.discoverPanel(source, dashboard, nested, criticality, discovery)
	}
}

func (loader Loader) analyzeConsumer(consumer domain.Consumer, dataSource string, discovery *domain.Discovery) {
	analysis, err := tmrpromql.Analyze(consumer.Expression)
	if err != nil || len(analysis.Unresolved) != 0 {
		consumer.Unresolved = true
		message := "PromQL expression is unresolved"
		if err != nil {
			message = err.Error()
		} else if len(analysis.Unresolved) != 0 {
			message = analysis.Unresolved[0].Reason
		}
		if dataSource == "" {
			message += "; dashboard datasource type is not explicit"
		}
		discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
			Adapter:  "grafana",
			Source:   consumer.Source,
			Message:  message,
			Required: loader.Required,
		})
	} else {
		for _, reference := range analysis.References {
			reference.ConsumerID = consumer.ID
			reference.Evidence.Source = consumer.Source
			discovery.References = append(discovery.References, reference)
		}
	}
	discovery.Consumers = append(discovery.Consumers, consumer)
}

func dataSourceType(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var scalar string
	if err := json.Unmarshal(raw, &scalar); err == nil {
		return strings.ToLower(scalar)
	}
	var object struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &object); err == nil {
		return strings.ToLower(object.Type)
	}
	return ""
}

func isKnownNonPrometheus(dataSource string) bool {
	if dataSource == "" || strings.Contains(dataSource, "prometheus") {
		return false
	}
	for _, known := range []string{"loki", "tempo", "elasticsearch", "cloudwatch", "influx", "jaeger", "zipkin"} {
		if strings.Contains(dataSource, known) {
			return true
		}
	}
	return false
}

func dashboardCriticality(tags []string) domain.Criticality {
	for _, tag := range tags {
		switch strings.ToLower(tag) {
		case "critical", "paging", "slo":
			return domain.CriticalityCritical
		case "production", "prod":
			return domain.CriticalityHigh
		}
	}
	return domain.CriticalityMedium
}

func addDashboardTags(metadata map[string]string, tags []string) {
	if len(tags) == 0 {
		return
	}
	unique := make(map[string]struct{}, len(tags))
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, exists := unique[tag]; exists {
			continue
		}
		unique[tag] = struct{}{}
		normalized = append(normalized, tag)
	}
	if len(normalized) == 0 {
		return
	}
	sort.Strings(normalized)
	encoded, _ := json.Marshal(normalized)
	metadata["dashboard_tags"] = string(encoded)
}

func variableExpression(query json.RawMessage, definition string) string {
	if len(query) != 0 {
		var scalar string
		if err := json.Unmarshal(query, &scalar); err == nil && scalar != "" {
			return scalar
		}
		var object struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(query, &object); err == nil && object.Query != "" {
			return object.Query
		}
	}
	return definition
}
