#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT_DIR="$ROOT_DIR/benchmarks/phase16"
RUNTIME_DIR="${TMPDIR:-/tmp}/shortscale-phase16"
STATE_FILE="$RUNTIME_DIR/fixture.env"
COOKIE_JAR="$RUNTIME_DIR/cookies.txt"
BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"

cd "$ROOT_DIR"

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'required command not found: %s\n' "$1" >&2
    exit 1
  fi
}

for REQUIRED_COMMAND in curl docker python3 openssl; do
  require_command "$REQUIRED_COMMAND"
done

post_json_expect() {
  local expected_status="$1"
  local endpoint="$2"
  local payload="$3"
  local response_file
  local status

  response_file="$(mktemp "$RUNTIME_DIR/response.XXXXXX")"

  status="$(
    curl -sS \
      -o "$response_file" \
      -w '%{http_code}' \
      -b "$COOKIE_JAR" \
      -c "$COOKIE_JAR" \
      -H 'Content-Type: application/json' \
      --data-binary "$payload" \
      "$BASE_URL$endpoint"
  )"

  if [ "$status" != "$expected_status" ]; then
    printf 'request failed: %s -> HTTP %s\n' "$endpoint" "$status" >&2
    cat "$response_file" >&2
    printf '\n' >&2
    rm -f "$response_file"
    exit 1
  fi

  cat "$response_file"
  rm -f "$response_file"
}

json_short_code() {
  python3 -c 'import json,sys; print(json.load(sys.stdin)["short_code"])'
}

setup_fixture() {
  if [ -e "$STATE_FILE" ]; then
    printf 'Phase 16 fixture state already exists: %s\n' "$STATE_FILE" >&2
    printf 'Run: benchmarks/phase16/fixture.sh cleanup\n' >&2
    exit 1
  fi

  mkdir -p "$RUNTIME_DIR"
  chmod 700 "$RUNTIME_DIR"
  : > "$COOKIE_JAR"
  chmod 600 "$COOKIE_JAR"

  curl -fsS -o /dev/null "$BASE_URL/health/live"
  curl -fsS -o /dev/null "$BASE_URL/health/ready"

  local stamp
  local email
  local password
  local auth_payload
  local login_payload
  local redirect_destination
  local analytics_destination
  local async_destination
  local response
  local redirect_code
  local analytics_code
  local async_code
  local session_cookie
  local seed_output
  local raw_total
  local rollup_total
  local mismatch_total

  stamp="$(date +%s)-$$"
  email="phase16-${stamp}@example.test"
  password="P16-$(openssl rand -hex 16)"

  auth_payload="$(printf '{"email":"%s","password":"%s"}' "$email" "$password")"
  post_json_expect 201 "/api/v1/auth/register" "$auth_payload" >/dev/null

  login_payload="$auth_payload"
  post_json_expect 200 "/api/v1/auth/login" "$login_payload" >/dev/null

  session_cookie="$(
    awk '$6 == "shortscale_session" {print $7}' "$COOKIE_JAR" | tail -n 1
  )"

  if [ -z "$session_cookie" ]; then
    printf 'login succeeded but shortscale_session cookie was not captured\n' >&2
    exit 1
  fi

  redirect_destination="https://example.com/shortscale-phase16-redirect"
  analytics_destination="https://example.com/shortscale-phase16-analytics"
  async_destination="https://example.com/shortscale-phase16-async"

  response="$(
    post_json_expect \
      201 \
      "/api/v1/urls" \
      "$(printf '{"url":"%s"}' "$redirect_destination")"
  )"
  redirect_code="$(printf '%s' "$response" | json_short_code)"

  response="$(
    post_json_expect \
      201 \
      "/api/v1/urls" \
      "$(printf '{"url":"%s"}' "$analytics_destination")"
  )"
  analytics_code="$(printf '%s' "$response" | json_short_code)"

  response="$(
    post_json_expect \
      201 \
      "/api/v1/urls" \
      "$(printf '{"url":"%s"}' "$async_destination")"
  )"
  async_code="$(printf '%s' "$response" | json_short_code)"

  seed_output="$(
    docker compose exec -T postgres \
      psql \
        -X \
        -q \
        -A \
        -t \
        -U shortscale \
        -d shortscale \
        -v "code=$analytics_code" \
      < "$SCRIPT_DIR/seed-analytics.sql"
  )"

  raw_total="$(printf '%s\n' "$seed_output" | sed -n '1p')"
  rollup_total="$(printf '%s\n' "$seed_output" | sed -n '2p')"
  mismatch_total="$(printf '%s\n' "$seed_output" | sed -n '3p')"

  test "$raw_total" = "100000"
  test "$rollup_total" = "100000"
  test "$mismatch_total" = "0"

  cat > "$STATE_FILE" <<EOF
