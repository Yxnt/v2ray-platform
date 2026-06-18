#!/usr/bin/env bash
# deploy/preflight-cloudrun.sh
# Validates that the current environment is ready to run deploy/deploy-cloudrun.sh.

set -euo pipefail

required_env=(
  GCP_PROJECT
  DATABASE_URL
  BOOTSTRAP_ADMIN_EMAIL
  BOOTSTRAP_ADMIN_PASSWORD
  CONTROL_PLANE_SESSION_SECRET
)

required_cmds=(
  gcloud
  docker
  git
)

missing=0

echo "==> Checking required environment variables"
for name in "${required_env[@]}"; do
  if [[ -z "${!name:-}" ]]; then
    echo "missing env: ${name}" >&2
    missing=1
  else
    echo "ok env: ${name}"
  fi
done

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

echo ""
echo "==> Checking gcloud authentication"
if ! gcloud auth list --filter=status:ACTIVE --format='value(account)' | grep -q .; then
  echo "missing active gcloud auth session" >&2
  missing=1
else
  echo "ok gcloud auth: active account detected"
fi

if [[ "${missing}" -ne 0 ]]; then
  echo ""
  echo "Cloud Run deploy preflight failed." >&2
  echo "Fix the missing items above, then rerun deploy/preflight-cloudrun.sh." >&2
  exit 1
fi

echo ""
echo "Cloud Run deploy preflight passed."
