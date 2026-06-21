#!/usr/bin/env bash
# deploy/deploy-server.sh
# Uploads the repo to a remote Linux host and starts the control-plane with docker compose.

set -euo pipefail

: "${DEPLOY_HOST:?DEPLOY_HOST is required}"

random_hex() {
  local bytes="${1}"
  openssl rand -hex "${bytes}" | tr -d '\n'
}

derive_public_url() {
  local host_part="${DEPLOY_HOST##*@}"
  host_part="${host_part%%:*}"
  printf 'https://%s' "${host_part}"
}

generated_messages=()
record_generated() {
  generated_messages+=("${1}")
}

DEPLOY_PATH="${DEPLOY_PATH:-/opt/v2ray-platform}"
if [[ -z "${CONTROL_PLANE_PUBLIC_URL:-}" ]]; then
  CONTROL_PLANE_PUBLIC_URL="$(derive_public_url)"
  record_generated "Derived CONTROL_PLANE_PUBLIC_URL=${CONTROL_PLANE_PUBLIC_URL} from DEPLOY_HOST"
fi
BOOTSTRAP_ADMIN_EMAIL="${BOOTSTRAP_ADMIN_EMAIL:-admin@local.invalid}"
if [[ -z "${BOOTSTRAP_ADMIN_PASSWORD:-}" ]]; then
  BOOTSTRAP_ADMIN_PASSWORD="$(random_hex 16)"
  record_generated "Generated BOOTSTRAP_ADMIN_PASSWORD"
fi
if [[ -z "${CONTROL_PLANE_SESSION_SECRET:-}" ]]; then
  CONTROL_PLANE_SESSION_SECRET="$(random_hex 32)"
  record_generated "Generated CONTROL_PLANE_SESSION_SECRET"
fi
if [[ -z "${CONTROL_PLANE_ADMIN_TOKEN:-}" ]]; then
  CONTROL_PLANE_ADMIN_TOKEN="$(random_hex 24)"
  record_generated "Generated CONTROL_PLANE_ADMIN_TOKEN"
fi
if [[ -z "${CLOUDFRONT_MASTER_KEY:-}" ]]; then
  CLOUDFRONT_MASTER_KEY="$(random_hex 16)"
  record_generated "Generated CLOUDFRONT_MASTER_KEY"
fi
CONTROL_PLANE_IMAGE="${CONTROL_PLANE_IMAGE:-ghcr.io/yxnt/v2ray-platform-control-plane:latest}"
POSTGRES_DB="${POSTGRES_DB:-v2ray_platform}"
POSTGRES_USER="${POSTGRES_USER:-v2ray_platform}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-}"
CONTROL_PLANE_BIND_PORT="${CONTROL_PLANE_BIND_PORT:-127.0.0.1:18080}"
CONTROL_PLANE_HEALTHCHECK_URL="${CONTROL_PLANE_HEALTHCHECK_URL:-http://127.0.0.1:${CONTROL_PLANE_BIND_PORT##*:}/healthz}"
DEPLOY_INFO_PATH="${DEPLOY_INFO_PATH:-${DEPLOY_PATH}/deploy-info.txt}"

if [[ -z "${DATABASE_URL:-}" ]]; then
  if [[ -z "${POSTGRES_PASSWORD}" ]]; then
    POSTGRES_PASSWORD="$(random_hex 24)"
    record_generated "Generated POSTGRES_PASSWORD for bundled Postgres"
  fi
  DATABASE_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable"
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
SSH_OPTS_ARRAY=()
if [[ -n "${SSH_OPTS:-}" ]]; then
  # shellcheck disable=SC2206
  SSH_OPTS_ARRAY=(${SSH_OPTS})
fi

bash "${SCRIPT_DIR}/preflight-server.sh"

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

archive="${tmpdir}/repo.tar.gz"
envfile="${tmpdir}/.env.server"
dumpfile_remote=""

