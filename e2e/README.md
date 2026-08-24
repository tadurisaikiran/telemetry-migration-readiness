# Live End-to-End Harness

This harness experimentally compares TMR predictions with a running telemetry
stack. It uses a controlled Go exporter, Prometheus, provisioned Grafana, and
Sloth. The normal suite pins:

| Component | Version |
| --- | --- |
| Go builder | 1.27.0 |
| Prometheus / promtool | 3.13.2 LTS |
| Grafana | 13.1.3 |
| Sloth | 0.16.0 |

Run it from the repository root with Docker Compose v2:

```bash
./e2e/scripts/run-e2e.sh
```

The script builds `tmr`, independently runs `promtool check rules` for every
scenario, runs `promtool test rules`, validates and generates the Sloth spec,
then starts and queries the stack for each lifecycle stage.
For every migration stage it also requires the explicit migration manifest and
the mapped Weaver V2 diff to produce the same status and exit code.

| Scenario | Exporter | Consumers | Expected TMR | Runtime |
| --- | --- | --- | --- | --- |
| `01-before` | old | old | baseline | healthy |
| `02-dual-write` | old + new | old | `BLOCKED` | healthy |
| `03-partial` | old + new | mixed | `BLOCKED` | healthy |
| `04-uncertain` | old + new | migrated + dynamic critical query | `INCOMPLETE` | healthy |
| `05-migrated` | old + new | new | `READY` | healthy |
| `06-premature-cutover` | new | old | `BLOCKED` | predicted recordings absent |
| `07-legacy-removed` | new | new | `READY` | critical recordings present |

The last two stages prove both directions: TMR predicts an observable failure
before an unsafe cutover and predicts readiness before a successful cutover.
Each stack uses a fresh Prometheus data volume, so an old time series cannot
hide a broken scenario.

Pinned E2E runs on every pull request. The scheduled compatibility workflow
tests the previous supported versions and floating upstream latest tags without
making normal CI non-reproducible.
