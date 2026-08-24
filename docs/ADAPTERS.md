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

## Change adapters

- `weaver` reads current V1 and V2 `weaver registry diff` JSON. Every
  actionable metric or registry-attribute change requires an exact explicit
  Prometheus mapping or a documented ignore decision. Missing mappings produce
  `requiresMapping=true` and prevent readiness evaluation.

Weaver is never invoked by TMR and OpenTelemetry names are never inferred to be
Prometheus names. See [the Weaver integration guide](WEAVER.md).
