# Validated candidate remediation

`tmr remediate` asks an explicitly configured AI provider for expression
replacements, then independently validates each proposal. It never applies a
patch.

```bash
tmr remediate \
  --config ./tmr.yaml \
  --migration ./migration.yaml \
  --ai-command ./my-tmr-ai-provider \
  --ai-timeout 30s
```

The command supports the same explicit migration or mapped Weaver change
source as `analyze`. Provider and protocol failures exit `1`; successful output
preserves the current deterministic `0/2/3` readiness exit code.

## Eligible targets

TMR sends only consumers that satisfy every condition:

- classification is `LEGACY_ONLY`;
- a confirmed direct reference matches a rename source;
- the rename has an explicit destination;
- the consumer is in a local Prometheus rule YAML or exported Grafana dashboard
  JSON file;
- the expression and identity contain no secret-like redactions; and
- the expression is nonempty.

Metric/label removals, unresolved evidence, transitive-only consumers, remote
API consumers, SLO documents, and arbitrary files are excluded. A transitive
alert should be repaired at the confirmed upstream recording-rule target.

## Provider protocol

The request schema is:

```text
tmr-ai-remediation-request/v1alpha1
```

Each deterministic target includes an opaque target ID, consumer identity,
criticality, artifact kind, source location, exact current expression, and the
canonical migration source/destination symbols. Targets are ordered by
criticality and stable IDs.

The provider returns exactly one strict JSON document:

```json
{
  "schemaVersion": "tmr-ai-remediation-response/v1alpha1",
  "candidates": [
    {
      "id": "checkout-recording-rule",
      "targetId": "target-0123456789abcdef",
      "beforeExpression": "rate(old_metric[5m])",
      "afterExpression": "rate(new_metric[5m])",
      "rationale": "Use the explicit metric rename destination."
    }
  ]
}
```

The provider cannot return a source path, locator, status, validation claim,
patch application, command, or production action. Unknown fields are rejected.
Target IDs and before expressions must match the request exactly, and only one
candidate per target is accepted.

## Deterministic validation

For every candidate, TMR performs all of these checks:

1. Parse replacement PromQL with the official Prometheus AST parser.
2. Reject unresolved syntax or secret-like content.
3. Prove the legacy metric/label reference is absent.
4. Prove the explicit rename destination is present.
5. Find exactly one matching scalar in the original source artifact.
6. Replace that scalar only in memory and record its YAML line/column or JSON
   Pointer.
7. Reparse the complete candidate artifact through the Prometheus-rule or
   Grafana adapter.
8. Prove the target consumer now contains the destination reference and not the
   legacy reference.
9. Replace that source's discovery in memory, rebuild the dependency graph, and
   rerun the exact configured readiness policy.
10. Reject a candidate whose target remains `LEGACY_ONLY` or `UNCERTAIN`.

Only then does output say `VALIDATED CANDIDATE`. The report contains a precise
locator, before/after expression, consumer classification, and simulated
readiness status.

## Important limitations

No file is written. No branch, pull request, dashboard API, or production
system is changed. Human review and an explicit separate edit are required.

Validation proves syntax and telemetry dependency movement; it does not prove
that range windows, aggregation, thresholds, or other query semantics are
equivalent. Run Prometheus rule tests and the full TMR analysis after applying
any candidate manually. A simulated result is not the current authoritative
state.

See [the threat model](THREAT_MODEL.md) before connecting a provider to
sensitive artifacts.
