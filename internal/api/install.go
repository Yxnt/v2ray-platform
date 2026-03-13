package api

import (
"bytes"
"crypto/md5"
"errors"
"fmt"
"io"
"net/http"
"strings"
"text/template"
"time"
)

var installScriptTmpl = template.Must(template.New("install").Parse(`#!/usr/bin/env bash
set -euo pipefail

CONTROL_PLANE_URL="{{.ControlPlaneURL}}"
BOOTSTRAP_TOKEN="{{.Token}}"
NODE_NAME="{{.Name}}"
NODE_REGION="{{.Region}}"
NODE_PUBLIC_HOST="{{.Host}}"
NODE_TAGS="{{.Tags}}"
RUNTIME_FLAVOR="{{.Flavor}}"
NODE_USAGE_SOURCE="runtime"
NODE_USAGE_QUERY_SERVER="127.0.0.1:10085"
NODE_USAGE_QUERY_COMMAND="${V2RAY_INSTALL_DIR}/v2ray api stats --server=127.0.0.1:10085 -json"
NODE_USAGE_COLLECTION_INTERVAL_SECONDS="600"
V2RAY_INSTALL_DIR="/usr/local/v2ray"
V2RAY_CONFIG="${V2RAY_INSTALL_DIR}/config.json"
BIN_DIR="/usr/local/bin"
ENV_FILE="/etc/default/v2ray-platform-node-agent"
STATE_DIR="/var/lib/v2ray-platform"

if [ "$(id -u)" -ne 0 ]; then
  echo "please run as root" >&2; exit 1
fi

# ── system packages ───────────────────────────────────────────────────────────
if command -v apt-get >/dev/null 2>&1; then
  DEBIAN_FRONTEND=noninteractive apt-get update -y -qq
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq curl ca-certificates unzip nginx
fi

# ── install V2Ray ─────────────────────────────────────────────────────────────
echo "Installing V2Ray..."
mkdir -p "${V2RAY_INSTALL_DIR}"
ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64)  V2RAY_PKG="v2ray-linux-64.zip" ;;
  aarch64) V2RAY_PKG="v2ray-linux-arm64-v8a.zip" ;;
  *)       echo "Unsupported architecture: ${ARCH}" >&2; exit 1 ;;
esac
V2RAY_URL="https://github.com/v2fly/v2ray-core/releases/latest/download/${V2RAY_PKG}"
tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT
curl -fsSL "${V2RAY_URL}" -o "${tmpdir}/${V2RAY_PKG}"
unzip -q "${tmpdir}/${V2RAY_PKG}" -d "${tmpdir}/v2ray"
install -m 0755 "${tmpdir}/v2ray/v2ray" "${V2RAY_INSTALL_DIR}/v2ray"
cp "${tmpdir}/v2ray/geoip.dat"   "${V2RAY_INSTALL_DIR}/" 2>/dev/null || true
cp "${tmpdir}/v2ray/geosite.dat" "${V2RAY_INSTALL_DIR}/" 2>/dev/null || true

# ── initial V2Ray config (placeholder; node-agent will overwrite after sync) ──
cat > "${V2RAY_CONFIG}" <<EOF
{
  "log": {"loglevel": "info"},
  "inbounds": [{
    "port": 23333,
    "listen": "127.0.0.1",
    "protocol": "vmess",
    "settings": {"clients": [], "decryption": "none"},
    "streamSettings": {
      "network": "ws",
      "security": "none",
      "wsSettings": {"path": "/${NODE_NAME}"}
    }
  }],
  "outbounds": [
    {"protocol": "freedom", "tag": "direct"},
    {"protocol": "blackhole", "tag": "blocked"}
  ]
}
EOF

# ── V2Ray systemd service ─────────────────────────────────────────────────────
cat > /etc/systemd/system/v2ray.service <<'UNIT'
[Unit]
Description=V2Ray Service
Documentation=https://www.v2fly.org/
After=network.target nss-lookup.target

[Service]
User=root
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
NoNewPrivileges=true
Environment="V2RAY_VMESS_AEAD_FORCED=false"
ExecStart=/usr/bin/env v2ray.vmess.aead.forced=false /usr/local/v2ray/v2ray run -config /usr/local/v2ray/config.json
Restart=on-failure
RestartPreventExitStatus=23

[Install]
WantedBy=multi-user.target
UNIT

# ── nginx config ──────────────────────────────────────────────────────────────
echo "Configuring nginx..."
cat > /etc/nginx/conf.d/v2ray.conf <<EOF
server {
    listen 80;
    server_name _;

    location /${NODE_NAME} {
        proxy_redirect     off;
        proxy_pass         http://127.0.0.1:23333;
        proxy_http_version 1.1;
        proxy_set_header   Upgrade \$http_upgrade;
        proxy_set_header   Connection "upgrade";
        proxy_set_header   Host \$host;
        proxy_set_header   X-Real-IP \$remote_addr;
        proxy_set_header   X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }
}
EOF
rm -f /etc/nginx/sites-enabled/default /etc/nginx/sites-available/default 2>/dev/null || true

# ── agent env file ────────────────────────────────────────────────────────────
echo "Writing agent env file..."
mkdir -p "${STATE_DIR}"
cat > "${ENV_FILE}" <<EOF
CONTROL_PLANE_URL=${CONTROL_PLANE_URL}
BOOTSTRAP_TOKEN=${BOOTSTRAP_TOKEN}
NODE_NAME=${NODE_NAME}
NODE_REGION=${NODE_REGION}
NODE_PUBLIC_HOST=${NODE_PUBLIC_HOST}
NODE_TAGS=${NODE_TAGS}
RUNTIME_FLAVOR=${RUNTIME_FLAVOR}
AGENT_STATE_PATH=${STATE_DIR}/agent-state.json
NODE_CONFIG_OUTPUT_PATH=${V2RAY_CONFIG}
NODE_USAGE_SOURCE=${NODE_USAGE_SOURCE}
NODE_USAGE_QUERY_SERVER=${NODE_USAGE_QUERY_SERVER}
NODE_USAGE_QUERY_COMMAND=${NODE_USAGE_QUERY_COMMAND}
NODE_USAGE_COLLECTION_INTERVAL_SECONDS=${NODE_USAGE_COLLECTION_INTERVAL_SECONDS}
NODE_RELOAD_COMMAND=systemctl restart v2ray
EOF
chmod 0600 "${ENV_FILE}"

# ── node-agent binary ─────────────────────────────────────────────────────────
echo "Downloading node-agent..."
case "${ARCH}" in
  x86_64)  AGENT_ARCH="amd64" ;;
  aarch64) AGENT_ARCH="arm64" ;;
  *)       echo "Unsupported architecture: ${ARCH}" >&2; exit 1 ;;
esac
AGENT_URL="https://github.com/Yxnt/v2ray-platform/releases/download/latest/node-agent-linux-${AGENT_ARCH}"
curl -fsSL "${AGENT_URL}" -o "${BIN_DIR}/v2ray-platform-node-agent"
chmod 0755 "${BIN_DIR}/v2ray-platform-node-agent"

# ── node-agent systemd service ────────────────────────────────────────────────
echo "Installing node-agent systemd unit..."
cat > /etc/systemd/system/v2ray-platform-node-agent.service <<'UNIT'
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
UNIT

# ── enable and start all services ────────────────────────────────────────────
systemctl daemon-reload
systemctl enable --now v2ray
systemctl enable --now nginx
systemctl enable --now v2ray-platform-node-agent

echo "Node '${NODE_NAME}' bootstrap complete."
`))

