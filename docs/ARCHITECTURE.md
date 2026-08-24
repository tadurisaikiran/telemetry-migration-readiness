# Architecture

Telemetry Migration Readiness analyzes telemetry contract changes and their
downstream consumers. The long-term product answers three questions:

1. What changed?
2. Which consumers still depend on the legacy contract?
3. Has deterministic policy established that backward compatibility can be
   removed?

The local deterministic pipeline described below is implemented today.

## Non-negotiable rules

1. The deterministic core is authoritative.
2. An LLM must never make or override a migration safety decision.
3. Formal parsers take precedence over regular expressions and model inference.
4. Missing, malformed, or unresolved required evidence fails closed.
5. Every dependency finding retains its evidence and source location.
6. Prometheus and OpenTelemetry are separate domains. Similar names do not
   establish a mapping.
7. External systems enter through adapters and do not shape the core model.
8. The core remains local-first and does not phone home.

## Implemented deterministic pipeline

```text
explicit migration or mapped Weaver diff + source configuration
             |
             v
strict validation and local adapters
             |
             v
official PromQL AST reference extraction
             |
             v
in-memory dependency graph
             |
             v
deterministic impact + readiness policy
             |
             v
console / JSON / Markdown reports
```

`internal/config` owns YAML-specific migration and configuration document
structs. It rejects unknown
fields, multiple YAML documents, oversized input, and invalid change shapes.
It then normalizes the document into `internal/domain` and applies reusable
domain validation.

`internal/domain` contains vendor-neutral migrations, symbols, consumers,
references, evidence, productions, diagnostics, source locations, and owners.
Adapter-specific document shapes never leak into this package.

`adapters/weaver` consumes current structured Weaver V1/V2 registry diffs and
requires explicit OpenTelemetry-to-Prometheus mappings. Missing mappings stop
the pipeline before readiness evaluation. It neither invokes Weaver nor infers
backend names.

The consumer adapters normalize Prometheus rule YAML, PrometheusRule CRDs, Grafana
dashboard JSON, Sloth SLO YAML, and Pyrra SLO YAML. Malformed or unresolved
required input becomes a diagnostic rather than evidence of absence.

`pkg/promql` uses Prometheus's official parser and walks the typed AST. It does
not use substring matching to establish metric or label dependencies.

`internal/graph`, `internal/impact`, and `internal/readiness` form the safety
core. The graph is rebuilt in memory for every run, traversal is cycle-safe,
and the readiness evaluator is the only component allowed to produce a safety
status.

`cmd/tmr` is a thin CLI boundary over that engine. Versioned JSON is the stable
machine API for Actions and future optional integrations. Exit codes are part
of the public contract. Progress percentages remain informational and never
establish safety.

## Next architectural layers

- OpenTelemetry, trace, and log analysis.
- AI explanation or remediation.
- Runtime evidence, APIs, MCP, and server/UI modes.
- Additional live end-to-end tiers described in `TESTING.md`.

These remain adapters or optional consumers of the deterministic engine. None
may weaken or override its readiness result.
