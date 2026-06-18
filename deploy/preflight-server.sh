#!/usr/bin/env bash
# deploy/preflight-server.sh
# Validates that the current environment is ready to run deploy/deploy-server.sh.

set -euo pipefail

required_env=(
  DEPLOY_HOST
  DEPLOY_PATH
  CONTROL_PLANE_PUBLIC_URL
  BOOTSTRAP_ADMIN_EMAIL
  BOOTSTRAP_ADMIN_PASSWORD
  CONTROL_PLANE_SESSION_SECRET
)

required_cmds=(
  ssh
  scp
  tar
  curl
  git
)

missing=0
SSH_OPTS_ARRAY=()
if [[ -n "${SSH_OPTS:-}" ]]; then
  # shellcheck disable=SC2206
  SSH_OPTS_ARRAY=(${SSH_OPTS})
fi

echo "==> Checking required environment variables"
for name in "${required_env[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    echo "missing env: ${name}" >&2
    missing=1
  else
    echo "ok env: ${name}"
  fi
done

if [[ -z "${DATABASE_URL:-}" ]]; then
  for name in POSTGRES_DB POSTGRES_USER POSTGRES_PASSWORD; do
    if [[ -z "${!name:-}" ]]; then
      echo "missing env: ${name} (required when DATABASE_URL is not set)" >&2
      missing=1
    else
      echo "ok env: ${name}"
    fi
  done
else
  echo "ok env: DATABASE_URL"
fi

if [[ -n "${POSTGRES_RESTORE_DUMP:-}" ]]; then
  if [[ ! -f "${POSTGRES_RESTORE_DUMP}" ]]; then
    echo "missing file: POSTGRES_RESTORE_DUMP=${POSTGRES_RESTORE_DUMP}" >&2
    missing=1
  else
    echo "ok file: POSTGRES_RESTORE_DUMP"
  fi
fi

echo ""
echo "==> Checking required commands"
for cmd in "${required_cmds[@]}"; do
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "missing command: ${cmd}" >&2
    missing=1
  else
    echo "ok command: ${cmd}"
  fi
done

if [[ "${missing}" -ne 0 ]]; then
  echo ""
  echo "Server deploy preflight failed." >&2
  echo "Fix the missing items above, then rerun deploy/preflight-server.sh." >&2
  exit 1
fi

echo ""
echo "==> Checking SSH connectivity"
if ! ssh ${SSH_OPTS:+${SSH_OPTS_ARRAY[@]}} -o BatchMode=yes -o ConnectTimeout=10 "${DEPLOY_HOST}" "echo connected" >/dev/null 2>&1; then
  echo "unable to connect to ${DEPLOY_HOST} over SSH" >&2
  exit 1
fi
echo "ok ssh: ${DEPLOY_HOST}"

echo ""
echo "==> Checking remote Docker runtime"
if ! ssh ${SSH_OPTS:+${SSH_OPTS_ARRAY[@]}} "${DEPLOY_HOST}" "command -v docker >/dev/null 2>&1"; then
  echo "remote host is missing docker" >&2
  exit 1
fi
echo "ok remote command: docker"

if ! ssh ${SSH_OPTS:+${SSH_OPTS_ARRAY[@]}} "${DEPLOY_HOST}" "docker compose version >/dev/null 2>&1"; then
  echo "remote host is missing docker compose" >&2
  exit 1
fi
echo "ok remote command: docker compose"

echo ""
echo "Server deploy preflight passed."
