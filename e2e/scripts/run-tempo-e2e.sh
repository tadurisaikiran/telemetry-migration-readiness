#!/usr/bin/env bash
set -euo pipefail

e2e_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
repo_dir=$(cd "${e2e_dir}/.." && pwd)
tempo_image='grafana/tempo:2.10.5@sha256:ee21727732c7a7199cb71c3eee9153bbf23f9b0b87619f0555a0cf21a67f1a33'
container_name=tmr-tempo-e2e
temporary_dir=$(mktemp -d "${TMPDIR:-/tmp}/tmr-tempo-e2e.XXXXXX")
tmr_bin=${TMR_BIN:-"${temporary_dir}/tmr"}
container_started=false

cleanup() {
  if [[ "${container_started}" == true ]]; then
    docker stop "${container_name}" >/dev/null 2>&1 || true
  fi
  rm -rf -- "${temporary_dir}"
}
trap cleanup EXIT

if docker container inspect "${container_name}" >/dev/null 2>&1; then
  printf 'Refusing to replace existing Docker container %s.\n' "${container_name}" >&2
  exit 1
fi

if [[ -z "${TMR_BIN:-}" ]]; then
  (cd "${repo_dir}" && go build -trimpath -o "${tmr_bin}" ./cmd/tmr)
fi

docker run --rm --detach \
  --name "${container_name}" \
  --publish 127.0.0.1:13200:3200 \
  --volume "${e2e_dir}/tempo/tempo.yaml:/etc/tempo.yaml:ro" \
  "${tempo_image}" \
  -config.file=/etc/tempo.yaml \
  -reporting.enabled=false >/dev/null
container_started=true

for _ in {1..60}; do
  if curl --fail --silent --show-error http://127.0.0.1:13200/ready >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! curl --fail --silent --show-error http://127.0.0.1:13200/ready >/dev/null; then
  docker logs "${container_name}" >&2
  printf 'Tempo did not become ready within 60 seconds.\n' >&2
  exit 1
fi

run_analysis() {
  local scenario=$1
  local expected_status=$2
  local expected_exit=$3
  local report_file="${temporary_dir}/${scenario}.json"

  set +e
  (cd "${repo_dir}" && "${tmr_bin}" analyze \
    --config "e2e/tempo/tmr-${scenario}.yaml" \
    --migration e2e/tempo/migration.yaml \
    --format json \
    --output "${report_file}")
  local actual_exit=$?
  set -e

  if [[ "${actual_exit}" -ne "${expected_exit}" ]]; then
    printf 'Tempo scenario %s exited %s, expected %s.\n' \
      "${scenario}" "${actual_exit}" "${expected_exit}" >&2
    return 1
  fi
  if ! grep --quiet '"status": "'"${expected_status}"'"' "${report_file}"; then
    printf 'Tempo scenario %s did not report %s.\n' "${scenario}" "${expected_status}" >&2
    return 1
  fi
}

run_analysis legacy BLOCKED 2
run_analysis migrated READY 0
run_analysis invalid INCOMPLETE 3

printf '\nTMR pinned Tempo TraceQL lifecycle passed.\n'
