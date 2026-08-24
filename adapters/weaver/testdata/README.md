# Weaver test fixtures

The V1 fixture follows Weaver's `SchemaChanges` representation in
`crates/weaver_version/src/schema_changes.rs`. The V2 fixture follows the
published
[`semconv.diff.v2.json`](https://github.com/open-telemetry/weaver/blob/main/schemas/semconv.diff.v2.json)
schema. Both describe the same actionable registry changes so adapter behavior
can be compared across formats.

The mapping is TMR-owned. It deliberately demonstrates that an OpenTelemetry
attribute or metric name is never treated as a Prometheus label or metric name
without an explicit backend mapping.