cat > "${envfile}" <<EOF
DATABASE_URL=${DATABASE_URL}
BOOTSTRAP_ADMIN_EMAIL=${BOOTSTRAP_ADMIN_EMAIL}
BOOTSTRAP_ADMIN_PASSWORD=${BOOTSTRAP_ADMIN_PASSWORD}
CONTROL_PLANE_SESSION_SECRET=${CONTROL_PLANE_SESSION_SECRET}
CLOUDFRONT_MASTER_KEY=${CLOUDFRONT_MASTER_KEY}
CONTROL_PLANE_URL=${CONTROL_PLANE_PUBLIC_URL}
CONTROL_PLANE_BIND_PORT=${CONTROL_PLANE_BIND_PORT}
CONTROL_PLANE_IMAGE=${CONTROL_PLANE_IMAGE}
POSTGRES_DB=${POSTGRES_DB}
POSTGRES_USER=${POSTGRES_USER}
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
EOF

if [[ -n "${CONTROL_PLANE_ADMIN_TOKEN:-}" ]]; then
  echo "CONTROL_PLANE_ADMIN_TOKEN=${CONTROL_PLANE_ADMIN_TOKEN}" >> "${envfile}"
fi

if [[ -n "${CONTROL_PLANE_ALERT_WEBHOOK_URL:-}" ]]; then
  echo "CONTROL_PLANE_ALERT_WEBHOOK_URL=${CONTROL_PLANE_ALERT_WEBHOOK_URL}" >> "${envfile}"
fi

COPYFILE_DISABLE=1 tar \
  --exclude='._*' \
  --exclude='.git' \
  --exclude='.pensieve' \
  --exclude='.DS_Store' \
  -czf "${archive}" \
  -C "${REPO_ROOT}" \
  .

ssh ${SSH_OPTS:+${SSH_OPTS_ARRAY[@]}} "${DEPLOY_HOST}" "mkdir -p '${DEPLOY_PATH}'"
scp ${SSH_OPTS:+${SSH_OPTS_ARRAY[@]}} "${archive}" "${DEPLOY_HOST}:${DEPLOY_PATH}/repo.tar.gz"
scp ${SSH_OPTS:+${SSH_OPTS_ARRAY[@]}} "${envfile}" "${DEPLOY_HOST}:${DEPLOY_PATH}/.env.server"

if [[ -n "${POSTGRES_RESTORE_DUMP:-}" ]]; then
  dumpfile_remote="${DEPLOY_PATH}/$(basename "${POSTGRES_RESTORE_DUMP}")"
  scp ${SSH_OPTS:+${SSH_OPTS_ARRAY[@]}} "${POSTGRES_RESTORE_DUMP}" "${DEPLOY_HOST}:${dumpfile_remote}"
fi

ssh ${SSH_OPTS:+${SSH_OPTS_ARRAY[@]}} "${DEPLOY_HOST}" "DEPLOY_PATH='${DEPLOY_PATH}' bash -s" <<'EOF'
set -euo pipefail
cd "${DEPLOY_PATH}"
tar -xzf repo.tar.gz
rm -f repo.tar.gz
docker compose --env-file .env.server -f deploy/docker-compose.server.yml up -d postgres
for _ in $(seq 1 30); do
  if docker compose --env-file .env.server -f deploy/docker-compose.server.yml exec -T postgres \
    sh -lc 'PGPASSWORD="$POSTGRES_PASSWORD" pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"' >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
EOF

if [[ -n "${dumpfile_remote}" ]]; then
  ssh ${SSH_OPTS:+${SSH_OPTS_ARRAY[@]}} "${DEPLOY_HOST}" "DEPLOY_PATH='${DEPLOY_PATH}' DUMP_FILE='${dumpfile_remote}' bash -s" <<'EOF'
set -euo pipefail
cd "${DEPLOY_PATH}"
container_id="$(docker compose --env-file .env.server -f deploy/docker-compose.server.yml ps -q postgres)"
docker cp "${DUMP_FILE}" "${container_id}:/tmp/restore.dump"
docker compose --env-file .env.server -f deploy/docker-compose.server.yml exec -T postgres \
  sh -lc 'PGPASSWORD="$POSTGRES_PASSWORD" pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --clean --if-exists --no-owner --no-privileges /tmp/restore.dump'
rm -f "${DUMP_FILE}"
docker compose --env-file .env.server -f deploy/docker-compose.server.yml exec -T postgres rm -f /tmp/restore.dump
EOF
fi

