# When Telemetry Migrations Fail Silently

Changing a telemetry contract is unusually dangerous because the application
can stay healthy while its operational controls become blind. A renamed metric
does not make a dashboard process crash. A removed label does not necessarily
make an alerting rule invalid. The query can remain syntactically correct and
simply return no series.

That creates a failure mode that ordinary deployment health checks miss:

```text
service: healthy
telemetry pipeline: healthy
dashboard: empty
alert: no longer matches
SLO: measures incomplete traffic
```

This article develops a practical safety rule for telemetry migrations:

> Producer compatibility creates time to migrate consumers; it does not prove
> that the consumers have been migrated.

## The compatibility window is only the beginning

OpenTelemetry schemas exist partly because telemetry producers and consumers
otherwise need coordinated changes when semantic conventions evolve. The
[OpenTelemetry schema specification](https://opentelemetry.io/docs/specs/otel/schemas/)
describes that coordination problem directly.

The OpenTelemetry Collector's Schema Processor makes the transition more
operable. Its migration mode preserves old and new attributes during a rename,
giving teams a window to update queries, dashboards, and alerts before removing
the old names. As of August 23, 2026, the processor is marked alpha for traces,
metrics, and logs; its documentation also warns that migration mode can
increase attribute count and storage cost. See the
[Schema Processor documentation](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/processor/schemaprocessor/README.md).

Dual emission prevents immediate breakage, but it cannot answer the removal
question:

```text
Can we stop emitting the old contract now?
```

To answer that, we need evidence about every relevant consumer.

## A concrete metric and label migration

Consider a checkout service moving from:

```text
checkout_request_duration_seconds{http_method="GET"}
```

to:

```text
checkout_server_request_duration_seconds{http_request_method="GET"}
```

The producer can temporarily emit both versions. The consumer surface may
include:

- Grafana panels that query the raw histogram;
- Prometheus recording rules that derive latency and request-rate series;
- alerts that consume those derived series;
- Sloth SLO definitions whose generated rules query the counter;
- downstream rules that depend transitively on another recording rule.

The last case matters. Searching only for the old raw metric misses consumers
that reference a derived recording rule:

```text
checkout_request_duration_seconds
              |
              v
checkout:requests:rate1m
              |
        +-----+------+
        |            |
        v            v
  latency alert  checkout SLO
```

Removal safety therefore needs a dependency graph, not only a text search.

## Six states, two experiments

A useful migration test advances the same stack through these states:

| Stage | Producer | Consumers | Expected decision |
| --- | --- | --- | --- |
| Original | old | old | baseline |
| Compatibility introduced | old + new | old | `BLOCKED` |
| Partially migrated | old + new | mixed | `BLOCKED` |
| Ambiguous critical query | old + new | mostly new | `INCOMPLETE` |
| Consumer migration complete | old + new | new | `READY` |
| Legacy contract removed | new | new | `READY` and healthy |

There are two experiments hidden in this sequence.

First, remove the legacy metric before the legacy consumers are migrated. The
readiness analyzer should predict `BLOCKED`, and the running Prometheus stack
should then demonstrate the predicted failure: the derived series disappears.

Second, migrate every critical consumer, require a `READY` decision, remove the
legacy metric, and prove that the same critical recording rules still produce
series. A static analyzer should be judged against both outcomes. Predicting
breakage is not enough; it must also avoid permanently blocking a safe change.

## Syntax validity is not migration validity

Prometheus provides independent checks that belong in this workflow.
[`promtool check rules`](https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/)
validates rule files, and
[`promtool test rules`](https://prometheus.io/docs/prometheus/latest/configuration/unit_testing_rules/)
evaluates rules against synthetic input series.

Those checks answer whether the Prometheus artifacts are valid and behave as
specified. They do not know whether a metric referenced by a valid expression
will still exist after a producer migration. Both layers are necessary:

```text
promtool: is this rule valid and does it evaluate as expected?
readiness analysis: will its telemetry dependencies survive the change?
```

Sloth adds another independent layer. It generates Prometheus recording and
alerting rules from an SLO specification and provides a validation command for
CI, as documented by the
[Sloth project](https://github.com/slok/sloth). A migration test should validate
the SLO input, inspect the generated rule dependency, and then observe the
result in a running Prometheus instance.

Grafana should be real in this test too. Grafana supports file-based,
version-controlled datasource and dashboard provisioning, so a containerized
test can load the exact dashboard JSON being analyzed. The behavior is
documented in
[Grafana provisioning](https://grafana.com/docs/grafana/latest/administration/provisioning/).

## Unknown is a safety result

Telemetry repositories contain templated and dynamically constructed queries.
For example:

```text
${service}_request_duration_seconds
```

A deterministic parser cannot prove which metric that expression will select
without resolving the variable. If the consumer is critical, silently treating
it as unaffected creates a false sense of safety.

A fail-closed model uses three final decisions:

- `BLOCKED`: known legacy or incompatible critical consumers remain;
- `INCOMPLETE`: required evidence is missing or a critical reference is
  unresolved;
- `READY`: every required critical consumer is migrated or demonstrably
  compatible, with no unresolved critical evidence.

`INCOMPLETE` is not a parser failure disguised as a product feature. It is an
explicit statement that absence of evidence is not evidence of safe removal.

## Use ecosystem tools at their natural boundaries

Several existing tools answer adjacent questions:

- [OpenTelemetry Weaver](https://github.com/open-telemetry/weaver/blob/main/docs/usage.md)
  can generate structured differences between semantic-convention registries.
  That answers *what changed*.
- [Perses metrics-usage](https://github.com/perses/metrics-usage)
  collects metric usage from Prometheus rules, Grafana, and Perses, and exposes
  exact and partial metric references over an API. That helps answer *where a
  metric is used*.
- The OpenTelemetry Schema Processor provides a compatibility window in the
  data path.

Migration readiness is the lifecycle that joins those facts:

```text
change definition
      +
consumer evidence
      +
transitive dependencies
      +
criticality
      +
runtime verification
      =
removal decision
```

This boundary is important. Reimplementing registry diffing or ecosystem-wide
usage collection would create weaker duplicates. A readiness engine should
consume their structured output through adapters while preserving source
evidence.

## Reproduce the lifecycle

The Telemetry Migration Readiness repository contains the complete controlled
experiment described above. The normal run pins Go, Prometheus, Grafana, and
Sloth versions, starts a fresh Prometheus volume for each scenario, and checks
both the predicted failure and the successful final removal.

```bash
git clone https://github.com/tadurisaikiran/telemetry-migration-readiness.git
cd telemetry-migration-readiness
go test -race ./...
./e2e/scripts/run-e2e.sh
```

The scenario definitions and exact assertions are documented in the
[live E2E guide](../../e2e/README.md). Prometheus rule tests, Sloth validation,
Grafana provisioning, readiness exit codes, and live Prometheus queries all
have to agree before the lifecycle passes.

## The practical checklist

Before removing a compatibility layer, require evidence for each of these:

1. The change is represented as a machine-readable old-to-new contract.
2. Queries are parsed structurally rather than searched only as text.
3. Recording-rule outputs are included in a transitive dependency graph.
4. Dashboards, alerts, rules, and SLO sources retain file or API provenance.
5. Dynamic critical queries fail closed until resolved.
6. The unsafe cutover is proven to fail in a controlled environment.
7. The completed migration is proven to survive legacy removal.
8. The safety decision is deterministic and reproducible without an AI model.

The essential distinction is simple: dual emission says the migration can
start safely. Consumer evidence and runtime verification say when it can end.
