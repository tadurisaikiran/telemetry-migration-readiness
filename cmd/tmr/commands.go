package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	weaveradapter "github.com/tadurisaikiran/telemetry-migration-readiness/adapters/weaver"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/analysis"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/config"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/domain"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/explanation"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/graph"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/readiness"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/remediation"
	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/report"
)

type stringListFlag []string

func (values *stringListFlag) String() string {
	return strings.Join(*values, ",")
}

func (values *stringListFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func runAnalyze(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("analyze", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "path to a TMR YAML configuration")
	migrationPath := flags.String("migration", "", "path to a migration YAML manifest")
	weaverDiffPath := flags.String("weaver-diff", "", "path to a Weaver registry diff JSON document")
	weaverMappingPath := flags.String("weaver-mapping", "", "path to an explicit Weaver backend mapping")
	format := flags.String("format", "", "report format: console, json, or markdown")
	output := flags.String("output", "", "optional report output path")
	if err := flags.Parse(args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 || *configPath == "" {
		fmt.Fprintln(stderr, "analyze requires --config and one change source and accepts no positional arguments")
		return 1
	}
	if *migrationPath != "" && (*weaverDiffPath != "" || *weaverMappingPath != "") {
		fmt.Fprintln(stderr, "--migration and --weaver-diff/--weaver-mapping are mutually exclusive")
		return 1
	}
	if *migrationPath == "" && (*weaverDiffPath == "" || *weaverMappingPath == "") {
		fmt.Fprintln(stderr, "analyze requires --migration or both --weaver-diff and --weaver-mapping")
		return 1
	}

	configuration, err := config.LoadConfig(ctx, *configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	migration, err := loadSelectedMigration(ctx, *migrationPath, *weaverDiffPath, *weaverMappingPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		if isWeaverIncomplete(err) {
			return 3
		}
		return 1
	}
	result, _, _, err := analysis.Run(ctx, configuration, migration)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	selectedFormat := *format
	if selectedFormat == "" {
		selectedFormat = configuration.Output.Formats[0]
	}
	contents, err := renderResult(selectedFormat, result)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if err := writeOutput(*output, contents, stdout); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return readinessExitCode(result.Summary.Status)
}

func runAdvise(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("advise", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "path to a TMR YAML configuration")
	migrationPath := flags.String("migration", "", "path to a migration YAML manifest")
	weaverDiffPath := flags.String("weaver-diff", "", "path to a Weaver registry diff JSON document")
	weaverMappingPath := flags.String("weaver-mapping", "", "path to an explicit Weaver backend mapping")
	question := flags.String("question", "", "read-only migration question for the AI provider")
	providerCommand := flags.String("ai-command", "", "local AI provider executable")
	providerTimeout := flags.Duration("ai-timeout", 30*time.Second, "AI provider timeout (maximum 2m)")
	var providerArgs stringListFlag
	flags.Var(&providerArgs, "ai-arg", "argument passed directly to the AI provider executable (repeatable)")
	if err := flags.Parse(args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 || *configPath == "" || *question == "" || *providerCommand == "" {
		fmt.Fprintln(stderr, "advise requires --config, one change source, --question, and --ai-command and accepts no positional arguments")
		return 1
	}
	if *providerTimeout <= 0 || *providerTimeout > 2*time.Minute {
		fmt.Fprintln(stderr, "--ai-timeout must be positive and no greater than 2m")
		return 1
	}
	if *migrationPath != "" && (*weaverDiffPath != "" || *weaverMappingPath != "") {
		fmt.Fprintln(stderr, "--migration and --weaver-diff/--weaver-mapping are mutually exclusive")
		return 1
	}
	if *migrationPath == "" && (*weaverDiffPath == "" || *weaverMappingPath == "") {
		fmt.Fprintln(stderr, "advise requires --migration or both --weaver-diff and --weaver-mapping")
		return 1
	}

	configuration, err := config.LoadConfig(ctx, *configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	migration, err := loadSelectedMigration(ctx, *migrationPath, *weaverDiffPath, *weaverMappingPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		if isWeaverIncomplete(err) {
			return 3
		}
		return 1
	}
	result, target, _, err := analysis.Run(ctx, configuration, migration)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	request, err := explanation.BuildRequest(*question, result, target)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	response, err := (explanation.CommandClient{
		Path:    *providerCommand,
		Args:    providerArgs,
		Timeout: *providerTimeout,
	}).Explain(ctx, request)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if err := explanation.Render(stdout, request, response); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return readinessExitCode(result.Summary.Status)
}

func runRemediate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("remediate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "path to a TMR YAML configuration")
	migrationPath := flags.String("migration", "", "path to a migration YAML manifest")
	weaverDiffPath := flags.String("weaver-diff", "", "path to a Weaver registry diff JSON document")
	weaverMappingPath := flags.String("weaver-mapping", "", "path to an explicit Weaver backend mapping")
	providerCommand := flags.String("ai-command", "", "local AI provider executable")
	providerTimeout := flags.Duration("ai-timeout", 30*time.Second, "AI provider timeout (maximum 2m)")
	var providerArgs stringListFlag
	flags.Var(&providerArgs, "ai-arg", "argument passed directly to the AI provider executable (repeatable)")
	if err := flags.Parse(args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 || *configPath == "" || *providerCommand == "" {
		fmt.Fprintln(stderr, "remediate requires --config, one change source, and --ai-command and accepts no positional arguments")
		return 1
	}
	if *providerTimeout <= 0 || *providerTimeout > 2*time.Minute {
		fmt.Fprintln(stderr, "--ai-timeout must be positive and no greater than 2m")
		return 1
	}
	if *migrationPath != "" && (*weaverDiffPath != "" || *weaverMappingPath != "") {
		fmt.Fprintln(stderr, "--migration and --weaver-diff/--weaver-mapping are mutually exclusive")
		return 1
	}
	if *migrationPath == "" && (*weaverDiffPath == "" || *weaverMappingPath == "") {
		fmt.Fprintln(stderr, "remediate requires --migration or both --weaver-diff and --weaver-mapping")
		return 1
	}

	configuration, err := config.LoadConfig(ctx, *configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	migration, err := loadSelectedMigration(ctx, *migrationPath, *weaverDiffPath, *weaverMappingPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		if isWeaverIncomplete(err) {
			return 3
		}
		return 1
	}
	result, _, discovery, err := analysis.Run(ctx, configuration, migration)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	request, err := remediation.BuildRequest(result)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if len(request.Targets) == 0 {
		fmt.Fprintln(stdout, "No confirmed direct local Prometheus-rule or Grafana expression is patchable for this migration.")
		fmt.Fprintf(stdout, "Current authoritative status remains: %s\n", result.Summary.Status)
		return readinessExitCode(result.Summary.Status)
	}
	response, err := (remediation.CommandClient{
		Path:    *providerCommand,
		Args:    providerArgs,
		Timeout: *providerTimeout,
	}).Propose(ctx, request)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	candidates, err := remediation.Validate(
		ctx,
		request,
		response,
		migration,
		discovery,
		analysis.ReadinessPolicy(configuration),
	)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if err := remediation.Render(stdout, request, candidates); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return readinessExitCode(result.Summary.Status)
}

func loadSelectedMigration(ctx context.Context, migrationPath, weaverDiffPath, weaverMappingPath string) (domain.Migration, error) {
	if migrationPath != "" {
		return config.LoadMigration(ctx, migrationPath)
	}
	return loadWeaverMigration(ctx, weaverDiffPath, weaverMappingPath)
}

func loadWeaverMigration(ctx context.Context, diffPath, mappingPath string) (domain.Migration, error) {
	migration, _, err := weaveradapter.LoadMigration(ctx, diffPath, mappingPath)
	return migration, err
}

func isWeaverIncomplete(err error) bool {
	var target *weaveradapter.MappingRequiredError
	if errors.As(err, &target) {
		return true
	}
	var unsupported *weaveradapter.UnsupportedChangeError
	return errors.As(err, &unsupported)
}

func runGraph(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("graph", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "path to a TMR YAML configuration")
	format := flags.String("format", "json", "graph format (json)")
	output := flags.String("output", "", "optional graph output path")
	if err := flags.Parse(args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 || *configPath == "" {
		fmt.Fprintln(stderr, "graph requires --config and accepts no positional arguments")
		return 1
	}
	if *format != "json" {
		fmt.Fprintln(stderr, "graph --format currently supports only json")
		return 1
	}
	configuration, err := config.LoadConfig(ctx, *configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	_, target, err := analysis.Discover(ctx, configuration)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	contents, err := report.GraphJSON(target)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	if err := writeOutput(*output, contents, stdout); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func runExplain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("explain", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "path to a TMR YAML configuration")
	symbolName := flags.String("symbol", "", "Prometheus metric name")
	if err := flags.Parse(args); err != nil {
		return flagExitCode(err)
	}
	if flags.NArg() != 0 || *configPath == "" || *symbolName == "" {
		fmt.Fprintln(stderr, "explain requires --config and --symbol and accepts no positional arguments")
		return 1
	}
	configuration, err := config.LoadConfig(ctx, *configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	_, target, err := analysis.Discover(ctx, configuration)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	start := graph.SymbolNodeID(domain.Symbol{
		Domain: domain.DomainPrometheus,
		Kind:   domain.SymbolKindMetric,
		Name:   *symbolName,
	})
	paths := target.ImpactPaths(start)
	fmt.Fprintln(stdout, *symbolName)
	if len(paths) == 0 {
		fmt.Fprintln(stdout, "\nNo confirmed dependents found.")
		return 0
	}
	fmt.Fprintln(stdout, "\nDependency paths:")
	for _, path := range paths {
		end, exists := target.Node(path.Nodes[len(path.Nodes)-1])
		if !exists || end.Consumer == nil {
			continue
		}
		fmt.Fprintf(stdout, "  %s\n", readablePath(target, path))
	}
	return 0
}

func renderResult(format string, result readiness.Result) ([]byte, error) {
	if format == "json" {
		return report.JSON(result)
	}
	var output bytes.Buffer
	switch format {
	case "console":
		if err := report.Console(&output, result); err != nil {
			return nil, err
		}
	case "markdown":
		if err := report.Markdown(&output, result); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported report format %q", format)
	}
	return output.Bytes(), nil
}

func writeOutput(path string, contents []byte, stdout io.Writer) error {
	if path == "" {
		_, err := stdout.Write(contents)
		return err
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		return fmt.Errorf("write output %q: %w", path, err)
	}
	return nil
}

func readablePath(target *graph.Graph, path graph.Path) string {
	names := make([]string, 0, len(path.Nodes))
	for _, nodeID := range path.Nodes {
		node, exists := target.Node(nodeID)
		if !exists {
			names = append(names, nodeID)
			continue
		}
		names = append(names, node.Name)
	}
	return strings.Join(names, " -> ")
}

func flagExitCode(err error) int {
	if err == flag.ErrHelp {
		return 0
	}
	return 1
}

func readinessExitCode(status readiness.Status) int {
	switch status {
	case readiness.StatusReady:
		return 0
	case readiness.StatusBlocked:
		return 2
	case readiness.StatusIncomplete:
		return 3
	default:
		return 1
	}
}
