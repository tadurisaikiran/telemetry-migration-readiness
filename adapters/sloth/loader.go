// Package sloth discovers PromQL references in Sloth SLO specifications.
package sloth

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	tmrpromql "github.com/tadurisaikiran/telemetry-migration-readiness/pkg/promql"
)

const maxSlothBytes = 8 << 20

// Loader controls whether unresolved SLO evidence is required.
type Loader struct {
	Required bool
}

// LoadFile reads one Sloth specification.
func (loader Loader) LoadFile(ctx context.Context, path string) (domain.Discovery, error) {
	if err := ctx.Err(); err != nil {
		return domain.Discovery{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("open Sloth SLO %q: %w", path, err)
	}
	defer file.Close()
	result, err := loader.Parse(filepath.Clean(path), file)
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("load Sloth SLO %q: %w", path, err)
	}
	return result, nil
}

// Parse discovers event and raw SLI queries in a Sloth specification.
func (loader Loader) Parse(source string, reader io.Reader) (domain.Discovery, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maxSlothBytes+1))
	if err != nil {
		return domain.Discovery{}, fmt.Errorf("read Sloth SLO: %w", err)
	}
	if len(contents) > maxSlothBytes {
		return domain.Discovery{}, fmt.Errorf("Sloth SLO exceeds the %d-byte size limit", maxSlothBytes)
	}
	var document slothDocument
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return domain.Discovery{}, fmt.Errorf("decode Sloth SLO: %w", err)
	}
	if strings.TrimSpace(document.Service) == "" {
		return domain.Discovery{}, fmt.Errorf("Sloth service is required")
	}
	if len(document.SLOs) == 0 {
		return domain.Discovery{}, fmt.Errorf("Sloth slos must contain at least one SLO")
	}

	var discovery domain.Discovery
	for index, slo := range document.SLOs {
		if strings.TrimSpace(slo.Name) == "" {
			return domain.Discovery{}, fmt.Errorf("Sloth slos[%d].name is required", index)
		}
		queries := slo.queries()
		consumer := domain.Consumer{
			ID:          fmt.Sprintf("sloth:%s:%s:%s:%d", source, document.Service, slo.Name, index),
			Kind:        domain.ConsumerKindSLO,
			Name:        fmt.Sprintf("%s / %s", document.Service, slo.Name),
			Source:      domain.SourceLocation{File: source, Line: slo.line, Column: slo.column},
			Criticality: domain.CriticalityCritical,
			Expression:  strings.Join(queries, "\n"),
			Metadata: map[string]string{
				"service":   document.Service,
				"objective": fmt.Sprint(slo.Objective),
			},
		}
		if len(queries) == 0 {
			consumer.Unresolved = true
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter:  "sloth",
				Source:   consumer.Source,
				Message:  "SLO has no supported PromQL SLI query",
				Required: loader.Required,
			})
		}
		for _, query := range queries {
			analysis, analysisErr := tmrpromql.Analyze(expandKnownTemplates(query))
			if analysisErr != nil || len(analysis.Unresolved) != 0 {
				consumer.Unresolved = true
				message := "PromQL expression is unresolved"
				if analysisErr != nil {
					message = analysisErr.Error()
				} else {
					message = analysis.Unresolved[0].Reason
				}
				discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
					Adapter:  "sloth",
					Source:   consumer.Source,
					Message:  message,
					Required: loader.Required,
				})
				continue
			}
			for _, reference := range analysis.References {
				reference.ConsumerID = consumer.ID
				reference.Evidence.Source = consumer.Source
				discovery.References = append(discovery.References, reference)
			}
		}
		discovery.Consumers = append(discovery.Consumers, consumer)
	}
	return discovery, nil
}

func expandKnownTemplates(query string) string {
	result := strings.ReplaceAll(query, "{{.window}}", "5m")
	result = strings.ReplaceAll(result, "{{ .window }}", "5m")
	return result
}

type slothDocument struct {
	Version string     `yaml:"version"`
	Service string     `yaml:"service"`
	SLOs    []slothSLO `yaml:"slos"`
}

type slothSLO struct {
	Name      string   `yaml:"name"`
	Objective any      `yaml:"objective"`
	SLI       slothSLI `yaml:"sli"`
	line      int
	column    int
}

func (slo *slothSLO) UnmarshalYAML(node *yaml.Node) error {
	type plainSLO slothSLO
	var decoded plainSLO
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*slo = slothSLO(decoded)
	slo.line = node.Line
	slo.column = node.Column
	return nil
}

type slothSLI struct {
	Events struct {
		ErrorQuery string `yaml:"error_query"`
		TotalQuery string `yaml:"total_query"`
	} `yaml:"events"`
	Raw struct {
		ErrorRatioQuery string `yaml:"error_ratio_query"`
	} `yaml:"raw"`
}

func (slo slothSLO) queries() []string {
	var queries []string
	for _, query := range []string{
		slo.SLI.Events.ErrorQuery,
		slo.SLI.Events.TotalQuery,
		slo.SLI.Raw.ErrorRatioQuery,
	} {
		if strings.TrimSpace(query) != "" {
			queries = append(queries, query)
		}
	}
	return queries
}
