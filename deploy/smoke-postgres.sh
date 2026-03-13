#!/usr/bin/env bash
set -euo pipefail

POSTGRES_IMAGE="${POSTGRES_IMAGE:-docker.ispider.io/postgres:latest}"
POSTGRES_PORT="${POSTGRES_PORT:-55432}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-v2ray-platform-pg-test-${POSTGRES_PORT}}"
DATABASE_URL="${DATABASE_URL:-postgres://postgres:postgres@127.0.0.1:${POSTGRES_PORT}/v2ray_platform?sslmode=disable}"
BOOTSTRAP_ADMIN_EMAIL="${BOOTSTRAP_ADMIN_EMAIL:-admin@example.com}"
BOOTSTRAP_ADMIN_PASSWORD="${BOOTSTRAP_ADMIN_PASSWORD:-adminadmin}"
SMOKE_PORT="${SMOKE_PORT:-18080}"
CONTROL_PLANE_URL="http://127.0.0.1:${SMOKE_PORT}"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
STATE_PATH="${REPO_ROOT}/.tmp-agent-state.json"
CONFIG_PATH="${REPO_ROOT}/.tmp-config.json"
USAGE_PATH="${REPO_ROOT}/.tmp-usage.json"
USAGE_STATS_PATH="${REPO_ROOT}/.tmp-usage-stats.txt"

cleanup() {
  rm -f "${STATE_PATH}" "${CONFIG_PATH}" "${USAGE_PATH}" "${USAGE_STATS_PATH}"
  docker rm -f "${POSTGRES_CONTAINER}" >/dev/null 2>&1 || true
  if [[ -n "${SERVER_PID:-}" ]]; then
    kill "${SERVER_PID}" >/dev/null 2>&1 || true
    wait "${SERVER_PID}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

cd "${REPO_ROOT}"
docker rm -f "${POSTGRES_CONTAINER}" >/dev/null 2>&1 || true
docker run --name "${POSTGRES_CONTAINER}" \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=v2ray_platform \
  -p "${POSTGRES_PORT}:5432" \
  -d "${POSTGRES_IMAGE}" >/dev/null

for _ in $(seq 1 60); do
  if docker exec "${POSTGRES_CONTAINER}" pg_isready -U postgres >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

DATABASE_URL="${DATABASE_URL}" \
BOOTSTRAP_ADMIN_EMAIL="${BOOTSTRAP_ADMIN_EMAIL}" \
BOOTSTRAP_ADMIN_PASSWORD="${BOOTSTRAP_ADMIN_PASSWORD}" \
CONTROL_PLANE_LISTEN_ADDR=":${SMOKE_PORT}" \
go run ./cmd/control-plane >/tmp/v2ray-platform-control-plane.log 2>&1 &
SERVER_PID=$!

for _ in $(seq 1 30); do
  if curl -fsS "${CONTROL_PLANE_URL}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

LOGIN_JSON="$(curl -fsS \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"${BOOTSTRAP_ADMIN_EMAIL}\",\"password\":\"${BOOTSTRAP_ADMIN_PASSWORD}\"}" \
  "${CONTROL_PLANE_URL}/api/admin/login")"

SESSION_TOKEN="$(python -c 'import json,sys; print(json.loads(sys.stdin.read())["session_token"])' <<<"${LOGIN_JSON}")"

BOOTSTRAP_JSON="$(curl -fsS \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer ${SESSION_TOKEN}" \
  -d '{"description":"pg-smoke","ttl_hours":1}' \
  "${CONTROL_PLANE_URL}/api/admin/bootstrap-tokens")"

BOOTSTRAP_TOKEN="$(python -c 'import json,sys; print(json.loads(sys.stdin.read())["bootstrap_token"])' <<<"${BOOTSTRAP_JSON}")"

CONTROL_PLANE_URL="${CONTROL_PLANE_URL}" \
BOOTSTRAP_TOKEN="${BOOTSTRAP_TOKEN}" \
NODE_NAME=pg-smoke-node \
NODE_REGION=test \
NODE_PUBLIC_HOST=pg-smoke.example.com \
AGENT_STATE_PATH="${STATE_PATH}" \
NODE_CONFIG_OUTPUT_PATH="${CONFIG_PATH}" \
NODE_HEARTBEAT_INTERVAL_SECONDS=1 \
timeout 3 go run ./cmd/node-agent >/tmp/v2ray-platform-node-agent.log 2>&1 || true

MEMBER_JSON="$(curl -fsS \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer ${SESSION_TOKEN}" \
  -d '{"name":"Alice","email":"alice@example.com"}' \
  "${CONTROL_PLANE_URL}/api/admin/members")"

NODE_ID="$(curl -fsS -H "Authorization: Bearer ${SESSION_TOKEN}" "${CONTROL_PLANE_URL}/api/admin/nodes" | python -c 'import json,sys; print(json.load(sys.stdin)["items"][0]["id"])')"
MEMBER_ID="$(python -c 'import json,sys; print(json.loads(sys.stdin.read())["id"])' <<<"${MEMBER_JSON}")"
GRANT_JSON="$(curl -fsS \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer ${SESSION_TOKEN}" \
  -d "{\"node_id\":\"${NODE_ID}\",\"member_id\":\"${MEMBER_ID}\"}" \
  "${CONTROL_PLANE_URL}/api/admin/grants")"

