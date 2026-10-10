#!/usr/bin/env bash

set -euo pipefail

API_ORIGIN="$(
  printf '%s%s' \
    'https:' \
    '//shortscale-api-production.up.railway.app'
)"

FRONTEND_ORIGIN="$(
  printf '%s%s' \
    'https:' \
    '//sscale.vercel.app'
)"

MAX_ATTEMPTS="${PRODUCTION_SMOKE_MAX_ATTEMPTS:-12}"
SLEEP_SECONDS="${PRODUCTION_SMOKE_SLEEP_SECONDS:-5}"

wait_for_status() {
  LABEL="$1"
  EXPECTED_STATUS="$2"
  URL="$3"

  for ATTEMPT in $(seq 1 "$MAX_ATTEMPTS")
  do
    STATUS="$(
      curl \
        --silent \
        --show-error \
        --connect-timeout 5 \
        --max-time 10 \
        -o /dev/null \
        -w '%{http_code}' \
        "$URL" \
        2>/dev/null \
        || true
    )"

    printf '%-24s attempt %-2s -> HTTP %s\n' \
      "$LABEL" \
      "$ATTEMPT" \
      "$STATUS"

    if [ "$STATUS" = "$EXPECTED_STATUS" ]; then
      return 0
    fi

    if [ "$ATTEMPT" -lt "$MAX_ATTEMPTS" ]; then
      sleep "$SLEEP_SECONDS"
    fi
  done

  printf '%s expected HTTP %s but never became healthy.\n' \
    "$LABEL" \
    "$EXPECTED_STATUS" \
    >&2

  return 1
}

printf '\n===== SHORTSCALE PRODUCTION SMOKE =====\n\n'

wait_for_status \
  'API live' \
  '200' \
  "$API_ORIGIN/health/live"

wait_for_status \
  'API ready' \
  '200' \
  "$API_ORIGIN/health/ready"

wait_for_status \
  'Frontend root' \
  '200' \
  "$FRONTEND_ORIGIN/"

wait_for_status \
  'Public metrics closed' \
  '404' \
  "$API_ORIGIN/metrics"

wait_for_status \
  'Unauthenticated auth/me' \
  '401' \
  "$FRONTEND_ORIGIN/api/v1/auth/me"

printf '\nProduction smoke checks passed.\n'
