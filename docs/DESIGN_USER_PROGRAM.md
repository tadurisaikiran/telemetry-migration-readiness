# Design User Program

The first design-user cohort should validate whether TMR gives observability
engineers useful, trustworthy evidence during real telemetry migrations. It is
not a launch list, sales funnel, or request for endorsements.

The target is 5–10 teams. Each engagement should produce a sanitized,
reproducible finding or a documented successful workflow.

## Participant profile

A strong design user owns or regularly changes at least two of these:

- Prometheus metrics and recording rules;
- Grafana dashboards;
- Prometheus alerting rules;
- Sloth or Pyrra SLO definitions;
- OpenTelemetry instrumentation or Collector configuration.

Prefer teams that have a metric or label migration planned, in progress, or
recently completed. A large installation is not required, but the evaluation
should include multiple consumer types or a transitive recording-rule
dependency.

Do not recruit a participant solely because they can provide a testimonial.
The useful participant is willing to test an uncertain workflow and report
where TMR is wrong, incomplete, slow, or confusing.

## Qualification screen

Use these questions before scheduling an evaluation:

1. Which telemetry contract is changing: metric name, label name, or removal?
2. Which consumer types may depend on it?
3. Are the relevant artifacts available as sanitized local files?
4. Is there a known expected outcome for at least one migration state?
5. Can the participant spend 45 minutes running a local CLI and discussing the
   result?
6. Are they able to share a minimal reproduction if the result is wrong?

If the artifacts cannot leave the participant's environment, ask them to share
only aggregate counts, permanent diagnostic codes, and a newly constructed
synthetic reproduction.

## Safety and privacy

TMR is local-first, but its input files can still contain sensitive names,
queries, labels, repository paths, or operational thresholds.

- Never request credentials, tokens, customer data, production endpoints, or
  unredacted telemetry.
- Never ask a participant to upload proprietary artifacts to a public issue.
- Use synthetic names in shared fixtures.
- Remove organization, service, owner, URL, path, namespace, and dashboard UID
  identifiers when they are not necessary to reproduce behavior.
- Report security concerns through GitHub private vulnerability reporting.
- Do not publish a company name, quotation, logo, or case study without a
  separate explicit approval from an authorized representative.

## The 45-minute evaluation

### 1. Establish the expected result — 5 minutes

Record the telemetry change, consumer types, critical consumers, and the
participant's expected `READY`, `BLOCKED`, or `INCOMPLETE` result before running
TMR. This prevents the tool output from rewriting the expectation after the
fact.

### 2. Prepare sanitized inputs — 10 minutes

Create a migration manifest and a local configuration referencing only the
needed fixture files:

```bash
tmr validate --migration migration.yaml
```

Validation errors are useful design feedback. Capture the error and permanent
diagnostic code rather than working around it silently.

### 3. Analyze — 10 minutes

Run both human and machine-readable output:

```bash
tmr analyze --config tmr.yaml --migration migration.yaml
tmr analyze --config tmr.yaml --migration migration.yaml \
  --format json --output tmr-report.json
```

Record the process exit code. The permanent contract is:

| Exit | Meaning |
| --- | --- |
| `0` | Policy passed; the migration is ready. |
| `1` | Tool, configuration, adapter, or runtime error. |
| `2` | The migration is blocked. |
| `3` | Required evidence is incomplete. |

### 4. Inspect one dependency chain — 10 minutes

Select the most critical or surprising result:

```bash
tmr explain --config tmr.yaml --migration migration.yaml \
  --symbol checkout_request_duration_seconds
tmr graph --config tmr.yaml --migration migration.yaml \
  --output graph.json
```

Check whether the consumer, source location, evidence, classification, and
transitive path are correct.

### 5. Debrief — 10 minutes

Ask:

- Did TMR match the expected final decision?
- Did it miss a critical consumer or report a false dependency?
- Was any `INCOMPLETE` result actionable?
- Could the participant identify the next file to change?
- Which input or output required explanation?
- Would they put this check in a pull request today? Why or why not?

## Required finding record

Create one record per evaluation, even when no bug is found:

```yaml
session_id: anonymous-01
tmr_version: commit-or-release
change_kind: metric_rename
consumer_types:
  - grafana
  - prometheus_rule
expected_status: BLOCKED
actual_status: BLOCKED
exit_code: 2
critical_consumers_expected: 3
critical_consumers_found: 3
false_positives: 0
false_negatives: 0
unresolved_critical: 0
time_to_first_result_minutes: 8
reproducible_fixture: true
follow_up_issue: issue-number-or-none
```

Do not put participant names or confidential organization details in this
record. Store contact information separately under the maintainer's control.

## Triage order

Handle findings in this order:

1. Possible false `READY` result or missed critical dependency.
2. Parser, adapter, or evidence failure incorrectly interpreted as absence.
3. Incorrect `BLOCKED` or `INCOMPLETE` result.
4. Source-location or transitive-path errors.
5. Installation, documentation, and workflow friction.
6. Feature requests outside the current supported contract.

Every correctness fix needs a sanitized regression fixture and a test at the
lowest useful layer. Critical readiness changes also require integration or
live E2E coverage.

## Direct outreach draft

The maintainer should edit and send this personally. Do not automate it or send
it from a project bot.

> I am testing an open-source, local CLI for one specific observability problem:
> proving whether dashboards, Prometheus rules, and SLOs are ready before an old
> metric or label is removed. I am looking for engineers with a real migration
> who are willing to spend 45 minutes trying it and showing me where the result
> is wrong or hard to use. You would not need to share proprietary files; a
> sanitized or synthetic reproduction is enough. Would this be relevant to a
> migration your team is handling?

Follow up once, after a reasonable interval, with a short note that makes it
easy to decline. Do not add people to a mailing list.

## Follow-up draft

> Thank you for testing the migration workflow. I recorded the expected and
> actual readiness result, the dependency evidence, and the friction you found.
> I will link the sanitized issue or fix when it is public. I will not identify
> you or your organization in project material unless we separately agree on
> the exact wording.

## Cohort success criteria

The initial cohort is complete when:

- 5–10 qualified participants have run the evaluation;
- every session has an expected-versus-actual record;
- every possible safety false negative has a public sanitized regression or a
  private security report;
- recurring onboarding friction has been fixed or explicitly documented;
- at least one migration exercises a transitive recording-rule dependency;
- at least one critical unknown produces `INCOMPLETE` rather than false safety;
- no public result identifies a participant without explicit consent.

Use the design-user feedback issue form only for sanitized, public findings.
