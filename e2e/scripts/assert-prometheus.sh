#!/usr/bin/env bash
set -euo pipefail

query_result() {
  curl --fail --silent --show-error \
    --get --data-urlencode "query=$1" \
    http://127.0.0.1:19090/api/v1/query
}

assert_present() {
  local query=$1
  local response
  for _ in $(seq 1 30); do
    response=$(query_result "${query}")
    if [[ "${response}" != *'"result":[]'* ]]; then
      return 0
    fi
    sleep 1
  done
  printf 'Expected Prometheus data for query: %s\n%s\n' "${query}" "${response}" >&2
  return 1
}

assert_absent() {
  local query=$1
  local response
  response=$(query_result "${query}")
  if [[ "${response}" != *'"result":[]'* ]]; then
    printf 'Expected no Prometheus data for query: %s\n%s\n' "${query}" "${response}" >&2
    return 1
  fi
}

assert_present 'up{job="checkout"} == 1'
assert_present "${TMR_EXPECT_PRESENT:?TMR_EXPECT_PRESENT is required}"

if [[ -n "${TMR_EXPECT_ABSENT:-}" ]]; then
  assert_absent "${TMR_EXPECT_ABSENT}"
fi

if [[ "${TMR_EXPECT_RUNTIME:-healthy}" == "healthy" ]]; then
  assert_present 'checkout:p95_latency'
  assert_present 'checkout:requests:rate1m'
else
  assert_absent 'checkout:p95_latency'
  assert_absent 'checkout:requests:rate1m'
fi

grafana_health=$(curl --fail --silent --show-error http://127.0.0.1:13000/api/health)
[[ "${grafana_health}" == *'"database": "ok"'* || "${grafana_health}" == *'"database":"ok"'* ]]
dashboard_search=$(curl --fail --silent --show-error 'http://127.0.0.1:13000/api/search?query=Checkout')
[[ "${dashboard_search}" == *'checkout-live'* ]]
