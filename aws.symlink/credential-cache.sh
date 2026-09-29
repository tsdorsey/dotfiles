#!/usr/bin/env bash
# Keeps short-lived AWS credentials in ~/.aws/cache for non-interactive callers.
#
# activate  refreshes the cache, starts the background serve daemon watching the
#           caller, and prints the environment the caller should adopt. This is
#           the one command another application needs.
# refresh   talks to the real AWS profile (1Password) and writes the cache.
# print     only reads the cache; the generated <profile>_cached config points
#           credential_process here.
# serve     refreshes before expiration and deletes the cache when every watched
#           pid is gone.
#
# From a shell:
#   eval "$(~/.aws/credential-cache.sh activate work_development $$)"
# From a program (parse the JSON, apply "env", delete "unset"):
#   ~/.aws/credential-cache.sh activate work_development <pid> --json

set -euo pipefail

CACHE_DIR="${HOME}/.aws/cache"
POLL_SECONDS=5
REFRESH_WINDOW_SECONDS=600
REFRESH_RETRY_SECONDS=60
SCRIPT_PATH=$(realpath "$0")
STATIC_CREDENTIAL_VARS=(AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_CREDENTIAL_EXPIRATION)

usage() {
  cat >&2 <<EOF
Usage:
  $0 activate <profile> [pid] [--sh|--json]
  $0 refresh <profile>
  $0 print <profile>
  $0 serve <profile> <pid>

activate refreshes the cache for <profile>, starts a background daemon that
keeps it fresh while <pid> (default: the calling process) is alive, and prints
the environment to adopt. --sh (default) prints shell statements for eval;
--json prints {"env": {...}, "unset": [...]}.
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

cache_json_path() {
  printf '%s/%s.json\n' "$CACHE_DIR" "$1"
}

cache_config_path() {
  printf '%s/%s.config\n' "$CACHE_DIR" "$1"
}

cache_pid_path() {
  printf '%s/%s.pids\n' "$CACHE_DIR" "$1"
}

cache_daemon_pid_path() {
  printf '%s/%s.daemon-pid\n' "$CACHE_DIR" "$1"
}

cache_log_path() {
  printf '%s/%s.log\n' "$CACHE_DIR" "$1"
}

cache_lock_path() {
  printf '%s/%s.lock\n' "$CACHE_DIR" "$1"
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

atomic_write() {
  local dest="$1"
  local content="$2"
  local tmp
  tmp=$(mktemp "${CACHE_DIR}/.tmp.XXXXXX")
  chmod 600 "$tmp"
  printf '%s\n' "$content" > "$tmp"
  mv -f "$tmp" "$dest"
  chmod 600 "$dest"
}

run_aws() {
  env \
    -u AWS_PROFILE \
    -u AWS_CONFIG_FILE \
    -u AWS_ACCESS_KEY_ID \
    -u AWS_SECRET_ACCESS_KEY \
    -u AWS_SESSION_TOKEN \
    -u AWS_CREDENTIAL_EXPIRATION \
    AWS_SDK_LOAD_CONFIG=1 \
    aws "$@"
}

refresh_profile() {
  local profile="$1"
  local json region canonical config_body
  validate_profile "$profile"
  ensure_cache_dir
  json=$(run_aws configure export-credentials --profile "$profile" --format process) || {
    echo "Failed to export credentials for AWS profile \"${profile}\"." >&2
    echo "Ensure the AWS CLI is installed and \"aws sts get-caller-identity --profile ${profile}\" works." >&2
    return 1
  }
  region=$(run_aws configure get region --profile "$profile" | tr -d '[:space:]') || return 1
  if [[ -z "$region" ]]; then
    echo "Failed to get AWS region for profile \"${profile}\"." >&2
    return 1
  fi
  canonical=$(printf '%s' "$json" | jq -c -e 'select(.Version == 1 and .AccessKeyId != null and .SecretAccessKey != null)') || {
    echo "Failed to parse AWS credentials exported for profile \"${profile}\"." >&2
    return 1
  }
  atomic_write "$(cache_json_path "$profile")" "$canonical" || return 1
  config_body=$(printf '[profile %s_cached]\nregion = %s\ncredential_process = %s print %s\n' \
    "$profile" \
    "$region" \
    "$(quote_sh "$SCRIPT_PATH")" \
    "$(quote_sh "$profile")") || return 1
  atomic_write "$(cache_config_path "$profile")" "$config_body" || return 1
}

print_profile() {
  local profile="$1"
  local cache_json
  validate_profile "$profile"
  cache_json=$(cache_json_path "$profile")
  if [[ ! -f "$cache_json" ]]; then
    echo "AWS credential cache is missing for profile \"${profile}\"." >&2
    exit 1
  fi
  cat "$cache_json"
}

cached_region() {
  local config_file="$1"
  local region
  region=$(sed -n 's/^region[[:space:]]*=[[:space:]]*\([^[:space:]]*\)[[:space:]]*$/\1/p' "$config_file" | head -n 1)
  if [[ -z "$region" ]]; then
    echo "AWS region missing from credential cache config ${config_file}" >&2
    return 1
  fi
  printf '%s\n' "$region"
}

start_daemon() {
  local profile="$1"
  local watch_pid="$2"
  # Double fork so the daemon is not a job of the caller's shell and survives it.
  ( "$SCRIPT_PATH" serve "$profile" "$watch_pid" </dev/null >/dev/null 2>&1 & ) &
  wait $! 2>/dev/null || true
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
  local config_file region

  validate_profile "$profile"
  if [[ -z "$watch_pid" ]]; then
    watch_pid="$PPID"
  fi
  if ! pid_is_alive "$watch_pid"; then
    echo "Refusing to activate profile \"${profile}\" for dead pid ${watch_pid}." >&2
    exit 1
  fi

  refresh_profile "$profile" || exit 1
  config_file=$(cache_config_path "$profile")
  region=$(cached_region "$config_file") || exit 1
  start_daemon "$profile" "$watch_pid"

  case "$format" in
    sh) print_env_sh "$region" "$config_file" "${profile}_cached" ;;
    json) print_env_json "$region" "$config_file" "${profile}_cached" ;;
  esac
}

