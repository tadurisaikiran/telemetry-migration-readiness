# Perses metrics-usage integration

TMR can consume downstream-consumer evidence from a separately deployed
[Perses metrics-usage](https://github.com/perses/metrics-usage) service. This is
an optional HTTP adapter: TMR does not import Perses packages, start the
service, or require it for local analysis.

## Configuration

```yaml
apiVersion: tmr/v1alpha1
sources:
  persesUsage:
    - url: https://metrics-usage.example.com
      required: true
      timeout: 10s
      bearerTokenEnv: TMR_PERSES_TOKEN
```

`url` must be an absolute `http` or `https` URL without credentials, a query,
or a fragment. A path prefix is supported. `required` defaults to `true`, and
`timeout` defaults to `10s` with a maximum of two minutes.

`bearerTokenEnv` names an environment variable; the secret itself never
appears in configuration or reports. When configured, an unset or empty
variable is a source failure. Redirects are allowed only within the configured
scheme and host so credentials cannot be forwarded to another origin.

## Imported API evidence

For each run, TMR reads:

- `GET /api/v1/metrics?used=true` for exact metric usage;
- `GET /api/v1/partial_metrics` for unresolved metric patterns; and
- `GET /api/v1/pending_usages` for usage awaiting full metric metadata.

Responses are limited to 32 MiB each and must contain exactly one top-level
JSON object. Unknown response fields are ignored for forward compatibility,
while malformed objects and non-success HTTP statuses remain diagnostics.
Both current dashboard `uid`/`title` fields and legacy `id`/`name` examples are
accepted.

The adapter produces deterministic canonical evidence:

- dashboards become medium-criticality dashboard consumers;
- alert rules become high-criticality alert consumers;
- recording rules become medium-criticality consumers and produced metric
  symbols, enabling transitive impact paths;
- returned rule expressions are analyzed with the official Prometheus PromQL
  parser rather than trusted as opaque associations; and
- every API association retains the endpoint URL and `usage_api` evidence
  method, alongside independent `promql_ast` evidence for rule expressions.

## Deliberate uncertainty

An exact dashboard-to-metric association proves metric usage, but it does not
identify labels used inside the dashboard query. TMR therefore adds a
label-scoped unresolved reference. Metric rename/removal analysis can still use
the exact association; a label change on that metric is `UNCERTAIN` unless
another adapter supplies query-level evidence.

Partial metric keys remain pattern references with unknown confidence and
`requiresResolution=true`. Current `matchingMetrics` entries are also imported
as exact associations, but the pattern remains because today's match set cannot
prove future safety. Pending usage is retained with its own endpoint provenance
rather than discarded.

## Failure behavior

Failure of the exact metrics endpoint invalidates that source. Failure of a
supplemental partial or pending endpoint preserves already imported exact
evidence and adds a diagnostic. A diagnostic from a required source prevents
`READY` and returns the analysis `INCOMPLETE` exit code (`3`) unless a stronger
legacy finding already blocks the migration. With `required: false`, the same
failure stays visible but does not by itself prevent readiness.

This behavior is intentional: an unavailable evidence source is never treated
as proof that no consumer exists.
