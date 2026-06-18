#!/usr/bin/env bash
# deploy/deploy-auto.sh
# Chooses the right deploy path based on the current environment.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ -n "${DEPLOY_HOST:-}" ]]; then
  echo "Auto deploy mode: ssh-server"
  bash "${SCRIPT_DIR}/deploy-server.sh"
  exit 0
fi

if [[ -n "${GCP_PROJECT:-}" ]]; then
  echo "Auto deploy mode: cloudrun"
  bash "${SCRIPT_DIR}/deploy-cloudrun.sh"
  exit 0
fi

cat >&2 <<'EOF'
Unable to determine deploy mode automatically.

Set one of the following before retrying:
- GCP_PROJECT for Cloud Run deploys
- DEPLOY_HOST for SSH server deploys

Templates:
- deploy/cloudrun.env.example
- deploy/server.env.example
EOF
exit 1
