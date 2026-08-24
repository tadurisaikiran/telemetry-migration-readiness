# Roadmap

The roadmap is ordered by evidence and user value. Milestone completion means
the documented acceptance tests pass; it does not imply every ecosystem or
deployment is supported.

## Implemented

- Repository and canonical migration model (Milestones 0–1).
- PromQL AST extraction and Prometheus rule discovery (Milestones 2–3).
- In-memory transitive graph (Milestone 4).
- Grafana, Sloth, and Pyrra local adapters (Milestones 5–6).
- Fail-closed readiness and versioned reports (Milestones 7–8).
- GitHub Action packaging (Milestone 9).
- Weaver registry-diff import with explicit backend mappings (Milestone 10).
- Perses metrics-usage evidence through a bounded, fail-closed HTTP adapter
  (Milestone 11).
- Provider-neutral, read-only AI explanation over redacted deterministic
  evidence (Milestone 12).
- Pinned live Prometheus/Grafana/Sloth migration lifecycle.

## Current release path

1. Community health files and issue/branch/PR workflow.
2. Contributor-sized issues and design-user feedback loop.
3. Compatibility, failure-injection, fuzz, vulnerability, and benchmark gates.
4. `v0.1.0` binaries, checksums, provenance, and a reproducible demo.

## Optional integrations after the core release gate

- Deterministically validated candidate AI remediation.
- CODEOWNERS, repository metadata, and dashboard ownership inference.
- Runtime query evidence.
- TraceQL, LogQL, Collector configuration, MCP, and server/UI modes.

These additions cannot weaken the local deterministic readiness result. The
ordering may change in response to design-user evidence; changes should be
recorded in issues and pull requests.