ssh ${SSH_OPTS:+${SSH_OPTS_ARRAY[@]}} "${DEPLOY_HOST}" "DEPLOY_PATH='${DEPLOY_PATH}' bash -s" <<'EOF'
set -euo pipefail
cd "${DEPLOY_PATH}"
docker compose --env-file .env.server -f deploy/docker-compose.server.yml pull control-plane
docker compose --env-file .env.server -f deploy/docker-compose.server.yml up -d control-plane
EOF

ssh ${SSH_OPTS:+${SSH_OPTS_ARRAY[@]}} "${DEPLOY_HOST}" \
  "DEPLOY_PATH='${DEPLOY_PATH}' DEPLOY_INFO_PATH='${DEPLOY_INFO_PATH}' CONTROL_PLANE_PUBLIC_URL='${CONTROL_PLANE_PUBLIC_URL}' CONTROL_PLANE_HEALTHCHECK_URL='${CONTROL_PLANE_HEALTHCHECK_URL}' CONTROL_PLANE_IMAGE='${CONTROL_PLANE_IMAGE}' BOOTSTRAP_ADMIN_EMAIL='${BOOTSTRAP_ADMIN_EMAIL}' BOOTSTRAP_ADMIN_PASSWORD='${BOOTSTRAP_ADMIN_PASSWORD}' CONTROL_PLANE_ADMIN_TOKEN='${CONTROL_PLANE_ADMIN_TOKEN}' CLOUDFRONT_MASTER_KEY='${CLOUDFRONT_MASTER_KEY}' POSTGRES_DB='${POSTGRES_DB}' POSTGRES_USER='${POSTGRES_USER}' POSTGRES_PASSWORD='${POSTGRES_PASSWORD}' DATABASE_URL_SET='${DATABASE_URL:+1}' bash -s" <<'EOF'
set -euo pipefail
mkdir -p "$(dirname "${DEPLOY_INFO_PATH}")"
umask 077
cat > "${DEPLOY_INFO_PATH}" <<INFO
v2ray-platform deploy info
generated_at=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
deploy_path=${DEPLOY_PATH}
public_url=${CONTROL_PLANE_PUBLIC_URL}
remote_healthcheck_url=${CONTROL_PLANE_HEALTHCHECK_URL}
control_plane_image=${CONTROL_PLANE_IMAGE}
bootstrap_admin_email=${BOOTSTRAP_ADMIN_EMAIL}
bootstrap_admin_password=${BOOTSTRAP_ADMIN_PASSWORD}
control_plane_admin_token=${CONTROL_PLANE_ADMIN_TOKEN}
cloudfront_master_key=${CLOUDFRONT_MASTER_KEY}
env_file=${DEPLOY_PATH}/.env.server
INFO

if [[ -z "${DATABASE_URL_SET}" ]]; then
  cat >> "${DEPLOY_INFO_PATH}" <<INFO
postgres_db=${POSTGRES_DB}
postgres_user=${POSTGRES_USER}
postgres_password=${POSTGRES_PASSWORD}
INFO
fi
chmod 600 "${DEPLOY_INFO_PATH}"
EOF

echo "Verifying deployed server health..."
if ! ssh ${SSH_OPTS:+${SSH_OPTS_ARRAY[@]}} "${DEPLOY_HOST}" "curl -fsSL '${CONTROL_PLANE_HEALTHCHECK_URL}' >/dev/null"; then
  echo "server deploy completed but remote health check failed: ${CONTROL_PLANE_HEALTHCHECK_URL}" >&2
  exit 1
fi

echo ""
echo "✓ Server deployed: ${CONTROL_PLANE_PUBLIC_URL}"
echo ""
if [[ "${#generated_messages[@]}" -gt 0 ]]; then
  echo "Auto-generated values:"
  for item in "${generated_messages[@]}"; do
    echo "  - ${item}"
  done
  echo ""
fi
echo "Next steps:"
echo "  1. Remote health check passed at ${CONTROL_PLANE_HEALTHCHECK_URL}."
echo "  2. Confirm the host or reverse proxy points public traffic at ${CONTROL_PLANE_BIND_PORT}."
echo "  3. Review sensitive bootstrap data on the server at ${DEPLOY_INFO_PATH}."
echo "  4. Open ${CONTROL_PLANE_PUBLIC_URL} to verify the admin UI loads."
echo "  5. Log in with ${BOOTSTRAP_ADMIN_EMAIL}."
echo "  6. Add nodes via the '+ Add Node' panel in the Nodes tab."
