# Node Agent

## Overview

The node agent (`cmd/node-agent`) runs on each proxy server. It:

- Registers with the control plane using a one-time bootstrap token
- Sends periodic heartbeats
- Pulls the latest rendered V2Ray config and applies it
- Collects per-user traffic stats and uploads usage snapshots

The agent binary is published to GitHub Releases on every push to `main`.

## Installation

Use the generated install script from the admin UI (**Bootstrap token → Copy install script**), or run manually:

```sh
export CONTROL_PLANE_URL=http://<your-control-plane>
export BOOTSTRAP_TOKEN=<token from admin UI>
export NODE_NAME=sg-1
export NODE_REGION=ap-southeast-1
export NODE_PUBLIC_HOST=sg-1.example.com
export AGENT_STATE_PATH=$PWD/.agent-state.json
export NODE_CONFIG_OUTPUT_PATH=$PWD/server.generated.json
go run ./cmd/node-agent
```

See [configuration.md](configuration.md) for all environment variables.

## Usage Stats Collection

To collect real per-credential traffic from V2Ray:

```sh
export NODE_USAGE_SOURCE=runtime
export NODE_USAGE_QUERY_SERVER=127.0.0.1:10085
export NODE_USAGE_QUERY_COMMAND="/usr/local/v2ray/v2ray api stats --server=127.0.0.1:10085 -json"
export NODE_USAGE_COLLECTION_INTERVAL_SECONDS=60
```

> **Important**: always include `-json` in `NODE_USAGE_QUERY_COMMAND` when using V2Ray 5.x.

The rendered node config enables the stats API on `127.0.0.1:10085` and records traffic under
`user>>>UUID>>>traffic>>>uplink/downlink`. Per-user stats require **both**:
- `policy.system.statsUserUplink/Downlink = true`
- `policy.levels."0".statsUserUplink/Downlink = true`

If you are upgrading an existing node, trigger a **Rebuild** from the admin UI to push the updated config.

## Auto-Update

The agent checks its own MD5 hash against the latest GitHub Release binary on every heartbeat.
On mismatch it:

1. Downloads the new binary to a temp file
2. Verifies the downloaded MD5
3. Atomically replaces the running binary (`os.Rename`)
4. Re-execs itself (`syscall.Exec`) — systemd keeps tracking the same PID

No manual intervention is needed after pushing a new release.

## Troubleshooting Usage Stats

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| `no user traffic counters` | No clients connected, or V2Ray just restarted | Connect a client, generate traffic, wait ~60s |
| `query command failed` | Wrong binary path or missing `--server` flag | Check `NODE_USAGE_QUERY_COMMAND` in `/etc/default/v2ray-platform-node-agent` |
| No `user>>>` entries in raw output | `policy.levels."0".statsUserUplink/Downlink` not set | Trigger **Rebuild** in admin UI |
| `[usage]` lines never appear in logs | `NODE_USAGE_SOURCE` is `disabled` or unset | Set `NODE_USAGE_SOURCE=runtime` and restart |
| Admin Usage page shows zeros | Snapshots stored with `member_id = NULL` | Verify member has a grant on the node; trigger **Rebuild** |

### Manual verification on a node

```sh
# Check environment config
grep -E 'NODE_USAGE' /etc/default/v2ray-platform-node-agent

# Query V2Ray stats directly
/usr/local/v2ray/v2ray api stats --server=127.0.0.1:10085 -json | python3 -m json.tool

# Watch agent logs
journalctl -u v2ray-platform-node-agent -f
```

Expected log sequence when working:

```
[usage] collecting (source=runtime cmd="...")
[usage] raw output (NNN bytes): {"stat":[...]}
[usage] parsed N user counter(s)
[usage] uploading N snapshot(s) to control plane
[usage] upload OK
```
