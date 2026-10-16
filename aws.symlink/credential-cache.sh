#!/usr/bin/env bash
# Keeps short-lived AWS credentials in ~/.aws/cache for non-interactive callers.
#
# One daemon per profile owns the cache. It generates a random AES-256-GCM key
# at startup and keeps it only in memory, refreshes the credentials from the
# real profile (1Password) before they expire, and answers `print` over a Unix
# socket by decrypting the file on demand. When the daemon exits it deletes the
# cache, and even a leftover copy of <profile>.json is useless without the key.
#
# activate  starts the daemon for <profile> if none is running, or registers
#           another watched pid with the running one. Credentials are only
#           fetched by the daemon, so a second activate never rewrites them.
#           Prints the environment the caller should adopt.
# print     asks the daemon for the plaintext credentials. The generated
#           <profile>_cached config points credential_process here.
# refresh   asks the running daemon to refresh now.
# serve     the daemon entrypoint; activate starts it detached.
#
# From a shell:
#   eval "$(~/.aws/credential-cache.sh activate work_development $$)"
# From a program (parse the JSON, apply "env", delete "unset"):
#   ~/.aws/credential-cache.sh activate work_development <pid> --json

set -euo pipefail

CACHE_DIR="${HOME}/.aws/cache"
ACTIVATE_TIMEOUT_SECONDS="${AWS_CREDENTIAL_CACHE_TIMEOUT:-120}"
SCRIPT_PATH=$(realpath "$0")
STATIC_CREDENTIAL_VARS=(AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_CREDENTIAL_EXPIRATION)

usage() {
  cat >&2 <<EOF
Usage:
  $0 activate <profile> [pid] [--sh|--json]
  $0 print <profile>
  $0 refresh <profile>
  $0 serve <profile> <pid>

activate starts a background daemon for <profile> (or joins the running one),
watches <pid> (default: the calling process), and prints the environment to
adopt. --sh (default) prints shell statements for eval; --json prints
{"env": {...}, "unset": [...]}.
EOF
  exit 1
}

validate_profile() {
  local profile="$1"
  if [[ ! "$profile" =~ ^[A-Za-z0-9_+=,.@-]+$ ]]; then
    echo "Invalid AWS profile name: ${profile}" >&2
    exit 1
  fi
}

cache_path() {
  printf '%s/%s.%s\n' "$CACHE_DIR" "$1" "$2"
}

ensure_cache_dir() {
  mkdir -p "$CACHE_DIR"
  chmod 700 "$CACHE_DIR"
}

quote_sh() {
  local escaped
  escaped=$(printf '%s' "$1" | sed "s/'/'\\\\''/g")
  printf "'%s'" "$escaped"
}

pid_is_alive() {
  local pid="$1"
  [[ "$pid" =~ ^[1-9][0-9]*$ ]] && kill -0 "$pid" 2>/dev/null
}

# The daemon and the socket client live in credential-cache.js next to this file.
NODE_SCRIPT="$(dirname "$SCRIPT_PATH")/credential-cache.js"

run_node() {
  CREDENTIAL_CACHE_SCRIPT="$SCRIPT_PATH" node "$NODE_SCRIPT" "$@"
}

start_daemon() {
  local profile="$1"
  local watch_pid="$2"
  # Double fork so the daemon is not a job of the caller's shell and survives it.
  ( "$SCRIPT_PATH" serve "$profile" "$watch_pid" </dev/null >/dev/null 2>&1 & ) &
  wait $! 2>/dev/null || true
}

report_daemon_failure() {
  local profile="$1"
  local log_file
  log_file=$(cache_path "$profile" log)
  echo "AWS credential cache daemon for profile \"${profile}\" did not start." >&2
  if [[ -f "$log_file" ]]; then
    echo "Recent log (${log_file}):" >&2
    tail -n 8 "$log_file" >&2
  fi
  return 1
}

