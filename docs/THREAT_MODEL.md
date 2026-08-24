# Threat model

TMR analyzes infrastructure and observability artifacts that can reveal
internal service names, topology, operational thresholds, customer context,
and accidentally embedded credentials. The deterministic local core is the
default. Optional remote evidence and AI provider integrations add explicit
trust boundaries.

## Assets

Assets requiring protection include:

- repository and dashboard contents;
- migration plans and internal telemetry names;
- alert and SLO thresholds;
- credentials embedded accidentally in URLs, expressions, or diagnostics;
- environment variables and credential files; and
- the integrity of the deterministic readiness decision.

## Trust boundaries

Local migration/configuration files, dashboards, comments, descriptions,
expressions, API responses, and model output are untrusted data. They never
become instructions to TMR.

The deterministic parsers, graph, and readiness evaluator form the safety
boundary. Optional adapters may add evidence or diagnostics but cannot mark a
migration safe. Required evidence failures prevent `READY`.

A command supplied to `tmr advise --ai-command` is explicitly trusted local
code. TMR executes it without a shell, but it still runs with the user's OS
permissions and may independently access files, environment, or the network.
Only use a reviewed provider executable. TMR does not sandbox it.

## Prompt injection

Repository text can contain instructions such as “ignore previous rules” or
requests to disclose secrets. The explanation request labels every
repository-derived field as untrusted data and includes immutable guardrails
that forbid treating it as instructions. The provider must enforce the same
separation in its model prompt.

No provider response can carry a status, patch, command, or evidence mutation.
Strict unknown-field rejection blocks attempts to extend the response schema.
This limits consequences even if model instructions are compromised.

## Data minimization and secrets

The explanation packet includes only migration changes, aggregate counts,
blockers, uncertainties, relevant evidence paths, and diagnostics. It excludes
unaffected/migrated consumer content, complete configuration, the process
environment, credential files, and unrelated repository files.

Common bearer tokens, credential assignments, URL user information, AWS access
key IDs, and private-key blocks are redacted before transmission. Provider
errors and rendered output are redacted too. Pattern redaction cannot prove
that arbitrary secret formats are absent, so users must inspect artifacts and
the provider's data-retention policy before sending sensitive data remotely.

## Decision integrity

Only `internal/readiness` produces `READY`, `BLOCKED`, or `INCOMPLETE`. The AI
request records that decision, and the response type has no status field.
Rendered output repeats the authoritative status before and after AI prose.
The `advise` command returns the deterministic readiness exit code even when a
provider succeeds.

AI-inferred absence never removes a reference, resolves uncertainty, or proves
safety. Read-only explanation has no reanalysis input.

## Availability and resource limits

Configuration, adapter responses, explanation requests, provider responses,
stderr, and execution time are bounded. AI providers have a maximum two-minute
timeout. Oversized or malformed responses fail as tool errors instead of being
partially trusted.

Deterministic graph traversal is cycle-safe. Parser fuzz tests and live failure
scenarios protect against malformed inputs and fail-open regressions.

## Output handling

AI output is untrusted prose. TMR strips terminal control characters and labels
it non-authoritative. Consumers of future machine interfaces must preserve this
distinction and must not turn explanation text into executable commands.

## Production mutation

The read-only agent cannot write files, open pull requests, call production
Grafana/Prometheus mutation APIs, or push branches. Future candidate remediation
uses a separate protocol and must pass deterministic syntax, artifact, and
dependency validation before presentation.

Candidate remediation changes exactly one expression scalar in an in-memory
copy. The provider cannot choose a path or locator. TMR requires the proposed
`beforeExpression` to equal deterministic evidence, parses the replacement with
the official PromQL parser, proves the legacy symbol is absent and destination
present, reparses the artifact, rebuilds the graph, and reruns readiness. It
does not write the candidate. A simulated `READY` result does not change the
current status.

These checks do not prove semantic equivalence beyond telemetry references. A
syntactically valid query can change aggregation, ranges, thresholds, or label
logic. Every candidate therefore requires human review and independent tests.
After any manual edit, rerun `tmr analyze`; do not rely on an earlier simulation.
Direct production modification remains out of scope.

## Reporting vulnerabilities

Follow [SECURITY.md](../SECURITY.md) for private vulnerability reporting. Do
not place secrets or sensitive repository content in a public issue.