log_line() {
  local profile="$1"
  shift
  ensure_cache_dir
  printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >> "$(cache_log_path "$profile")"
  chmod 600 "$(cache_log_path "$profile")"
}

acquire_lock() {
  local lockdir="$1"
  local tries=0
  local owner=""
  while true; do
    if mkdir "$lockdir" 2>/dev/null; then
      printf '%s\n' "$$" > "${lockdir}/pid"
      chmod 600 "${lockdir}/pid"
      return 0
    fi
    owner=""
    if [[ -f "${lockdir}/pid" ]]; then
      owner=$(cat "${lockdir}/pid" 2>/dev/null || true)
    fi
    if [[ -z "$owner" ]] || ! kill -0 "$owner" 2>/dev/null; then
      rm -rf "$lockdir"
      continue
    fi
    tries=$((tries + 1))
    if [[ "$tries" -gt 100 ]]; then
      echo "Timed out waiting for credential cache lock ${lockdir}" >&2
      return 1
    fi
    sleep 0.1
  done
}

release_lock() {
  rm -rf "$1"
}

pid_is_alive() {
  local pid="$1"
  [[ "$pid" =~ ^[1-9][0-9]*$ ]] && kill -0 "$pid" 2>/dev/null
}

list_live_pids() {
  local pid_file="$1"
  local extra="${2:-}"
  local seen=" "
  local pid=""
  if [[ -f "$pid_file" ]]; then
    while IFS= read -r pid || [[ -n "$pid" ]]; do
      [[ "$seen" == *" ${pid} "* ]] && continue
      if pid_is_alive "$pid"; then
        printf '%s\n' "$pid"
        seen="${seen}${pid} "
      fi
    done < "$pid_file"
  fi
  if [[ "$seen" != *" ${extra} "* ]] && pid_is_alive "$extra"; then
    printf '%s\n' "$extra"
  fi
}

write_pids() {
  local pid_file="$1"
  local live="$2"
  local tmp
  tmp=$(mktemp "${CACHE_DIR}/.tmp.XXXXXX")
  chmod 600 "$tmp"
  if [[ -n "$live" ]]; then
    printf '%s\n' "$live" > "$tmp"
  fi
  mv -f "$tmp" "$pid_file"
  chmod 600 "$pid_file"
}

daemon_is_alive() {
  local daemon_pid_file="$1"
  local daemon_pid=""
  [[ -f "$daemon_pid_file" ]] || return 1
  daemon_pid=$(cat "$daemon_pid_file" 2>/dev/null || true)
  pid_is_alive "$daemon_pid"
}

