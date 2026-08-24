#!/usr/bin/env bash
set -euo pipefail

report_path="${RUNNER_TEMP}/tmr-report.md"
set +e
"${RUNNER_TEMP}/tmr" analyze \
  --config "${TMR_CONFIG}" \
  --migration "${TMR_MIGRATION}" \
  --format markdown \
  --output "${report_path}"
exit_code=$?
set -e

if [[ ! -s "${report_path}" ]]; then
  printf '# Telemetry Migration Readiness\n\n**Status:** **ERROR**\n' > "${report_path}"
fi

status="ERROR"
case "${exit_code}" in
  0) status="READY" ;;
  2) status="BLOCKED" ;;
  3) status="INCOMPLETE" ;;
esac

cat "${report_path}" >> "${GITHUB_STEP_SUMMARY}"
{
  printf 'status=%s\n' "${status}"
  printf 'exit-code=%s\n' "${exit_code}"
  printf 'report=%s\n' "${report_path}"
} >> "${GITHUB_OUTPUT}"
