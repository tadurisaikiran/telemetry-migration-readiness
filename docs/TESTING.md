# Testing and Verification

Testing is part of the safety contract. A parser or adapter error must never be
mistaken for evidence that a dependency does not exist.

## Implemented test layers

The current deterministic engine has:

- unit tests for every required migration validation rule;
- valid YAML fixtures covering all four implemented change kinds;
- invalid YAML fixtures with exact golden diagnostics;
- PromQL AST unit and fuzz tests;
- component fixtures for Prometheus rules, PrometheusRule CRDs, Grafana, Sloth,
  and Pyrra;
- cycle and transitive-chain graph tests;
- fail-closed readiness and required-source failure tests;
- an exact JSON golden report for the checkout migration;
- CLI integration tests for output and the permanent `0/1/2/3` exit contract;
- CI checks for formatting, vetting, and race-enabled tests.
- a pinned live Docker lifecycle against Prometheus, Grafana, and Sloth.

Run the local checks with:

```bash
gofmt -w .
go vet ./...
go test -race ./...
```

## Mandatory live E2E release gate

The live harness is implemented under `e2e/` and is a pull-request release
gate. It runs pinned versions for reproducibility; a weekly workflow exercises
previous-supported and upstream-latest combinations.

The harness runs a pinned Docker Compose stack containing a controlled
exporter, Prometheus, Grafana, and Sloth, with Pyrra added as a second tier. It
will exercise this telemetry lifecycle:

```text
old only -> dual write -> partial consumer migration
         -> complete consumer migration -> old telemetry removed
```

It must prove both prediction directions:

1. TMR reports `BLOCKED` before an intentionally premature cutover, and the
   isolated stack exhibits the predicted missing critical data.
2. TMR reports `READY` only after every required consumer is migrated, and the
   same critical queries, rules, dashboards, and SLOs continue working after
   legacy telemetry is removed.

The release gate proves:

- direct metric and label dependency detection;
- Grafana, alert, recording-rule, and SLO consumer discovery;
- complete transitive propagation through recording rules;
- fail-closed behavior for unresolved critical queries and required adapter
  failures;
- no panic or false safety result for malformed PromQL or graph cycles;
- independent `promtool` checks and rule tests;
- Sloth validation and generated-rule cross-checks;
- proof that the deterministic result retains authority (the adversarial AI
  test is added with the optional AI milestone).

Run the core and live layers with:

```bash
go test -race ./...
./e2e/scripts/run-e2e.sh
```

See [the E2E harness guide](../e2e/README.md) for pinned versions, scenario
expectations, and runtime assertions.

Pinned E2E tests will run on pull requests. Compatibility matrices and latest
upstream versions will run on scheduled workflows after the harness exists.