# Poll until the daemon answers ping. Prints the region on success.
wait_for_daemon() {
  local profile="$1"
  local daemon_pid_file error_file region saw_daemon=0 daemon_pid=""
  local deadline=$((SECONDS + ACTIVATE_TIMEOUT_SECONDS))
  daemon_pid_file=$(cache_path "$profile" daemon-pid)
  error_file=$(cache_path "$profile" error)
  while true; do
    if region=$(run_node request "$profile" ping 2>/dev/null) && [[ -n "$region" ]]; then
      printf '%s\n' "$region"
      return 0
    fi
    if [[ -f "$error_file" ]]; then
      echo "AWS credential cache daemon for profile \"${profile}\" failed to start:" >&2
      cat "$error_file" >&2
      rm -f "$error_file"
      return 1
    fi
    if [[ -f "$daemon_pid_file" ]]; then
      daemon_pid=$(cat "$daemon_pid_file" 2>/dev/null || true)
      if pid_is_alive "$daemon_pid"; then
        saw_daemon=1
      elif [[ "$saw_daemon" == 1 ]]; then
        report_daemon_failure "$profile"
        return 1
      fi
    elif [[ "$saw_daemon" == 1 ]]; then
      report_daemon_failure "$profile"
      return 1
    fi
    if [[ "$SECONDS" -ge "$deadline" ]]; then
      echo "Timed out after ${ACTIVATE_TIMEOUT_SECONDS}s waiting for the credential cache daemon for profile \"${profile}\"." >&2
      report_daemon_failure "$profile" || true
      return 1
    fi
    sleep 0.2
  done
}

print_env_sh() {
  local region="$1" config_file="$2" cached_profile="$3"
  local var
  for var in "${STATIC_CREDENTIAL_VARS[@]}"; do
    printf 'unset %s\n' "$var"
  done
  printf 'export AWS_REGION=%s\n' "$(quote_sh "$region")"
  printf 'export AWS_CONFIG_FILE=%s\n' "$(quote_sh "$config_file")"
  printf 'export AWS_PROFILE=%s\n' "$(quote_sh "$cached_profile")"
}

print_env_json() {
  local region="$1" config_file="$2" cached_profile="$3"
  jq -c -n \
    --arg region "$region" \
    --arg config "$config_file" \
    --arg profile "$cached_profile" \
    --args '{
      env: { AWS_REGION: $region, AWS_CONFIG_FILE: $config, AWS_PROFILE: $profile },
      unset: $ARGS.positional
    }' "${STATIC_CREDENTIAL_VARS[@]}"
}

activate_profile() {
  local profile="$1"
  local watch_pid="${2:-}"
  local format="${3:-sh}"
  local region config_file

  validate_profile "$profile"
  if [[ -z "$watch_pid" ]]; then
    watch_pid="$PPID"
  fi
  if ! pid_is_alive "$watch_pid"; then
    echo "Refusing to activate profile \"${profile}\" for dead pid ${watch_pid}." >&2
    exit 1
  fi

  ensure_cache_dir
  rm -f "$(cache_path "$profile" error)"
  start_daemon "$profile" "$watch_pid"
  region=$(wait_for_daemon "$profile") || exit 1
  config_file=$(cache_path "$profile" config)

  case "$format" in
    sh) print_env_sh "$region" "$config_file" "${profile}_cached" ;;
    json) print_env_json "$region" "$config_file" "${profile}_cached" ;;
  esac
}

print_profile() {
  local profile="$1"
  validate_profile "$profile"
  if ! run_node request "$profile" print; then
    echo "AWS credential cache daemon is not running for profile \"${profile}\"; run \"$0 activate ${profile}\" first." >&2
    exit 1
  fi
}

refresh_profile() {
  local profile="$1"
  validate_profile "$profile"
  if ! run_node request "$profile" refresh; then
    echo "AWS credential cache daemon is not running for profile \"${profile}\"; run \"$0 activate ${profile}\" first." >&2
    exit 1
  fi
}

command="${1:-}"
if [[ $# -gt 0 ]]; then
  shift
fi

case "$command" in
  activate)
    activate_profile_arg=""
    activate_pid_arg=""
    activate_format="sh"
    for arg in "$@"; do
      case "$arg" in
        --sh) activate_format="sh" ;;
        --json) activate_format="json" ;;
        --*) usage ;;
        *)
          if [[ -z "$activate_profile_arg" ]]; then
            activate_profile_arg="$arg"
          elif [[ -z "$activate_pid_arg" ]]; then
            activate_pid_arg="$arg"
          else
            usage
          fi
          ;;
      esac
    done
    [[ -n "$activate_profile_arg" ]] || usage
    activate_profile "$activate_profile_arg" "$activate_pid_arg" "$activate_format"
    ;;
  print)
    [[ $# -eq 1 ]] || usage
    print_profile "$1"
    ;;
  refresh)
    [[ $# -eq 1 ]] || usage
    refresh_profile "$1"
    ;;
  serve)
    [[ $# -eq 2 ]] || usage
    validate_profile "$1"
    CREDENTIAL_CACHE_SCRIPT="$SCRIPT_PATH" exec node "$NODE_SCRIPT" daemon "$1" "$2"
    ;;
  *)
    usage
    ;;
esac