CREDENTIAL_UUID="$(python -c 'import json,sys; print(json.loads(sys.stdin.read())["credential"]["uuid"])' <<<"${GRANT_JSON}")"
cat >"${USAGE_STATS_PATH}" <<EOF
stat: {
  name: "user>>>${CREDENTIAL_UUID}>>>traffic>>>uplink"
  value: 120
}
stat: {
  name: "user>>>${CREDENTIAL_UUID}>>>traffic>>>downlink"
  value: 340
}
EOF

CONTROL_PLANE_URL="${CONTROL_PLANE_URL}" \
NODE_NAME=pg-smoke-node \
NODE_REGION=test \
NODE_PUBLIC_HOST=pg-smoke.example.com \
AGENT_STATE_PATH="${STATE_PATH}" \
NODE_CONFIG_OUTPUT_PATH="${CONFIG_PATH}" \
NODE_USAGE_SOURCE=runtime \
NODE_USAGE_COLLECTION_INTERVAL_SECONDS=1 \
NODE_USAGE_QUERY_COMMAND="cat ${USAGE_STATS_PATH}" \
NODE_HEARTBEAT_INTERVAL_SECONDS=1 \
timeout 3 go run ./cmd/node-agent >/tmp/v2ray-platform-node-agent.log 2>&1 || true

grep -q "${CREDENTIAL_UUID}" "${CONFIG_PATH}"
curl -fsS -H "Authorization: Bearer ${SESSION_TOKEN}" "${CONTROL_PLANE_URL}/api/admin/usage/nodes" | python -c 'import json,sys; items=json.load(sys.stdin)["items"]; assert len(items)==1, items; assert items[0]["total_bytes"]==460, items'
curl -fsS -H "Authorization: Bearer ${SESSION_TOKEN}" "${CONTROL_PLANE_URL}/api/admin/usage/members" | python -c 'import json,sys; items=json.load(sys.stdin)["items"]; assert len(items)==1, items; assert items[0]["total_bytes"]==460, items'

MEMBER_BOB_JSON="$(curl -fsS \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer ${SESSION_TOKEN}" \
  -d '{"name":"Bob","email":"bob@example.com","note":"Batch target"}' \
  "${CONTROL_PLANE_URL}/api/admin/members")"
MEMBER_CAROL_JSON="$(curl -fsS \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer ${SESSION_TOKEN}" \
  -d '{"name":"Carol","email":"carol@example.com","note":"Batch target"}' \
  "${CONTROL_PLANE_URL}/api/admin/members")"
BOB_ID="$(python -c 'import json,sys; print(json.loads(sys.stdin.read())["id"])' <<<"${MEMBER_BOB_JSON}")"
CAROL_ID="$(python -c 'import json,sys; print(json.loads(sys.stdin.read())["id"])' <<<"${MEMBER_CAROL_JSON}")"
GRANT_BOB_JSON="$(curl -fsS \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer ${SESSION_TOKEN}" \
  -d "{\"node_id\":\"${NODE_ID}\",\"member_id\":\"${BOB_ID}\"}" \
  "${CONTROL_PLANE_URL}/api/admin/grants")"
ALICE_GRANT_ID="$(python -c 'import json,sys; print(json.loads(sys.stdin.read())["grant"]["id"])' <<<"${GRANT_JSON}")"
BOB_GRANT_ID="$(python -c 'import json,sys; print(json.loads(sys.stdin.read())["grant"]["id"])' <<<"${GRANT_BOB_JSON}")"

curl -fsS -H "Authorization: Bearer ${SESSION_TOKEN}" "${CONTROL_PLANE_URL}/api/admin/members?q=alice" | python -c 'import json,sys; items=json.load(sys.stdin)["items"]; assert len(items)==1 and items[0]["email"]=="alice@example.com", items'
curl -fsS -H "Authorization: Bearer ${SESSION_TOKEN}" "${CONTROL_PLANE_URL}/api/admin/nodes?status=online&q=pg-smoke" | python -c 'import json,sys; items=json.load(sys.stdin)["items"]; assert len(items)==1 and items[0]["status"]=="online", items'
curl -fsS -H "Authorization: Bearer ${SESSION_TOKEN}" "${CONTROL_PLANE_URL}/api/admin/grants?q=bob" | python -c 'import json,sys; items=json.load(sys.stdin)["items"]; assert len(items)==1 and items[0]["member_email"]=="bob@example.com", items'
curl -fsS \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer ${SESSION_TOKEN}" \
  -d "{\"grant_ids\":[\"${ALICE_GRANT_ID}\",\"${BOB_GRANT_ID}\"]}" \
  "${CONTROL_PLANE_URL}/api/admin/grants/batch-revoke" | python -c 'import json,sys; payload=json.load(sys.stdin); assert payload["revoked_count"]==2, payload'
curl -fsS \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer ${SESSION_TOKEN}" \
  -d "{\"member_ids\":[\"${BOB_ID}\",\"${CAROL_ID}\"]}" \
  "${CONTROL_PLANE_URL}/api/admin/members/batch-delete" | python -c 'import json,sys; payload=json.load(sys.stdin); assert payload["deleted_count"]==2, payload'
curl -fsS -H "Authorization: Bearer ${SESSION_TOKEN}" "${CONTROL_PLANE_URL}/api/admin/audit-logs" | python -c 'import json,sys; actions=[item["action"] for item in json.load(sys.stdin)["items"]]; assert "grant.batch_revoked" in actions, actions; assert "member.batch_deleted" in actions, actions'

echo "PostgreSQL smoke test passed for node ${NODE_ID} with usage aggregation and batch management"