type installScriptData struct {
ControlPlaneURL string
Token           string
Name            string
Region          string
Host            string
Tags            string
Flavor          string
}

func (svc *ControlPlaneService) handleInstallScript(w http.ResponseWriter, r *http.Request) {
token := r.URL.Query().Get("token")
if token == "" {
http.Error(w, "token query parameter is required", http.StatusBadRequest)
return
}

scheme := "https"
if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
scheme = "http"
}
cpURL := scheme + "://" + r.Host

flavor := r.URL.Query().Get("flavor")
if flavor == "" {
flavor = "v2ray"
}

data := installScriptData{
ControlPlaneURL: cpURL,
Token:           token,
Name:            r.URL.Query().Get("name"),
Region:          r.URL.Query().Get("region"),
Host:            r.URL.Query().Get("host"),
Tags:            r.URL.Query().Get("tags"),
Flavor:          flavor,
}

var buf bytes.Buffer
if err := installScriptTmpl.Execute(&buf, data); err != nil {
http.Error(w, "failed to render install script", http.StatusInternalServerError)
return
}

w.Header().Set("Content-Type", "text/plain; charset=utf-8")
w.Header().Set("Content-Disposition", `inline; filename="install.sh"`)
w.WriteHeader(http.StatusOK)
_, _ = w.Write(buf.Bytes())
}

func (svc *ControlPlaneService) handleNodeAgentBinary(w http.ResponseWriter, r *http.Request) {
if svc.agentDownloadURL == "" {
writeJSON(w, http.StatusServiceUnavailable, map[string]string{
"error": "agent binary not available: set AGENT_DOWNLOAD_URL",
})
return
}
target := svc.agentDownloadURL
if r.URL.Query().Get("arch") == "arm64" {
target = strings.ReplaceAll(target, "amd64", "arm64")
}
http.Redirect(w, r, target, http.StatusFound)
}

func (svc *ControlPlaneService) handleNodeAgentMD5(w http.ResponseWriter, r *http.Request) {
	arch := r.URL.Query().Get("arch")
	if arch == "" {
		arch = "amd64"
	}
	md5hex := svc.agentCache.get(arch)
	if md5hex == "" {
		// Try to fetch synchronously once.
		svc.refreshAgentMD5(arch)
		md5hex = svc.agentCache.get(arch)
	}
	if md5hex == "" {
		writeError(w, http.StatusServiceUnavailable, errors.New("agent MD5 not available"))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(md5hex + "\n"))
}

// refreshAgentMD5 fetches the .md5 sidecar file published alongside the binary in GitHub
// Releases and stores the value in the cache. Using the sidecar avoids downloading the full
// binary just to compute its hash, and ensures the cached value is always in sync with the
// release (no stale-cache window between publish and the next hourly refresh).
func (svc *ControlPlaneService) refreshAgentMD5(arch string) {
	downloadURL := svc.agentDownloadURL
	if arch == "arm64" {
		downloadURL = strings.ReplaceAll(downloadURL, "amd64", "arm64")
	}
	md5URL := downloadURL + ".md5"
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(md5URL)
	if err != nil {
		// Sidecar not available (old release or local override) — fall back to downloading
		// the full binary and computing the MD5 ourselves.
		svc.refreshAgentMD5ByDownload(arch, downloadURL)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		svc.refreshAgentMD5ByDownload(arch, downloadURL)
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return
	}
	md5hex := strings.TrimSpace(string(body))
	if len(md5hex) == 32 {
		svc.agentCache.set(arch, md5hex)
	}
}

// refreshAgentMD5ByDownload is the fallback: download the full binary and hash it.
func (svc *ControlPlaneService) refreshAgentMD5ByDownload(arch, downloadURL string) {
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	h := md5.New()
	if _, err := io.Copy(h, resp.Body); err != nil {
		return
	}
	svc.agentCache.set(arch, fmt.Sprintf("%x", h.Sum(nil)))
}
