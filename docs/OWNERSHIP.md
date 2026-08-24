# Consumer ownership discovery

TMR can enrich discovered consumers with repository-local ownership evidence so
a blocker report identifies who should investigate it. Ownership is advisory:
it never changes a consumer classification, resolves uncertainty, validates a
candidate patch, or changes `READY`, `BLOCKED`, or `INCOMPLETE`.

Ownership discovery is opt-in. Add an `ownership` section to `tmr.yaml`:

```yaml
ownership:
  repositoryRoot: .
  metadata:
    - .tmr/ownership.yaml
  codeowners:
    enabled: true
    # Omit path to use GitHub's normal search order.
    # path: .github/CODEOWNERS
  dashboardTags: true
```

All configured metadata and CODEOWNERS paths are relative to
`repositoryRoot` and cannot escape it. An omitted `ownership` section performs
no ownership file reads or owner assignment.

## Precedence and confidence

TMR applies the following fixed precedence. A lower-ranked source cannot
replace a higher-ranked result.

| Precedence | Evidence | Confidence | Conflict behavior |
| ---: | --- | --- | --- |
| 1 | Explicit TMR ownership metadata | `confirmed` | Last matching rule wins |
| 2 | Existing adapter-supplied owner | `confirmed` | Preserved unless explicit metadata matches |
| 3 | GitHub CODEOWNERS | `high` | Last matching pattern wins; multiple owners remain joint owners |
| 4 | Grafana `team:`, `owner:`, or `owned-by:` tags | `medium` | Multiple distinct values remain explicit candidates and are marked ambiguous |

The JSON consumer contains `owner` when evidence selects an owner. Its metadata
also records `ownership.source`, `ownership.confidence`, and `ownership.rule`.
Ambiguous tag evidence leaves `owner` unset and adds a stable JSON array in
`ownership.candidates` plus `ownership.ambiguous: "true"`. Read-only AI
explanation receives these fields so it may explain whom to contact, but it
cannot choose an owner or change readiness.

An empty last-matching CODEOWNERS rule intentionally leaves `owner` unset and
adds `ownership.unassigned: "true"`; lower-confidence dashboard tags do not
override that repository decision.

## Explicit repository metadata

An ownership metadata file uses a strict, versioned schema:

```yaml
apiVersion: tmr.ownership/v1alpha1
kind: Ownership
spec:
  rules:
    - id: checkout-dashboards
      match:
        path: dashboards/checkout/**
        consumerKind: dashboard_panel
        dashboardTag: production
      owner:
        name: Checkout Platform
        email: checkout-platform@example.com

    - id: checkout-alert
      match:
        consumerId: prometheus:monitoring/checkout.yaml:0:checkout:alert:CheckoutDown:0
      owner:
        name: Checkout On-call
```

Every rule needs a unique `id`, at least one matcher, and `owner.name`. Populated
matchers are combined with AND. `path` is repository-relative and supports the
same conservative `*`, `?`, and `**` wildcard subset as the CODEOWNERS adapter;
`consumerId`, `consumerKind`, and `dashboardTag` are exact and case-sensitive.
Within and across metadata files, the last matching rule wins.

Unknown fields, additional YAML documents, unsupported consumer kinds,
unbounded files, and invalid patterns produce advisory diagnostics rather than
guessed ownership.

## GitHub CODEOWNERS

When `codeowners.path` is omitted, TMR searches `.github/CODEOWNERS`,
`CODEOWNERS`, then `docs/CODEOWNERS`, and uses the first file found. This matches
[GitHub's documented location order](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/about-code-owners#codeowners-file-location).

Matching is repository-relative and case-sensitive. TMR supports `*`, `?`,
`**`, root anchoring, directory patterns, inline comments, GitHub users and
teams, email owners, multiple joint owners, empty-owner rules, and last-match
precedence. GitHub's unsupported negation, character-range, and escaped-leading
comment syntax is rejected. Invalid lines are skipped with source-line
diagnostics; they are never interpreted approximately. Files at or above the
3 MB GitHub limit are rejected.

TMR does not call GitHub to verify that a named user or team exists or has
write access. A CODEOWNERS match is therefore high-confidence repository
evidence, not identity or authorization verification.

## Grafana tags

The Grafana adapter retains a sorted JSON copy of dashboard tags in
`dashboard_tags`. Ownership discovery recognizes only non-empty values with
these exact, case-insensitive prefixes:

- `team:`
- `owner:`
- `owned-by:`

For example, `team:Checkout` selects `Checkout` if no stronger source matches.
`team:Checkout` together with `owner:Payments` creates the sorted candidates
`Checkout` and `Payments`; TMR does not guess between them.

## Failure and safety behavior

Ownership file and pattern failures are visible as `codeowners` or
`ownership_metadata` diagnostics, but those diagnostics are always advisory.
This is intentional: inability to route a finding must not be confused with
incomplete dependency evidence. The same analysis with ownership disabled,
valid, ambiguous, missing, or malformed produces the same readiness summary.

Repository ownership text is untrusted data. It is redacted before entering an
optional AI explanation packet and is never executed, used as a path supplied
by a model, or used to authorize a candidate remediation.
