# Tempo and TraceQL evidence

TMR can discover dependencies on Tempo-indexed span and resource attributes in
a strict local inventory of TraceQL consumers. Tempo's own Search API validates
the complete expression before TMR extracts any confirmed reference.

## Migration manifest

An OpenTelemetry span attribute rename uses the canonical migration model:

```yaml
apiVersion: telemetry-migration/v1alpha1
kind: Migration
metadata:
  name: http-span-method
spec:
  changes:
    - id: span-http-method
      kind: span_attribute_rename
      domain: opentelemetry
      from:
        attribute: http.method
      to:
        attribute: http.request.method
```

Resource attributes use `resource_attribute_rename` or
`resource_attribute_remove`. Span and resource scopes never match each other.
Use `domain: tempo` only when the migration describes a backend-native Tempo
attribute contract rather than an OpenTelemetry contract.

## Explicit domain mapping

OpenTelemetry and Tempo are separate domains. Even identical names require a
mapping when an OpenTelemetry migration is evaluated against Tempo evidence:

```yaml
mappings:
  traceAttributes:
    - scope: span
      opentelemetry: http.method
      tempo: http.method
    - scope: span
      opentelemetry: http.request.method
      tempo: http.request.method
    - scope: resource
      opentelemetry: service.name
      tempo: service.name
```

Mappings are one-to-one within a scope. A rename needs mappings for both the
source and destination. If any required Tempo source is configured, a missing
mapping for an OpenTelemetry trace-attribute change is required incomplete
evidence. With optional Tempo sources only, the same diagnostic is advisory.

## Query inventory

TMR does not assume that Tempo is a query-history database. Saved searches,
dashboard TraceQL, runbook queries, or other important consumers should be
exported into a versioned local manifest:

```yaml
apiVersion: tmr.tempo/v1alpha1
kind: TraceQueries
queries:
  - id: checkout-http
    name: Checkout HTTP traces
    criticality: critical
    expression: '{ span.http.method = "GET" && resource.service.name = "checkout" }'
```

The document is strictly decoded. Query IDs must be unique bounded identifiers;
names, expressions, the query count, and the complete manifest are bounded.
Unknown fields and multiple YAML documents are rejected.

Configure one or more local inventories and the Tempo origin that validates
them:

```yaml
sources:
  tempoQueries:
    - url: https://tempo.example.com/tempo
      path: ./trace-queries/*.yaml
      required: true
      timeout: 60s
      bearerTokenEnv: TMR_TEMPO_TOKEN
      criticality: high
```

Per-query criticality overrides the source default. Required files, mappings,
validation calls, and conservative extraction diagnostics prevent `READY`.
The 60-second default accommodates a cold local Tempo querier; deployments with
known warm-up behavior can set any positive timeout up to two minutes.

## Official validation boundary

For every expression, TMR performs a read-only `GET /api/search` request with
the documented `q`, `limit`, `start`, and `end` parameters. The query uses a
one-result empty historical interval. Results are discarded; only a successful
HTTP response proves that Tempo's official parser accepted the complete
expression. The API is documented at
<https://grafana.com/docs/tempo/latest/api_docs/>.

After validation, TMR extracts exact `span.` and `resource.` attributes,
including quoted attribute segments. Unscoped attributes and `parent.`,
`event.`, `link.`, or `instrumentation.` custom scopes remain unresolved
because Milestone 16 does not model them. Templated attributes also remain
unresolved. Intrinsics such as `span:duration` are not custom-attribute
dependencies. TraceQL scope rules are documented at
<https://grafana.com/docs/tempo/latest/traceql/construct-traceql-queries/>.

TMR does not import or copy Tempo's AGPL parser into the Apache-licensed core.
The HTTP boundary keeps Tempo optional while still relying on its current
official grammar rather than a competing local grammar.

## Network and security behavior

- URLs must be absolute HTTP(S) origins without user information, a query, or
  a fragment.
- Redirects are limited and must remain on the configured origin.
- Bearer tokens come only from an explicitly named environment variable.
- Query strings and response bodies are omitted from validation errors.
- Response size and total source duration are bounded.
- TraceQL text is sent only to the explicitly configured Tempo deployment.

TMR does not mutate Tempo, write dashboards, or retain search results. Treat
TraceQL inventories as sensitive operational configuration.
