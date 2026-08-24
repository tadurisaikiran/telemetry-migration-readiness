# Testing and Verification

Testing is part of the safety contract. A parser or adapter error must never be
mistaken for evidence that a dependency does not exist.

The optional AI explanation boundary is tested as an adversarial protocol:
status fields and unknown consumer IDs are rejected, provider timeouts and
oversized output are bounded, secrets are redacted, terminal controls are
removed, stable risk ordering is asserted, and CLI tests prove that model prose
cannot change readiness exit codes.

Candidate remediation adds independent query, YAML, dashboard, and full graph
tests. Adversarial cases cover invalid PromQL, retained legacy references,
missing destinations, secret-like provider text, duplicate or unknown targets,
ambiguous artifact scalars, response status/patch claims, timeouts, oversized
process output, and proof that source files remain byte-for-byte unchanged.

Ownership discovery has parser, wildcard, source-order, last-match, precedence,
joint-owner, ambiguity, determinism, and fuzz coverage. An integration invariant
compares ownership-disabled, valid, and malformed runs and requires identical
readiness summaries; malformed ownership diagnostics must remain advisory.

Runtime query evidence has decoder, aggregation, deterministic-window,
resource-bound, unresolved-query, required/optional failure, and fuzz coverage.
Integration tests prove that observed legacy queries block removal, observed
destination-only queries do not, and an empty history cannot erase a blocker
discovered from configured artifacts.

Trace evidence has scoped/quoted attribute extraction, deduplication, mapping,
strict-manifest, Tempo authentication, parser rejection, timeout, response
bound, redirect, integration, exact-domain matching, and fuzz coverage. Tests
prove that legacy OTel attributes block only through an explicit mapping and
that a missing required mapping is `INCOMPLETE`.

## Implemented test layers

The current deterministic engine has:

- unit tests for every required migration validation rule;
- valid YAML fixtures and validation tests covering all implemented metric,
  label, span-attribute, and resource-attribute change kinds;
- invalid YAML fixtures with exact golden diagnostics;
- PromQL AST unit and fuzz tests;
- CODEOWNERS and strict ownership-metadata unit and fuzz tests;
- Prometheus query-log and TMR query-history unit and fuzz tests;
- TraceQL attribute-scanner unit and fuzz tests plus Tempo API component tests;
- component fixtures for Prometheus rules, PrometheusRule CRDs, Grafana, Sloth,
  and Pyrra;
- cycle and transitive-chain graph tests;
- fail-closed readiness and required-source failure tests;
- an exact JSON golden report for the checkout migration;
- CLI integration tests for output and the permanent `0/1/2/3` exit contract;
- CI checks for formatting, vetting, and race-enabled tests.
- pinned live Docker lifecycles against Prometheus, Grafana, and Sloth, plus a
  digest-pinned Tempo TraceQL validation tier.

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
./e2e/scripts/run-tempo-e2e.sh
```

See [the E2E harness guide](../e2e/README.md) for pinned versions, scenario
expectations, and runtime assertions.

Pinned E2E tests will run on pull requests. Compatibility matrices and latest
upstream versions will run on scheduled workflows after the harness exists.
