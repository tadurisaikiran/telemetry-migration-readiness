# Security Policy

## Supported versions

Until the first stable release, security fixes are applied to the latest commit
on `main`. After releases begin, this table will list supported versions.

| Version | Supported |
| --- | --- |
| `main` / pre-release | Yes |

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability. Use GitHub's private
vulnerability reporting feature on the repository Security tab. Include:

- affected version or commit;
- impact and threat scenario;
- minimal reproduction;
- any suggested mitigation;
- whether disclosure is time-sensitive.

You should receive an acknowledgement within three business days and an
initial assessment within seven business days. Timelines may change with
severity and maintainer availability, but reporters will receive status
updates.

## Security boundaries

TMR treats repository and telemetry artifacts as untrusted input. The local
core does not execute expressions it analyzes, does not require credentials,
and does not contact external services unless a remote adapter is explicitly
configured. Optional AI output cannot alter deterministic readiness.

Please report path traversal, resource exhaustion, credential exposure,
prompt-injection boundary failures, unsafe patch application, and any path that
can incorrectly turn incomplete evidence into `READY` as security issues.
