# Telemetry Migration Readiness

> Know what will break before changing your telemetry, automatically migrate
> what can be migrated, and know when it is safe to remove backward
> compatibility.

Telemetry changes are API changes. Renaming or removing a metric or label can
silently empty dashboards, disable alerts, and invalidate SLOs. Telemetry
Migration Readiness (`tmr`) is an open-source, local-first tool for analyzing
those migrations before backward compatibility is removed.

## Current status

The deterministic Prometheus v0.1 engine is implemented through the ecosystem
integration milestones. Its local adapters do not require AI, a database, a
network connection, or a hosted service; remote evidence sources are explicit
and optional.

Implemented:

- Prometheus-domain metric renames and removals.
- Prometheus-domain label renames and removals.
- Strict YAML decoding with unknown-field rejection.
- Official Prometheus PromQL AST analysis, including selectors, matchers,
  aggregations, vector matching, and label functions.
- Prometheus rule, PrometheusRule CRD, Grafana, Sloth, and Pyrra adapters.
- Cycle-safe transitive dependency graphs through recording rules.
- Fail-closed `READY`, `BLOCKED`, and `INCOMPLETE` decisions.
- Console, versioned JSON, Markdown, and graph JSON output.
- `analyze`, `advise`, `validate`, `explain`, and `graph` CLI commands.
- Optional OpenTelemetry Weaver V1/V2 registry-diff import with mandatory,
  explicit Prometheus backend mappings.
- Optional Perses metrics-usage HTTP evidence for dashboards, recording rules,
  alert rules, partial metrics, and pending usage.
- Optional read-only AI explanations through a provider-neutral local process;
  deterministic readiness remains authoritative.
- A pinned live Prometheus/Grafana/Sloth migration lifecycle that verifies
  predictions against runtime behavior.

## Requirements

- Go 1.27 or newer.

## Build and run

```bash
go build -o ./bin/tmr ./cmd/tmr
./bin/tmr validate --migration ./examples/checkout-migration/migration.yaml
./bin/tmr analyze \
  --config ./examples/checkout-migration/tmr.yaml \
  --migration ./examples/checkout-migration/migration.yaml
```

Successful validation prints:

```text
Migration manifest is valid.
Changes: 2
```

Invalid input is written to standard error and returns a nonzero exit code.

The analysis exit-code contract is permanent: `0` means policy passed, `1`
means a tool/configuration/runtime error, `2` means the migration is blocked,
and `3` means required evidence is incomplete.

## GitHub Action

```yaml
permissions:
  contents: read
  pull-requests: write

steps:
  - uses: actions/checkout@v5
  - uses: tadurisaikiran/telemetry-migration-readiness@v1
    with:
      config: tmr.yaml
      migration: migration.yaml
```

The Action writes the Markdown report to the job summary and creates or updates
one pull-request comment by default. See
[the Action documentation](docs/GITHUB_ACTION.md) for inputs, outputs, and
permission details.

## Example manifest

```yaml
apiVersion: telemetry-migration/v1alpha1
kind: Migration
metadata:
  name: checkout-http-migration
spec:
  description: Migrate checkout HTTP duration telemetry.
  changes:
    - id: checkout-duration
      kind: metric_rename
      domain: prometheus
      from:
        metric: checkout_request_duration_seconds
      to:
        metric: checkout_server_request_duration_seconds

    - id: checkout-method
      kind: label_rename
      domain: prometheus
      metric: checkout_server_request_duration_seconds
      from:
        label: http_method
      to:
        label: http_request_method
```

See [the migration model](docs/MIGRATION_MODEL.md) for the complete implemented
schema and validation rules.

## Weaver registry diffs

Weaver can be used as an alternative change source when an explicit backend
mapping is available:

```bash
tmr analyze \
  --config ./tmr.yaml \
  --weaver-diff ./weaver-diff.json \
  --weaver-mapping ./weaver-mapping.yaml
```

TMR never assumes that an OpenTelemetry identifier maps directly to a
Prometheus name. See [the Weaver integration guide](docs/WEAVER.md).

## Perses metrics-usage evidence

TMR can augment local discovery from a separately deployed Perses
metrics-usage service:

```yaml
sources:
  persesUsage:
    - url: https://metrics-usage.example.com
      required: true
      timeout: 10s
      bearerTokenEnv: TMR_PERSES_TOKEN
```

The adapter consumes the documented API only; Perses is not a TMR dependency.
See [the Perses metrics-usage integration guide](docs/PERSES.md).

## Optional AI explanations

AI is disabled unless `tmr advise` is given an explicit local provider
executable:

```bash
tmr advise \
  --config ./tmr.yaml \
  --migration ./migration.yaml \
  --question "Why is this blocked, and what should migrate first?" \
  --ai-command ./my-tmr-ai-provider
```

TMR sends a bounded, redacted JSON evidence packet over standard input and
accepts one strict JSON explanation on standard output. The provider cannot
return a readiness status or a patch. `advise` preserves the deterministic
exit code, so a useful explanation of a blocked migration still exits `2`.
See [the AI explanation protocol](docs/AI_AGENT.md) and
[threat model](docs/THREAT_MODEL.md).

## Design principles

- Deterministic analysis owns facts and safety decisions.
- Parsing or adapter failures must never be interpreted as absence of risk.
- TMR remains useful without an LLM, network connection, database, or hosted
  service.
- AI output is explanatory and can neither weaken evidence nor change status.
- Telemetry domains remain separate unless an explicit mapping connects them.
- Every dependency finding retains evidence and provenance.

The architecture and milestone boundaries are documented in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). The mandatory verification plan is
documented in [docs/TESTING.md](docs/TESTING.md).

For a problem-first explanation of the migration lifecycle, read
[When Telemetry Migrations Fail Silently](docs/articles/when-telemetry-migrations-fail-silently.md).

See [the roadmap](docs/ROADMAP.md), [contribution guide](CONTRIBUTING.md), and
[security policy](SECURITY.md) before proposing or reporting work.

Engineers evaluating a real migration can use the
[design-user program guide](docs/DESIGN_USER_PROGRAM.md) and submit only
sanitized findings through the design-user feedback issue form.

## Development

```bash
gofmt -w .
go vet ./...
go test -race ./...
```

## License

Apache License 2.0. See [LICENSE](LICENSE).
