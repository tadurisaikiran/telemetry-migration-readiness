package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/tadurisaikiran/telemetry-migration-readiness/internal/config"
)

const usageText = `Telemetry Migration Readiness

Usage:
  tmr analyze --config <path> (--migration <path> | --weaver-diff <path> --weaver-mapping <path>) [--format console|json|markdown]
  tmr validate --migration <path>
  tmr validate --weaver-diff <path> --weaver-mapping <path>
  tmr validate --config <path>
  tmr explain --config <path> --symbol <metric>
  tmr graph --config <path> [--output <path>]

Commands:
  analyze   Analyze migration readiness
  validate  Validate configuration and migration manifests
  explain   Explain dependency paths for one Prometheus metric
  graph     Export the dependency graph as JSON
`

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return 1
	}

	switch args[0] {
	case "analyze":
		return runAnalyze(ctx, args[1:], stdout, stderr)
	case "validate":
		return runValidate(ctx, args[1:], stdout, stderr)
	case "explain":
		return runExplain(ctx, args[1:], stdout, stderr)
	case "graph":
		return runGraph(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usageText)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usageText)
		return 1
	}
}

func runValidate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: tmr validate (--migration <path> | --weaver-diff <path> --weaver-mapping <path> | --config <path>)")
		flags.PrintDefaults()
	}

	migrationPath := flags.String("migration", "", "path to a migration YAML manifest")
	weaverDiffPath := flags.String("weaver-diff", "", "path to a Weaver registry diff JSON document")
	weaverMappingPath := flags.String("weaver-mapping", "", "path to an explicit Weaver backend mapping")
	configPath := flags.String("config", "", "path to a TMR YAML configuration")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "validate does not accept positional arguments: %v\n", flags.Args())
		return 1
	}
	if *migrationPath == "" && *configPath == "" && *weaverDiffPath == "" && *weaverMappingPath == "" {
		fmt.Fprintln(stderr, "--migration, --weaver-diff with --weaver-mapping, or --config is required")
		return 1
	}
	if *migrationPath != "" && (*weaverDiffPath != "" || *weaverMappingPath != "") {
		fmt.Fprintln(stderr, "--migration and --weaver-diff/--weaver-mapping are mutually exclusive")
		return 1
	}
	if (*weaverDiffPath == "") != (*weaverMappingPath == "") {
		fmt.Fprintln(stderr, "--weaver-diff and --weaver-mapping must be provided together")
		return 1
	}

	if *migrationPath != "" {
		migration, err := config.LoadMigration(ctx, *migrationPath)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "Migration manifest is valid.")
		fmt.Fprintf(stdout, "Changes: %d\n", len(migration.Changes))
	}
	if *weaverDiffPath != "" {
		migration, err := loadWeaverMigration(ctx, *weaverDiffPath, *weaverMappingPath)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			if isWeaverIncomplete(err) {
				return 3
			}
			return 1
		}
		fmt.Fprintln(stdout, "Weaver diff and mapping are valid.")
		fmt.Fprintf(stdout, "Changes: %d\n", len(migration.Changes))
	}
	if *configPath != "" {
		configuration, err := config.LoadConfig(ctx, *configPath)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "TMR configuration is valid.")
		fmt.Fprintf(stdout, "Sources: %d\n", sourceCount(configuration))
	}
	return 0
}

func sourceCount(configuration config.Config) int {
	return len(configuration.Sources.PrometheusRules) +
		len(configuration.Sources.Grafana) +
		len(configuration.Sources.Sloth) +
		len(configuration.Sources.Pyrra) +
		len(configuration.Sources.PersesUsage)
}
