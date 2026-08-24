#!/usr/bin/env bash
set -euo pipefail

e2e_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
repo_dir=$(cd "${e2e_dir}/.." && pwd)
compose=(docker compose --file "${e2e_dir}/docker-compose.yaml")
tmr_bin=${TMR_BIN:-"${TMPDIR:-/tmp}/tmr-e2e"}

cleanup() {
  if [[ -n "${TMR_SCENARIO_DIR:-}" && -n "${TMR_EXPORT_MODE:-}" ]]; then
    "${compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

if [[ -z "${TMR_BIN:-}" ]]; then
  (cd "${repo_dir}" && go build -trimpath -o "${tmr_bin}" ./cmd/tmr)
fi

run_tmr() {
  local expected=$1
  local expected_exit=$2
  local report_file
  report_file=$(mktemp)
  set +e
  (cd "${repo_dir}" && "${tmr_bin}" analyze \
    --config "${TMR_SCENARIO_DIR}/tmr.yaml" \
    --migration e2e/migrations/metric-rename.yml \
    --format json --output "${report_file}")
  local actual_exit=$?
  set -e
  if [[ "${actual_exit}" -ne "${expected_exit}" ]]; then
    printf 'TMR exit code %s, expected %s for %s\n' "${actual_exit}" "${expected_exit}" "${TMR_SCENARIO_DIR}" >&2
    return 1
  fi
  if ! grep --quiet '"status": "'"${expected}"'"' "${report_file}"; then
    printf 'TMR did not report %s for %s\n' "${expected}" "${TMR_SCENARIO_DIR}" >&2
    return 1
  fi
}

run_scenario() {
  local name=$1
  local export_mode=$2
  local expected_status=$3
  local expected_exit=$4
  local present=$5
  local absent=$6
  local runtime=$7

  export TMR_SCENARIO_DIR="${e2e_dir}/scenarios/${name}"
  export TMR_EXPORT_MODE="${export_mode}"
  export TMR_EXPECT_PRESENT="${present}"
  export TMR_EXPECT_ABSENT="${absent}"
  export TMR_EXPECT_RUNTIME="${runtime}"

  printf '\n=== %s (%s) ===\n' "${name}" "${expected_status}"
  "${compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
  "${compose[@]}" config --quiet
  "${compose[@]}" run --rm promtool check rules /scenario/prometheus/rules.yml
  "${compose[@]}" run --rm sloth validate -i /slo/checkout-slo.yml
  sloth_output=$(mktemp)
  "${compose[@]}" run --rm sloth generate -i /slo/checkout-slo.yml > "${sloth_output}"
  grep --quiet 'checkout:requests:rate1m' "${sloth_output}"

  if [[ "${expected_status}" != "BASELINE" ]]; then
    run_tmr "${expected_status}" "${expected_exit}"
  fi

  "${compose[@]}" up --detach --build exporter prometheus grafana
  "${e2e_dir}/scripts/wait-for-stack.sh"
  "${e2e_dir}/scripts/assert-prometheus.sh"
  "${compose[@]}" down --volumes --remove-orphans
}

export TMR_SCENARIO_DIR="${e2e_dir}/scenarios/01-before"
export TMR_EXPORT_MODE=old
"${compose[@]}" run --rm promtool test rules /common/rule-tests.yml

run_scenario 01-before old BASELINE 0 checkout_request_duration_seconds_count checkout_server_request_duration_seconds_count healthy
run_scenario 02-dual-write dual BLOCKED 2 checkout_request_duration_seconds_count '' healthy
run_scenario 03-partial dual BLOCKED 2 checkout_server_request_duration_seconds_count '' healthy
run_scenario 04-uncertain dual INCOMPLETE 3 checkout_server_request_duration_seconds_count '' healthy
run_scenario 05-migrated dual READY 0 checkout_server_request_duration_seconds_count '' healthy
run_scenario 06-premature-cutover new BLOCKED 2 checkout_server_request_duration_seconds_count checkout_request_duration_seconds_count broken
run_scenario 07-legacy-removed new READY 0 checkout_server_request_duration_seconds_count checkout_request_duration_seconds_count healthy

printf '\nTMR live E2E lifecycle passed.\n'
