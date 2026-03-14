# Configuration Reference

## Control Plane

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | _(none)_ | PostgreSQL DSN. If unset, uses in-memory store. |
| `BOOTSTRAP_ADMIN_EMAIL` | _(required)_ | Seeds the first admin account on first startup. |
| `BOOTSTRAP_ADMIN_PASSWORD` | _(required)_ | Password for the bootstrap admin. |
| `CONTROL_PLANE_SESSION_SECRET` | _(auto)_ | HMAC secret for signing admin session tokens. Rotate with care — existing sessions are invalidated. |
| `CONTROL_PLANE_ADMIN_TOKEN` | _(none)_ | Legacy static token fallback (`X-Admin-Token` header). Disabled when unset. |
| `PORT` | `8080` | HTTP listen port. |

## Node Agent

| Variable | Default | Description |
|----------|---------|-------------|
| `CONTROL_PLANE_URL` | _(required)_ | Base URL of the control plane. |
| `BOOTSTRAP_TOKEN` | _(required on first run)_ | One-time token to register the node. |
| `NODE_NAME` | _(required)_ | Unique node identifier. |
| `NODE_REGION` | _(optional)_ | Region label shown in Clash subscription. |
| `NODE_PUBLIC_HOST` | _(required)_ | Public hostname/IP of the node. |
| `AGENT_STATE_PATH` | `/var/lib/v2ray-platform-node-agent/state.json` | Persists registration state across restarts. |
| `NODE_CONFIG_OUTPUT_PATH` | `/usr/local/v2ray/config.json` | Where the rendered V2Ray config is written. |
| `NODE_USAGE_SOURCE` | `disabled` | Set to `runtime` to collect real traffic stats from V2Ray. |
| `NODE_USAGE_QUERY_SERVER` | `127.0.0.1:10085` | V2Ray stats gRPC endpoint. |
| `NODE_USAGE_QUERY_COMMAND` | _(auto)_ | Override stats query command. For V2Ray 5.x: `/usr/local/v2ray/v2ray api stats --server=127.0.0.1:10085 -json` |
| `NODE_USAGE_COLLECTION_INTERVAL_SECONDS` | `60` | How often to collect and upload usage snapshots. |
| `NODE_USAGE_INPUT_PATH` | _(none)_ | Legacy: file-based usage import fallback. |

## Notes

- PostgreSQL migrations run automatically at startup.
- Neon.tech and PgBouncer in transaction mode are supported via an internal `postgres-simple` driver (simple query protocol, no prepared statements).
- `BOOTSTRAP_ADMIN_EMAIL` / `BOOTSTRAP_ADMIN_PASSWORD` only take effect if no admin exists yet.
- Revoking a grant or deleting a member automatically triggers a config rebuild on affected nodes.
- Without `DATABASE_URL`, admin sessions are signed statelessly (valid until expiry; server-side logout-all is not available).
