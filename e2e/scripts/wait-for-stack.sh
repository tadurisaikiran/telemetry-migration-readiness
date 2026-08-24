#!/usr/bin/env bash
set -euo pipefail

wait_for() {
  local name=$1
  local url=$2
  for _ in $(seq 1 60); do
    if curl --fail --silent --show-error "${url}" >/dev/null; then
      return 0
    fi
    sleep 1
  done
  printf 'Timed out waiting for %s at %s\n' "${name}" "${url}" >&2
  return 1
}

wait_for Prometheus http://127.0.0.1:19090/-/ready
wait_for Grafana http://127.0.0.1:13000/api/health

for _ in $(seq 1 30); do
  response=$(curl --fail --silent --show-error \
    --get --data-urlencode 'query=up{job="checkout"} == 1' \
    http://127.0.0.1:19090/api/v1/query)
  if [[ "${response}" != *'"result":[]'* ]]; then
    exit 0
  fi
  sleep 1
done

printf 'Exporter never became queryable through Prometheus\n' >&2
exit 1
