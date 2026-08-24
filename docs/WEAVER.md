# OpenTelemetry Weaver Integration

TMR can consume structured JSON produced by `weaver registry diff` as an
optional change source. Weaver determines what changed in an OpenTelemetry
semantic-convention registry; TMR does not reimplement that diff.

OpenTelemetry and Prometheus remain separate domains. A similar-looking name
is not evidence that an OpenTelemetry metric or attribute becomes a particular
Prometheus metric or label. Every actionable Weaver change therefore needs an
explicit backend mapping or an explicit ignore reason.

## Produce a diff

Current Weaver supports V1 and V2 registry formats. Generate JSON directly:

```bash
weaver registry diff \
  --baseline-registry ./registry-old \
  --registry ./registry-new \
  --format json > ./weaver-diff.json
```

Add `--v2` when both registries use Weaver's V2 schema:

```bash
weaver registry diff \
  --v2 \
  --baseline-registry ./registry-old \
  --registry ./registry-new \
  --format json > ./weaver-diff-v2.json
```

TMR reads the resulting file. It does not invoke Weaver, clone registries, or
make a network request.

## Write the backend mapping

```yaml
apiVersion: tmr.weaver/v1alpha1
kind: WeaverMapping
metadata:
  name: checkout-http-migration
spec:
  mappings:
    - id: checkout-method
      weaver:
        kind: attribute
        type: renamed
        from: http.method
        to: http.request.method
      prometheus:
        kind: label_rename
        metric: checkout_server_request_duration_seconds
        from:
          label: http_method
        to:
          label: http_request_method

    - id: checkout-duration
      weaver:
        kind: metric
        type: renamed
        from: http.server.duration
        to: http.server.request.duration
      prometheus:
        kind: metric_rename
        from:
          metric: checkout_request_duration_seconds
        to:
          metric: checkout_server_request_duration_seconds

    - id: unrelated-rpc-metric
      weaver:
        kind: metric
        type: obsoleted
        from: rpc.client.duration
      ignore: This metric is not exported to Prometheus in this environment.
```

Each `weaver` selector must exactly match one diff item. A selector includes
the item kind, change type, old name, and new name for a rename.

Each entry must set exactly one resolution:

- `prometheus` defines a supported canonical metric or label change; or
- `ignore` records why the OpenTelemetry change does not affect the analyzed
  Prometheus environment.

Stale mapping entries that match no diff item are errors. Duplicate selectors
and IDs are errors. This prevents a typo from being interpreted as a completed
mapping.

## Validate and analyze

```bash
tmr validate \
  --weaver-diff ./weaver-diff.json \
  --weaver-mapping ./weaver-mapping.yaml

tmr analyze \
  --config ./tmr.yaml \
  --weaver-diff ./weaver-diff.json \
  --weaver-mapping ./weaver-mapping.yaml
```

`--migration` and the Weaver flags are alternative change sources. They cannot
be combined in one analysis.

If an actionable change has no entry, TMR reports
`requiresMapping=true` and exits `3` (`INCOMPLETE`). It does not run readiness
evaluation on a partial migration.

## Supported Weaver changes

TMR can map top-level Weaver metrics and registry attributes into the current
Prometheus change model. It retains other actionable top-level changes until
they are explicitly ignored:

| Weaver item | Weaver action | Explicit resolution |
| --- | --- | --- |
| metric | `renamed` | Prometheus `metric_rename` or `ignore` |
| metric | `removed` / `obsoleted` | Prometheus `metric_remove` or `ignore` |
| attribute | `renamed` | Prometheus `label_rename` or `ignore` |
| attribute | `removed` / `obsoleted` | Prometheus `label_remove` or `ignore` |
| any item | `uncategorized` | `ignore` only |
| event, span, entity, attribute group | rename/removal/obsoletion | `ignore` only |
| any item | `added` | no legacy-removal risk; omitted automatically |

Weaver's `updated` placeholder does not identify the changed field, so TMR
rejects it. Events, spans, entities, and attribute groups cannot map to a
current Prometheus change and therefore require an explicit `ignore` entry.

## Evidence

Every mapped canonical change includes source metadata in the versioned JSON
result:

- adapter and Weaver format;
- baseline and head registry identifiers;
- source item kind and change type;
- old and new OpenTelemetry identifiers;
- Weaver's note when present.

This metadata is explanatory evidence. The explicit Prometheus mapping and the
deterministic consumer graph remain the inputs to the readiness decision.

The adapter formats follow Weaver's current
[`SchemaChanges` documentation](https://github.com/open-telemetry/weaver/blob/main/docs/schema-changes.md)
and published
[`semconv.diff.v2.json` schema](https://github.com/open-telemetry/weaver/blob/main/schemas/semconv.diff.v2.json).