delete_cache() {
  local profile="$1"
  rm -f \
    "$(cache_json_path "$profile")" \
    "$(cache_config_path "$profile")" \
    "$(cache_pid_path "$profile")" \
    "$(cache_daemon_pid_path "$profile")"
}

needs_refresh() {
  local cache_json="$1"
  [[ -f "$cache_json" ]] || return 0
  node -e '
    const fs = require("fs");
    const data = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
    if (!data.Expiration) process.exit(1);
    const expiresAt = Date.parse(data.Expiration);
    if (Number.isNaN(expiresAt)) process.exit(0);
    const windowMs = Number(process.argv[2]) * 1000;
    process.exit(Date.now() >= expiresAt - windowMs ? 0 : 1);
  ' "$cache_json" "$REFRESH_WINDOW_SECONDS"
}

SERVE_PROFILE=""
SERVE_PID_FILE=""
SERVE_LOCKDIR=""
SERVE_IS_DAEMON=0

finish_serve() {
  local live=""
  [[ "$SERVE_IS_DAEMON" == 1 ]] || return 0
  SERVE_IS_DAEMON=0
  acquire_lock "$SERVE_LOCKDIR" || return 0
  live=$(list_live_pids "$SERVE_PID_FILE")
  write_pids "$SERVE_PID_FILE" "$live"
  if [[ -z "$live" ]]; then
    delete_cache "$SERVE_PROFILE"
  fi
  release_lock "$SERVE_LOCKDIR"
}

stop_serve() {
  trap - EXIT TERM INT
  finish_serve
  exit 0
}

serve_profile() {
  local profile="$1"
  local watch_pid="$2"
  local pid_file daemon_pid_file lockdir cache_json
  local live=""
  local next_refresh_attempt=0
  local now=0

  validate_profile "$profile"
  if ! pid_is_alive "$watch_pid"; then
    echo "Refusing to watch dead pid ${watch_pid} for profile \"${profile}\"." >&2
    exit 1
  fi
  ensure_cache_dir
  pid_file=$(cache_pid_path "$profile")
  daemon_pid_file=$(cache_daemon_pid_path "$profile")
  lockdir=$(cache_lock_path "$profile")
  cache_json=$(cache_json_path "$profile")
  SERVE_PROFILE="$profile"
  SERVE_PID_FILE="$pid_file"
  SERVE_LOCKDIR="$lockdir"

  trap '' HUP
  cd /

  acquire_lock "$lockdir"
  live=$(list_live_pids "$pid_file" "$watch_pid")
  write_pids "$pid_file" "$live"
  if daemon_is_alive "$daemon_pid_file"; then
    release_lock "$lockdir"
    log_line "$profile" "joined cache daemon for ${profile} watching ${watch_pid}"
    exit 0
  fi
  printf '%s\n' "$$" > "$daemon_pid_file"
  chmod 600 "$daemon_pid_file"
  SERVE_IS_DAEMON=1
  release_lock "$lockdir"
  trap stop_serve TERM INT
  trap finish_serve EXIT
  log_line "$profile" "cache daemon started for ${profile} watching ${watch_pid}"

  while true; do
    acquire_lock "$lockdir"
    live=$(list_live_pids "$pid_file")
    write_pids "$pid_file" "$live"
    if [[ -z "$live" ]]; then
      delete_cache "$profile"
      SERVE_IS_DAEMON=0
      release_lock "$lockdir"
      log_line "$profile" "removed credential cache for ${profile}; no watched processes remain"
      exit 0
    fi
    release_lock "$lockdir"

    now=$(date +%s)
    if needs_refresh "$cache_json" && [[ "$now" -ge "$next_refresh_attempt" ]]; then
      if refresh_profile "$profile" >> "$(cache_log_path "$profile")" 2>&1; then
        next_refresh_attempt=0
        log_line "$profile" "refreshed credential cache for ${profile}"
      else
        next_refresh_attempt=$((now + REFRESH_RETRY_SECONDS))
        log_line "$profile" "refresh failed for ${profile}; keeping the previous cache"
      fi
    fi
    sleep "$POLL_SECONDS"
  done
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
  refresh)
    [[ $# -eq 1 ]] || usage
    refresh_profile "$1"
    ;;
  print)
    [[ $# -eq 1 ]] || usage
    print_profile "$1"
    ;;
  serve)
    [[ $# -eq 2 ]] || usage
    serve_profile "$1" "$2"
    ;;
  *)
    usage
    ;;
esac
