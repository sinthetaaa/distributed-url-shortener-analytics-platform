#!/usr/bin/env bash

set -euo pipefail

: "${GITHUB_TOKEN:?GITHUB_TOKEN is required}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
: "${DEPLOY_SHA:?DEPLOY_SHA is required}"

MAX_ATTEMPTS="${DEPLOY_STATUS_MAX_ATTEMPTS:-90}"
SLEEP_SECONDS="${DEPLOY_STATUS_SLEEP_SECONDS:-10}"

GITHUB_API_ORIGIN="$(printf '%s%s' 'https:' '//api.github.com')"

EXPECTED_CONTEXTS=(
  'celebrated-flow - shortscale-api'
  'celebrated-flow - shortscale-analytics-consumer'
  'celebrated-flow - shortscale-observability'
  'Vercel'
)

printf 'Waiting for platform deployments for commit %s\n' "$DEPLOY_SHA"

for ATTEMPT in $(seq 1 "$MAX_ATTEMPTS")
do
  STATUS_JSON="$(
    curl \
      --fail-with-body \
      --silent \
      --show-error \
      -H "Authorization: Bearer $GITHUB_TOKEN" \
      -H 'Accept: application/vnd.github+json' \
      -H 'X-GitHub-Api-Version: 2022-11-28' \
      "$GITHUB_API_ORIGIN/repos/$GITHUB_REPOSITORY/commits/$DEPLOY_SHA/status"
  )"

  ALL_SUCCESS=1

  printf '\nAttempt %s/%s\n' "$ATTEMPT" "$MAX_ATTEMPTS"

  for CONTEXT in "${EXPECTED_CONTEXTS[@]}"
  do
    STATE="$(
      printf '%s' "$STATUS_JSON" \
        | jq -r \
            --arg context "$CONTEXT" \
            '
              [
                .statuses[]
                | select(.context == $context)
              ][0].state // "missing"
            '
    )"

    printf '  %-55s %s\n' "$CONTEXT" "$STATE"

    case "$STATE" in
      success)
        ;;

      failure|error)
        printf '\nDeployment context failed: %s (%s)\n' \
          "$CONTEXT" \
          "$STATE" \
          >&2

        exit 1
        ;;

      pending|expected|missing)
        ALL_SUCCESS=0
        ;;

      *)
        printf '\nUnexpected deployment state: %s -> %s\n' \
          "$CONTEXT" \
          "$STATE" \
          >&2

        exit 1
        ;;
    esac
  done

  if [ "$ALL_SUCCESS" -eq 1 ]; then
    printf '\nAll required platform deployments succeeded.\n'
    exit 0
  fi

  if [ "$ATTEMPT" -lt "$MAX_ATTEMPTS" ]; then
    sleep "$SLEEP_SECONDS"
  fi
done

printf '\nTimed out waiting for required platform deployments.\n' >&2
exit 1
