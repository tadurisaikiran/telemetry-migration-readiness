# Runtime query evidence

TMR can import recently executed PromQL expressions as additive dependency
evidence. This distinguishes a consumer discovered in configuration from a
query that was also observed at runtime, while preserving the fail-closed rule
that no observation is not proof of no use.

## Prometheus query log

Prometheus can write every engine query as one JSON object per line. Configure
the exported or locally mounted log as follows:

```yaml
apiVersion: tmr/v1alpha1
sources:
  runtimeQueries:
    - path: ./evidence/prometheus-query.log
      format: prometheus_query_log
      window: 168h
      criticality: high
      required: true
analysis:
  includeTransitiveDependencies: true
  unresolvedReferencePolicy: error
policy:
  failOnCriticalLegacyConsumer: true
  failOnCriticalUnknown: true
  minimumBlockingCriticality: high
output:
  formats: [console, json, markdown]
```

The decoder consumes the documented `params.query`, `ts`, `httpRequest`, and
`ruleGroup` fields. Unknown fields are allowed for forward compatibility with
Prometheus. TMR retains API/rule origin and bounded method, path, file, and
group details, but deliberately discards client IP data. Prometheus query-log
configuration is documented at
<https://prometheus.io/docs/guides/query-log/>.

## Provider-neutral history

Systems that export a selected query history can write strict JSONL using the
versioned TMR schema:

```json
{"schemaVersion":"tmr-runtime-query/v1alpha1","timestamp":"2026-08-24T15:00:00Z","query":"sum(rate(http_requests_total[5m]))","origin":"grafana_query_history","source":"dashboard/checkout"}
```

Each non-empty line must contain exactly one object with:

- `schemaVersion`: exactly `tmr-runtime-query/v1alpha1`;
- `timestamp`: an RFC 3339 timestamp;
- `query`: a non-empty PromQL expression;
- `origin`: a bounded identifier containing letters, numbers, dot, dash, or
  underscore; and
- optional `source`: a bounded human-readable origin detail.

Configure this format with `format: tmr_query_history`. Unknown fields are
rejected so producers cannot silently change the contract.

## Deterministic aggregation

TMR groups byte-identical query expressions inside each source. It reports the
execution count, first and last observation, origin set, and origin details.
When `window` is nonzero, the window is anchored to the newest valid timestamp
in that source; it never uses the current clock. An event exactly on the cutoff
is included. `window: 0s`, the default, includes every valid event.

This data-anchored rule makes repeated analysis of an immutable export
reproducible. `executionsPerDay` is descriptive evidence only and cannot change
readiness policy.

## Safety semantics

Every distinct expression becomes a runtime query consumer and is parsed by
the official Prometheus PromQL parser. A dependency on a legacy metric or label
can therefore block removal just like a configured dashboard, rule, or SLO.
Runtime evidence never:

- removes a consumer found by another adapter;
- turns an unobserved configured query into a migrated or unaffected query;
- infers that an empty history means no usage; or
- overrides deterministic readiness policy.

Malformed or unresolved records produce diagnostics. A diagnostic from a
`required: true` source prevents `READY`; optional-source diagnostics remain
visible without asserting completeness. Missing required files and unmatched
required globs also fail closed.

## Bounds and handling

Each source is limited to 64 MiB, 500,000 non-empty records, and 1 MiB per
record. Individual query and origin fields have smaller bounds. Source patterns
are expanded locally; TMR does not call Prometheus or Grafana APIs and does not
phone home. Protect runtime exports as production observability data and delete
them according to your normal retention policy.
