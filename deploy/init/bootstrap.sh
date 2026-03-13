#!/usr/bin/env bash
set -euo pipefail

CONTROL_PLANE_URL="${CONTROL_PLANE_URL:?CONTROL_PLANE_URL is required}"
BOOTSTRAP_TOKEN="${BOOTSTRAP_TOKEN:?BOOTSTRAP_TOKEN is required}"
NODE_NAME="${NODE_NAME:-$(hostname)}"
NODE_REGION="${NODE_REGION:-unknown}"
NODE_PUBLIC_HOST="${NODE_PUBLIC_HOST:-}"
NODE_PROVIDER="${NODE_PROVIDER:-}"
NODE_TAGS="${NODE_TAGS:-}"
RUNTIME_FLAVOR="${RUNTIME_FLAVOR:-v2ray}"
NODE_USAGE_SOURCE="${NODE_USAGE_SOURCE:-runtime}"
NODE_USAGE_QUERY_SERVER="${NODE_USAGE_QUERY_SERVER:-127.0.0.1:10085}"
NODE_USAGE_COLLECTION_INTERVAL_SECONDS="${NODE_USAGE_COLLECTION_INTERVAL_SECONDS:-60}"
NODE_USAGE_QUERY_COMMAND="${NODE_USAGE_QUERY_COMMAND:-}"
AGENT_DOWNLOAD_URL="${AGENT_DOWNLOAD_URL:-}"
LOCAL_AGENT_BINARY="${LOCAL_AGENT_BINARY:-}"
INSTALL_ROOT="${INSTALL_ROOT:-/opt/v2ray-platform}"
BIN_DIR="${BIN_DIR:-/usr/local/bin}"
STATE_DIR="${STATE_DIR:-/var/lib/v2ray-platform}"
CONFIG_DIR="${CONFIG_DIR:-/etc/v2ray-platform}"
ENV_FILE="${ENV_FILE:-/etc/default/v2ray-platform-node-agent}"

require_root() {
  if [ "$(id -u)" -ne 0 ]; then
    echo "please run as root" >&2
    exit 1
  fi
}

install_packages() {
  if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -y
    apt-get install -y curl ca-certificates tar
  fi
}

install_runtime() {
  mkdir -p "${INSTALL_ROOT}" "${STATE_DIR}" "${CONFIG_DIR}"
  if ! command -v v2ray >/dev/null 2>&1 && [ "${RUNTIME_FLAVOR}" = "v2ray" ]; then
    echo "v2ray binary not found; install it before enabling production traffic" >&2
  fi
}

install_agent() {
  if [ -n "${LOCAL_AGENT_BINARY}" ]; then
    install -m 0755 "${LOCAL_AGENT_BINARY}" "${BIN_DIR}/v2ray-platform-node-agent"
    return
  fi

  if [ -z "${AGENT_DOWNLOAD_URL}" ]; then
    echo "set AGENT_DOWNLOAD_URL or LOCAL_AGENT_BINARY to install the agent" >&2
    exit 1
  fi

  tmpdir="$(mktemp -d)"
  trap 'rm -rf "${tmpdir}"' EXIT
  curl -fsSL "${AGENT_DOWNLOAD_URL}" -o "${tmpdir}/node-agent"
  install -m 0755 "${tmpdir}/node-agent" "${BIN_DIR}/v2ray-platform-node-agent"
}

write_env_file() {
  cat > "${ENV_FILE}" <<EOF
CONTROL_PLANE_URL=${CONTROL_PLANE_URL}
BOOTSTRAP_TOKEN=${BOOTSTRAP_TOKEN}
NODE_NAME=${NODE_NAME}
NODE_REGION=${NODE_REGION}
NODE_PUBLIC_HOST=${NODE_PUBLIC_HOST}
NODE_PROVIDER=${NODE_PROVIDER}
NODE_TAGS=${NODE_TAGS}
RUNTIME_FLAVOR=${RUNTIME_FLAVOR}
AGENT_STATE_PATH=${STATE_DIR}/agent-state.json
NODE_CONFIG_OUTPUT_PATH=/etc/v2ray/config.generated.json
NODE_USAGE_SOURCE=${NODE_USAGE_SOURCE}
NODE_USAGE_QUERY_SERVER=${NODE_USAGE_QUERY_SERVER}
NODE_USAGE_COLLECTION_INTERVAL_SECONDS=${NODE_USAGE_COLLECTION_INTERVAL_SECONDS}
NODE_USAGE_QUERY_COMMAND=${NODE_USAGE_QUERY_COMMAND}
NODE_RELOAD_COMMAND=systemctl reload v2ray || systemctl restart v2ray
EOF
  chmod 0600 "${ENV_FILE}"
}

install_systemd_unit() {
  cat > /etc/systemd/system/v2ray-platform-node-agent.service <<'EOF'
[Unit]
Description=V2Ray Platform Node Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/default/v2ray-platform-node-agent
ExecStart=/usr/local/bin/v2ray-platform-node-agent
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable --now v2ray-platform-node-agent
}

require_root
install_packages
install_runtime
install_agent
write_env_file
install_systemd_unit

echo "node bootstrap completed"
