// Package analysis orchestrates local adapters, graph construction, and the
// deterministic readiness evaluator.
package analysis

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/tadurisaikiran/telemetry-migration-readiness/adapters/grafana"
	"github.com/tadurisaikiran/telemetry-migration-readiness/adapters/persesusage"
	"github.com/tadurisaikiran/telemetry-migration-readiness/adapters/prometheusrules"
	"github.com/tadurisaikiran/telemetry-migration-readiness/adapters/pyrra"
	"github.com/tadurisaikiran/telemetry-migration-readiness/adapters/sloth"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/config"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/graph"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/impact"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/ownership"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/readiness"
	filesource "github.com/tadurisaikiran/telemetry-migration-readiness/internal/source"
)

// Run executes the complete deterministic local analysis pipeline.
func Run(
	ctx context.Context,
	configuration config.Config,
	migration domain.Migration,
) (readiness.Result, *graph.Graph, domain.Discovery, error) {
	discovery, dependencyGraph, err := Discover(ctx, configuration)
	if err != nil {
		return readiness.Result{}, nil, domain.Discovery{}, err
	}
	result, err := readiness.Evaluate(migration, discovery, dependencyGraph, ReadinessPolicy(configuration))
	if err != nil {
		return readiness.Result{}, nil, domain.Discovery{}, fmt.Errorf("evaluate readiness: %w", err)
	}
	return result, dependencyGraph, discovery, nil
}

// ReadinessPolicy converts validated configuration into the exact policy used
// by Run. Candidate reanalysis calls this same function to avoid policy drift.
func ReadinessPolicy(configuration config.Config) readiness.Policy {
	return readiness.Policy{
		FailOnCriticalLegacyConsumer: configuration.Policy.FailOnCriticalLegacyConsumer,
		FailOnCriticalUnknown:        configuration.Policy.FailOnCriticalUnknown,
		MinimumBlockingCriticality:   domain.Criticality(configuration.Policy.MinimumBlockingCriticality),
		IncludeTransitive:            configuration.Analysis.IncludeTransitiveDependencies,
	}
}

// Discover runs configured adapters and constructs the dependency graph.
func Discover(ctx context.Context, configuration config.Config) (domain.Discovery, *graph.Graph, error) {
	var discovery domain.Discovery
	loadPatterns(ctx, "prometheus_rules", configuration.Sources.PrometheusRules, &discovery,
		func(ctx context.Context, path string, required bool) (domain.Discovery, error) {
			return (prometheusrules.Loader{Required: required}).LoadFile(ctx, path)
		})
	loadPatterns(ctx, "grafana", configuration.Sources.Grafana, &discovery,
		func(ctx context.Context, path string, required bool) (domain.Discovery, error) {
			return (grafana.Loader{Required: required}).LoadFile(ctx, path)
		})
	loadPatterns(ctx, "sloth", configuration.Sources.Sloth, &discovery,
		func(ctx context.Context, path string, required bool) (domain.Discovery, error) {
			return (sloth.Loader{Required: required}).LoadFile(ctx, path)
		})
	loadPatterns(ctx, "pyrra", configuration.Sources.Pyrra, &discovery,
		func(ctx context.Context, path string, required bool) (domain.Discovery, error) {
			return (pyrra.Loader{Required: required}).LoadFile(ctx, path)
		})
	loadPersesUsage(ctx, configuration.Sources.PersesUsage, &discovery)
	if err := ownership.Enrich(ctx, configuration.Ownership, &discovery); err != nil {
		return domain.Discovery{}, nil, fmt.Errorf("enrich consumer ownership: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return domain.Discovery{}, nil, err
	}
	dependencyGraph, err := impact.BuildGraph(discovery)
	if err != nil {
		return domain.Discovery{}, nil, fmt.Errorf("build dependency graph: %w", err)
	}
	return discovery, dependencyGraph, nil
}

func loadPersesUsage(
	ctx context.Context,
	sources []config.PersesUsageSource,
	discovery *domain.Discovery,
) {
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return
		}
		timeout, err := time.ParseDuration(source.Timeout)
		if err != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, persesDiagnostic(source, err.Error()))
			continue
		}
		var token string
		if source.BearerTokenEnv != "" {
			var exists bool
			token, exists = os.LookupEnv(source.BearerTokenEnv)
			if !exists || token == "" {
				discovery.Diagnostics = append(discovery.Diagnostics, persesDiagnostic(
					source,
					fmt.Sprintf("bearer token environment variable %q is unset or empty", source.BearerTokenEnv),
				))
				continue
			}
		}
		additional, err := (persesusage.Loader{
			BaseURL:     source.URL,
			Required:    source.Required,
			Timeout:     timeout,
			BearerToken: token,
		}).Discover(ctx)
		if err != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, persesDiagnostic(source, err.Error()))
			continue
		}
		discovery.Append(additional)
	}
}

func persesDiagnostic(source config.PersesUsageSource, message string) domain.Diagnostic {
	return domain.Diagnostic{
		Adapter:  "perses_metrics_usage",
		Source:   domain.SourceLocation{URL: source.URL},
		Message:  message,
		Required: source.Required,
	}
}

type fileLoader func(context.Context, string, bool) (domain.Discovery, error)

func loadPatterns(
	ctx context.Context,
	adapter string,
	patterns []config.SourcePattern,
	discovery *domain.Discovery,
	load fileLoader,
) {
	loaded := make(map[string]struct{})
	for _, pattern := range patterns {
		if err := ctx.Err(); err != nil {
			return
		}
		files, err := filesource.Expand(pattern.Pattern)
		if err != nil {
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter:  adapter,
				Source:   domain.SourceLocation{File: pattern.Pattern},
				Message:  err.Error(),
				Required: pattern.Required,
			})
			continue
		}
		if len(files) == 0 {
			discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
				Adapter:  adapter,
				Source:   domain.SourceLocation{File: pattern.Pattern},
				Message:  "source pattern matched no files",
				Required: pattern.Required,
			})
			continue
		}
		for _, file := range files {
			if _, exists := loaded[file]; exists {
				continue
			}
			loaded[file] = struct{}{}
			additional, err := load(ctx, file, pattern.Required)
			if err != nil {
				discovery.Diagnostics = append(discovery.Diagnostics, domain.Diagnostic{
					Adapter:  adapter,
					Source:   domain.SourceLocation{File: file},
					Message:  err.Error(),
					Required: pattern.Required,
				})
				continue
			}
			discovery.Append(additional)
		}
	}
}