BASE_URL=$BASE_URL
EMAIL=$email
SESSION_COOKIE=$session_cookie
REDIRECT_SHORT_CODE=$redirect_code
REDIRECT_DESTINATION=$redirect_destination
ANALYTICS_SHORT_CODE=$analytics_code
ANALYTICS_DESTINATION=$analytics_destination
ASYNC_SHORT_CODE=$async_code
ASYNC_DESTINATION=$async_destination
EOF

  chmod 600 "$STATE_FILE"

  printf '\nPhase 16 fixture created.\n'
  printf 'State file:           %s\n' "$STATE_FILE"
  printf 'Redirect short code:  %s\n' "$redirect_code"
  printf 'Analytics short code: %s\n' "$analytics_code"
  printf 'Async short code:     %s\n' "$async_code"
  printf 'Synthetic raw rows:   %s\n' "$raw_total"
  printf 'Synthetic rollup:     %s\n' "$rollup_total"
  printf 'Daily mismatches:     %s\n' "$mismatch_total"
  printf '\nSession token stored only in the chmod-600 runtime state file.\n'
}

cleanup_fixture() {
  if [ ! -f "$STATE_FILE" ]; then
    printf 'No Phase 16 fixture state found; nothing to clean.\n'
    exit 0
  fi

  # shellcheck disable=SC1090
  source "$STATE_FILE"

  docker compose exec -T postgres \
    psql \
      -X \
      -q \
      -U shortscale \
      -d shortscale \
      -v "email=$EMAIL" \
      -v "redirect_code=$REDIRECT_SHORT_CODE" \
      -v "analytics_code=$ANALYTICS_SHORT_CODE" \
      -v "async_code=$ASYNC_SHORT_CODE" \
    < "$SCRIPT_DIR/cleanup-fixtures.sql"

  docker compose exec -T redis \
    redis-cli DEL \
      "shortscale:url:$REDIRECT_SHORT_CODE" \
      "shortscale:url:$ANALYTICS_SHORT_CODE" \
      "shortscale:url:$ASYNC_SHORT_CODE" \
    >/dev/null

  rm -rf "$RUNTIME_DIR"

  printf 'Phase 16 fixture cleaned.\n'
}

show_fixture() {
  if [ ! -f "$STATE_FILE" ]; then
    printf 'No Phase 16 fixture state found.\n' >&2
    exit 1
  fi

  # shellcheck disable=SC1090
  source "$STATE_FILE"

  printf 'State file:           %s\n' "$STATE_FILE"
  printf 'Base URL:             %s\n' "$BASE_URL"
  printf 'Email:                %s\n' "$EMAIL"
  printf 'Redirect short code:  %s\n' "$REDIRECT_SHORT_CODE"
  printf 'Analytics short code: %s\n' "$ANALYTICS_SHORT_CODE"
  printf 'Async short code:     %s\n' "$ASYNC_SHORT_CODE"
  printf 'Session cookie:       [stored, not printed]\n'
}

case "${1:-}" in
  setup)
    setup_fixture
    ;;
  cleanup)
    cleanup_fixture
    ;;
  show)
    show_fixture
    ;;
  *)
    printf 'usage: %s {setup|show|cleanup}\n' "$0" >&2
    exit 2
    ;;
esac
