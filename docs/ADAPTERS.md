# Adapters

Adapters translate external artifact shapes into the canonical consumer,
reference, production, and diagnostic model. They never decide readiness.

## Local adapters

- `prometheusrules` reads standard Prometheus rule groups and Prometheus
  Operator `PrometheusRule` resources. Recording-rule outputs become produced
  symbols so impact can propagate transitively.
- `grafana` reads file-exported or API-envelope dashboard JSON, walks nested
  panels, and analyzes Prometheus targets. Templated or malformed expressions
  remain unresolved.
- `sloth` reads service-level objectives and analyzes raw error/total event
  queries. SLO consumers default to critical.
- `pyrra` reads Pyrra SLO resources and analyzes indicator metric expressions.
  SLO consumers default to critical.

Every configured source can be marked `required`. A load, parse, or expansion
failure on a required source prevents `READY`. All adapters preserve file,
line, expression, extraction method, and confidence where available.

## Remote evidence adapters

- `persesusage` calls a configured Perses metrics-usage service and normalizes
  its exact, partial, and pending usage into dashboards, alert rules, recording
  rules, references, and productions. Rule expressions are parsed again with
  TMR's official PromQL AST walker. Dashboard label usage remains explicitly
  unresolved because the API association does not carry dashboard query text.

The adapter has bounded response sizes, a source timeout, optional bearer-token
authentication by environment variable, and same-origin redirects only. A
failed required endpoint prevents `READY`; optional-source failures remain
visible diagnostics. See [the Perses integration guide](PERSES.md).

Remote and ecosystem integrations add evidence without becoming prerequisites
for the local deterministic core.

- `tempo` strictly loads versioned local TraceQL consumer manifests, validates
  each expression through Tempo's documented Search API, and extracts only
  exact `span.` and `resource.` attributes. Explicit one-to-one mappings add
  separate OpenTelemetry references; names are never equated implicitly.

Tempo responses, query counts, expression sizes, source duration, redirects,
and credentials are bounded. Search results are discarded: the API call is
used only to establish official parser acceptance. Unsupported or ambiguous
scopes remain unresolved required evidence. See [the Tempo integration
guide](TEMPO.md).

## Runtime evidence adapters

- `runtimequeries` reads local Prometheus query-log JSONL and versioned TMR
  query-history JSONL. It aggregates exact expressions, preserves execution
  counts and observation bounds, and parses every query with the official
  PromQL AST walker.

Runtime observations create additional consumers and references. They never
delete or downgrade configured consumers, and an empty source never establishes
that a query is unused. Malformed records remain visible diagnostics; a failed
required source prevents `READY`. Input files, lines, records, queries, origin
fields, and time windows are bounded. Prometheus client IP data is deliberately
not retained. See [runtime query evidence](RUNTIME_EVIDENCE.md).

## Advisory enrichment

`internal/ownership` enriches adapter output from explicit repository metadata,
GitHub CODEOWNERS, and Grafana dashboard tags. It runs after all local and
remote consumer adapters, so the same precedence is applied to every consumer.
Invalid ownership evidence produces visible non-blocking diagnostics and never
changes dependency completeness or readiness. See [consumer ownership
discovery](OWNERSHIP.md).

## Change adapters

- `weaver` reads current V1 and V2 `weaver registry diff` JSON. Every
  actionable metric or registry-attribute change requires an exact explicit
  Prometheus mapping or a documented ignore decision. Missing mappings produce
  `requiresMapping=true` and prevent readiness evaluation.

Weaver is never invoked by TMR and OpenTelemetry names are never inferred to be
Prometheus names. See [the Weaver integration guide](WEAVER.md).
