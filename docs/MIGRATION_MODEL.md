# Migration Model

The migration manifest explicitly describes a backend telemetry change.
Implemented domains are `prometheus`, `opentelemetry`, and `tempo`; symbols in
different domains never match without explicit mapping evidence.

## Envelope

Every manifest must contain exactly one YAML document with this envelope:

```yaml
apiVersion: telemetry-migration/v1alpha1
kind: Migration
metadata:
  name: checkout-http-migration
spec:
  description: Optional human-readable context.
  changes: []
```

`metadata.name` is required and `spec.changes` must contain at least one change.
Unknown YAML fields are rejected.

## Metric rename

```yaml
- id: checkout-duration
  kind: metric_rename
  domain: prometheus
  from:
    metric: checkout_request_duration_seconds
  to:
    metric: checkout_server_request_duration_seconds
```

The source and destination are required and must differ.

## Metric removal

```yaml
- id: legacy-checkout-total
  kind: metric_remove
  domain: prometheus
  from:
    metric: checkout_legacy_requests_total
```

`to` must be omitted because a removal has no replacement.

## Label rename

```yaml
- id: checkout-method
  kind: label_rename
  domain: prometheus
  metric: checkout_server_request_duration_seconds
  from:
    label: http_method
  to:
    label: http_request_method
```

The parent `metric`, source label, and destination label are required. The
labels must differ.

## Label removal

```yaml
- id: legacy-node-label
  kind: label_remove
  domain: prometheus
  metric: checkout_server_request_duration_seconds
  from:
    label: legacy_node
```

The parent `metric` and source label are required. `to` must be omitted.

## Span attribute rename or removal

```yaml
- id: span-http-method
  kind: span_attribute_rename
  domain: opentelemetry
  from:
    attribute: http.method
  to:
    attribute: http.request.method
```

Use `span_attribute_remove` and omit `to` for a removal. A backend-native Tempo
change can use `domain: tempo`; an OpenTelemetry change needs explicit mappings
before Tempo evidence can affect it.

## Resource attribute rename or removal

```yaml
- id: resource-zone
  kind: resource_attribute_remove
  domain: opentelemetry
  from:
    attribute: cloud.availability_zone
```

Use `resource_attribute_rename` with a distinct `to.attribute` for a rename.
Span and resource scopes are different symbol kinds and never match each other.

## Validation contract

Validation rejects:

- an unsupported API version or document kind;
- a missing migration name or empty change list;
- missing or duplicate change IDs;
- unsupported change kinds or domains;
- a rename without a destination;
- a removal with a destination;
- empty source or destination names;
- a label change without its parent metric;
- identical source and destination names;
- metric fields in a label endpoint or label fields in a metric endpoint;
- metric or label fields in an attribute endpoint, and attribute fields in a
  metric or label endpoint;
- a Prometheus change kind in an OpenTelemetry/Tempo domain or an attribute
  change kind in the Prometheus domain;
- unknown YAML fields, multiple documents, and manifests larger than 1 MiB.

Prometheus identifier grammar is not validated yet. That belongs with the
Prometheus parsing milestone; the current contract requires non-empty names.

## Canonical representation

Decoded endpoints become canonical symbols with an explicit domain and kind.
For a label, `Symbol.Parent` records the parent metric. Trace attributes have
an explicit span/resource kind and no parent. Removals have a nil destination.
YAML-specific structs remain private to the config package so future change
sources can produce the same domain model without mimicking YAML.
