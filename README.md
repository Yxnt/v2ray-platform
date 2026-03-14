# v2ray-platform

Independent control plane and node agent for managing many V2Ray nodes.

## Current MVP

- `control-plane`: admin and agent HTTP API
- `node-agent`: bootstrap registration, heartbeat, config pull, sync result reporting
- in-memory runtime store for local development
- PostgreSQL-backed runtime store when `DATABASE_URL` is set
- PostgreSQL schema in `migrations/0001_initial.sql`
- bootstrap script in `deploy/init/bootstrap.sh`
- usage snapshot upload and node/member usage summaries
- node/member search, filters, and batch admin actions
- revocable admin sessions with logout and logout-all
- automatic member expiry and quota enforcement sweeps
- alert evaluation for offline nodes, failed sync, and quota thresholds
- webhook alert delivery, export endpoints, and explicit config rebuild actions
- node groups, node-to-group membership, and member-to-group authorization

## Repository layout

```text
cmd/
  control-plane/
  node-agent/
internal/
  api/
  config/
  domain/
  render/
  store/
migrations/
deploy/
  init/
  systemd/
```

## Quick start

Start the control plane:

```sh
export BOOTSTRAP_ADMIN_EMAIL=admin@example.com
export BOOTSTRAP_ADMIN_PASSWORD=change-me-now
go run ./cmd/control-plane
```

Use PostgreSQL persistence:

```sh
export DATABASE_URL='postgres://postgres:postgres@127.0.0.1:5432/v2ray_platform?sslmode=disable'
export BOOTSTRAP_ADMIN_EMAIL=admin@example.com
export BOOTSTRAP_ADMIN_PASSWORD=change-me-now
go run ./cmd/control-plane
```

Deployment options and the recommended hosted setup are documented in `deploy/README.md`.

Login and create a bootstrap token:

```sh
curl -sS \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"change-me-now"}' \
  http://127.0.0.1:8080/api/admin/login

curl -sS \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <session token>' \
  -d '{"description":"aws-singapore-1","ttl_hours":24}' \
  http://127.0.0.1:8080/api/admin/bootstrap-tokens
```

Run an agent:

```sh
export CONTROL_PLANE_URL=http://127.0.0.1:8080
export BOOTSTRAP_TOKEN=<token from previous response>
export NODE_NAME=sg-1
export NODE_REGION=ap-southeast-1
export NODE_PUBLIC_HOST=sg-1.example.com
export AGENT_STATE_PATH=$PWD/.agent-state.json
export NODE_CONFIG_OUTPUT_PATH=$PWD/server.generated.json
go run ./cmd/node-agent
```

Create a member and grant access:

```sh
curl -sS \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <session token>' \
  -d '{"name":"alice","email":"alice@example.com"}' \
  http://127.0.0.1:8080/api/admin/members
```

Then use the returned `member_id` and the node id from `GET /api/admin/nodes`.

Revoke a grant or delete a member:

```sh
curl -sS \
  -X DELETE \
  -H 'Authorization: Bearer <session token>' \
  http://127.0.0.1:8080/api/admin/grants/<grant id>

curl -sS \
  -X DELETE \
  -H 'Authorization: Bearer <session token>' \
  http://127.0.0.1:8080/api/admin/members/<member id>
```

Search, filter, and batch admin operations:

```sh
curl -sS \
  -H 'Authorization: Bearer <session token>' \
  'http://127.0.0.1:8080/api/admin/nodes?q=sg&status=online&tag=edge'

curl -sS \
  -H 'Authorization: Bearer <session token>' \
  'http://127.0.0.1:8080/api/admin/members?q=alice'

curl -sS \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <session token>' \
  -d '{"grant_ids":["grant_x","grant_y"]}' \
  http://127.0.0.1:8080/api/admin/grants/batch-revoke

curl -sS \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <session token>' \
  -d '{"member_ids":["member_x","member_y"]}' \
  http://127.0.0.1:8080/api/admin/members/batch-delete
```

Collect real usage from local V2Ray/Xray stats:

```sh
export NODE_USAGE_SOURCE=runtime
export NODE_USAGE_QUERY_SERVER=127.0.0.1:10085
export NODE_USAGE_QUERY_COMMAND="/usr/local/v2ray/v2ray api stats --server=127.0.0.1:10085 -json"
export NODE_USAGE_COLLECTION_INTERVAL_SECONDS=60
go run ./cmd/node-agent
```

> **Important**: always include the `-json` flag in `NODE_USAGE_QUERY_COMMAND` when using V2Ray 5.x.
> Without it, output is protobuf text which still works via fallback parsing, but JSON is more
> reliable. The default install script sets this correctly.

The rendered node config enables the local stats API on `127.0.0.1:10085` and records per-user
traffic under `user>>>UUID>>>traffic>>>uplink/downlink`. The node-agent queries this every
`NODE_USAGE_COLLECTION_INTERVAL_SECONDS` seconds (default 60), computes the delta since the last
collection, and uploads snapshots to `/api/agent/usage`.

If you are upgrading an existing node, trigger a **Rebuild** from the admin UI so the node
receives a freshly rendered config with the stats API inbound and the correct `policy` settings.
Per-user stats require **both** `policy.system.statsUserUplink/Downlink = true` and
`policy.levels."0".statsUserUplink/Downlink = true`.

Then inspect aggregated usage:

```sh
curl -sS \
  -H 'Authorization: Bearer <session token>' \
  http://127.0.0.1:8080/api/admin/usage/nodes

curl -sS \
  -H 'Authorization: Bearer <session token>' \
  http://127.0.0.1:8080/api/admin/usage/members
```

### Tier system and subscription

Create a usage tier (bandwidth quota):

```sh
curl -sS \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer <session token>' \
  -d '{"name":"Standard","description":"50 GB/month","monthly_quota_gb":50}' \
  http://127.0.0.1:8080/api/admin/tiers
```

A tier can be assigned to a member from the Members panel in the admin UI. Members without a tier
have unlimited bandwidth.

Each member has a unique **subscription token** that never changes. Use it to generate a dynamic
Clash YAML subscription URL:

```
https://<control-plane-host>/api/sub/<subscription_token>
```

Users paste this URL into Clash (or any compatible client) under **Remote Profile**. Clicking
**Refresh** in the client fetches the latest server list automatically — no manual config
updates needed when you add or remove nodes.

### Node-agent auto-update

The node-agent checks its own MD5 hash against the latest GitHub Release binary on every
heartbeat. If a mismatch is detected, the agent:

1. Downloads the new binary to a temporary file.
2. Verifies the downloaded MD5 matches the expected value.
3. Atomically replaces the running binary (`os.Rename`).
4. Re-execs itself via `syscall.Exec` — the process image is replaced in-place so systemd
   continues to track the same PID.

No manual intervention is required after pushing a new release.

### Troubleshooting usage stats

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| `usage upload failed: no user traffic counters` | No clients currently connected, or V2Ray was just restarted (stats reset) | Connect a client and generate traffic; wait up to 60 s |
| `query command failed` | Wrong binary path or missing `--server` flag | Check `NODE_USAGE_QUERY_COMMAND` in `/etc/default/v2ray-platform-node-agent` |
| Raw output has no `user>>>` entries | `policy.levels."0".statsUserUplink/Downlink` not set | Trigger Rebuild in admin UI; the new config template is correct |
| `[usage]` lines never appear in `journalctl` | `NODE_USAGE_SOURCE` is `disabled` or unset | Set `NODE_USAGE_SOURCE=runtime` and restart |
| Admin Usage page shows zeros | Snapshots stored with `member_id = NULL` | Verify the member has been granted access to the node; trigger Rebuild |

To verify the stats pipeline manually on a node:

```sh
# 1. Check environment config
grep -E 'NODE_USAGE' /etc/default/v2ray-platform-node-agent

# 2. Query V2Ray stats directly
/usr/local/v2ray/v2ray api stats --server=127.0.0.1:10085 -json | python3 -m json.tool

# 3. Watch agent logs
journalctl -u v2ray-platform-node-agent -f
```

Expected log sequence when working correctly:
```
[usage] collecting (source=runtime cmd="...")
[usage] raw output (NNN bytes): {"stat":[...]}
[usage] parsed N user counter(s)
[usage] uploading N snapshot(s) to control plane
[usage] upload OK
```

## Notes

- Without `DATABASE_URL`, the control plane uses an in-memory store for local iteration and signs admin sessions statelessly so they remain valid across replicas.
- With `DATABASE_URL`, the control plane persists data in PostgreSQL.
- PostgreSQL migrations run automatically on startup.
- Neon.tech (and any PgBouncer in transaction mode) is supported via an internal `postgres-simple` driver that uses the simple query protocol, avoiding `prepared statement does not exist` errors.
- `BOOTSTRAP_ADMIN_EMAIL` and `BOOTSTRAP_ADMIN_PASSWORD` seed the first admin if it does not already exist.
- Revoking a grant or deleting a member automatically rebuilds the affected node config.
- `NODE_USAGE_SOURCE=runtime` enables real per-credential traffic collection from local V2Ray/Xray stats. Default is `disabled`.
- `NODE_USAGE_QUERY_SERVER` defaults to `127.0.0.1:10085`.
- `NODE_USAGE_QUERY_COMMAND` overrides the stats query command. For V2Ray 5.x, use: `/usr/local/v2ray/v2ray api stats --server=127.0.0.1:10085 -json`.
- `NODE_USAGE_COLLECTION_INTERVAL_SECONDS` defaults to `60`. The install script sets `600` (10 min) — lower it for faster feedback during testing.
- `NODE_USAGE_INPUT_PATH` remains available as a compatibility fallback for file-based usage imports.
- `CONTROL_PLANE_ADMIN_TOKEN` is now an explicit legacy fallback only; it is no longer enabled by default.
- With `DATABASE_URL`, the control plane persists revocable admin sessions and supports logout of the current session or all sessions. Without it, admin tokens are still valid until expiry, but server-side logout-all/session revocation is not available.
- Automatic lifecycle sweeps can expire members and suspend members that exceed quota.
- The built-in admin UI supports: node/member search, filters, batch member delete, batch grant revoke, node groups, group grants, alerts, exports, explicit rebuilds, audit logs, usage summaries, tier management, and Clash subscription URL generation.
- The V2Ray config renderer is intentionally narrow: one standard WS+VMess inbound template for the first cut.
- The node-agent binary is published to GitHub Releases on every push to `main`. The agent self-updates on MD5 mismatch — no manual binary deployment needed after the initial install.

## Prioritized backlog

Recommended order for the next production-focused features:

1. Security hardening
   - session invalidation/logout-all
   - secret rotation guidance
   - HTTPS/reverse-proxy defaults
   - optional IP allowlist or SSO

2. Member lifecycle controls
   - traffic quota
   - expiry date
   - manual suspend/disable
   - package/plan metadata

3. Alerting and operations
   - node offline alerts
   - sync failure alerts
   - abnormal traffic alerts

4. Backup and recovery
   - PostgreSQL backup/restore workflow
   - config/audit export
   - disaster-recovery notes

5. Node grouping and policy rollout
   - node groups / regions / lines
   - member-to-group authorization
   - staged rollout helpers

6. Batch config rebuild
   - explicitly trigger config regeneration for selected nodes after template/runtime upgrades

7. Multi-instance productionization
   - Cloud Run multi-instance considerations
   - connection pooling
   - stronger observability and metrics

## Detailed future backlog

This section is intentionally detailed so the remaining work can stay documented without being implemented now.

### 1. Security hardening

Why it matters:

- The current build is already usable, but it is still a first-cut admin plane.
- Once real users and real traffic are attached, the biggest risks move from feature gaps to session abuse, secret leakage, and weak operational guardrails.

Target outcome:

- Reduce the blast radius of leaked admin credentials.
- Make administrator access revocable.
- Make deployment defaults safer for internet exposure.

Recommended scope:

- Session invalidation
  - add a server-side session store or session version field
  - support logout-all for a single admin
  - support forced global session invalidation after secret rotation
  - expose recent session metadata in the admin UI if needed later

- Secret rotation
  - document rotation order for `CONTROL_PLANE_SESSION_SECRET`
  - document rotation order for bootstrap admin credentials
  - remove long-lived reliance on `CONTROL_PLANE_ADMIN_TOKEN` except emergency fallback
  - optionally introduce key versioning if zero-downtime session secret rotation becomes necessary

- Transport and exposure
  - standardize reverse-proxy or HTTPS entrypoint recommendations
  - document Cloud Run custom domain + TLS path
  - optionally add trusted proxy handling if deployed behind another edge
  - add guidance for IP allowlists or identity-aware proxy in front of the admin UI

- Authentication upgrades
  - optional second admin role model such as `owner` and `operator`
  - optional SSO later, for example Google Workspace or GitHub OAuth
  - optional password reset or invite-based admin creation flow

Suggested implementation order:

1. remove operational dependence on legacy admin token
2. add session invalidation and logout-all
3. document reverse-proxy / HTTPS best practices
4. add optional upstream identity integration

Not in scope for the first security pass:

- full multi-tenant isolation
- external RBAC policy engine
- hardware-backed key management integration

### 2. Member lifecycle controls

Why it matters:

- The current platform can create and revoke access, but it does not yet model commercial or operational lifecycle states.
- In practice, the next likely asks are “this friend expires next month”, “this user used too much traffic”, and “temporarily pause this account”.

Target outcome:

- Make member access manageable over time without manual cleanup.
- Allow the platform to become a lightweight service panel instead of only a topology manager.

Recommended scope:

- Member state
  - active
  - suspended
  - expired
  - archived

- Expiry controls
  - per-member expiry timestamp
  - optional per-grant expiry timestamp if node-specific access duration is needed
  - automatic disable workflow once expiry passes
  - UI indicators showing remaining time or expired status

- Traffic quota
  - monthly quota or total quota
  - enforce at member level first
  - optionally allow node-specific quota later
  - integrate usage summary view with quota display

- Package / plan metadata
  - human-readable plan name
  - notes such as “friends group”, “test account”, or “family plan”
  - soft limits for future billing or automation integration

- Enforcement design options
  - conservative approach: mark over-limit members and let admin revoke manually
  - stronger approach: automatically remove affected credentials from node config
  - recommended future path: support both, with auto-disable configurable

Suggested implementation order:

1. member status field
2. expiry timestamp and scheduled enforcement
3. quota fields and quota display
4. auto-disable / auto-re-enable policies

Key data model additions likely needed:

- new member status column
- member expiry timestamp
- quota bytes
- quota reset cycle metadata
- disable reason / lifecycle note

### 3. Alerting and operations

Why it matters:

- A control plane becomes much more valuable once it tells you what is wrong before users complain.
- The platform already has heartbeat, sync results, and usage data, which are enough to build a useful first alerting layer.

Target outcome:

- Detect broken nodes, failed config rollout, and unusual traffic conditions.
- Make it easy to plug into existing messaging tools.

Recommended scope:

- Node health alerts
  - node offline after heartbeat timeout
  - repeated degraded status
  - node registered but never synced successfully

- Config rollout alerts
  - sync failure after config change
  - repeated reload failure on the same node
  - config version drift where latest revision exists but node remains behind for too long

- Usage anomaly alerts
  - sudden traffic spike
  - zero traffic on nodes expected to be busy
  - optional quota threshold warnings such as 80 percent and 95 percent

- Delivery channels
  - webhook first
  - then Feishu / Telegram / email adapters as thin wrappers
  - recommend avoiding vendor-specific coupling in the core domain model

Suggested implementation order:

1. internal alert evaluation rules
2. webhook sink
3. UI page for recent alerts
4. provider-specific notification adapters

Operational note:

- Alerting should be idempotent and suppress duplicates; otherwise a single outage will spam your inbox.

### 4. Backup and recovery

Why it matters:

- The platform now contains operational state: members, grants, audit logs, and usage history.
- Even if nodes can be re-registered, losing control-plane data would still be painful.

Target outcome:

- Make it clear how to restore service after database loss or accidental changes.
- Keep recovery simple enough for a single-operator setup.

Recommended scope:

- Database backup
  - scheduled PostgreSQL logical dumps
  - retention policy guidance
  - restore verification checklist

- Configuration export
  - export members, nodes, grants, and audit logs
  - optional CSV/JSON export for admin review
  - optional encrypted archive workflow

- Recovery procedures
  - restore control-plane database
  - rotate bootstrap credentials after restore
  - confirm nodes reconnect and sync
  - confirm usage ingestion resumes

- Disaster-recovery docs
  - “database lost”
  - “session secret leaked”
  - “admin password lost”
  - “node runtime upgraded and usage collector broken”

Suggested implementation order:

1. documented `pg_dump` / restore path
2. export endpoints or scripts
3. restore validation checklist
4. optional automated backup integration

### 5. Node grouping and policy rollout

Why it matters:

- Once node count grows, per-node manual grant management becomes noisy.
- Operators usually end up thinking in groups such as “Hong Kong nodes”, “cheap nodes”, “stable nodes”, or “friends-only nodes”.

Target outcome:

- Make authorization and rollout operate at group level where appropriate.
- Preserve per-node overrides when needed.

Recommended scope:

- Node groups
  - group name
  - region/line/provider labels
  - optional display priority

- Group-based authorization
  - grant member to group instead of only node
  - materialize effective credentials onto matching nodes
  - keep direct node grants for exceptions

- Rollout helpers
  - staged template rollout by node group
  - maintenance mode per group
  - bulk selection by region or group in the admin UI

Suggested implementation order:

1. node group model and UI
2. group membership on nodes
3. group-level grants
4. rollout controls and maintenance windows

Risk note:

- Effective permission calculation becomes more complex once direct grants and group grants coexist, so the data model should be designed carefully before implementation.

### 6. Batch config rebuild

Why it matters:

- This is not urgent now, but it is useful after future template upgrades, runtime changes, or policy schema changes.
- Today config rebuild happens naturally when grants change; future operator workflows may need an explicit rebuild trigger.

Target outcome:

- Allow selected nodes to receive a newly rendered revision even if grants did not change.

Recommended scope:

- manual rebuild for one node
- batch rebuild for selected nodes
- optional rebuild by filter, for example by region or tag
- audit log for rebuild actions
- safe UX showing how many nodes will be affected

Suggested implementation order:

1. single-node rebuild endpoint
2. multi-select rebuild in UI
3. rebuild by filter or group

### 7. Multi-instance productionization

Why it matters:

- The current architecture is friendly to a single small deployment.
- If the control plane becomes more heavily used, runtime assumptions around connection pooling, observability, and statelessness need to be made explicit.

Target outcome:

- Make scaling predictable instead of accidental.
- Keep Cloud Run or similar serverless deployment viable under higher concurrency.

Recommended scope:

- Database connection management
  - tune Go SQL pool limits
  - document Neon / Postgres connection constraints
  - optionally place PgBouncer in front if concurrency grows

- Stateless admin plane assumptions
  - ensure session handling remains safe across replicas
  - avoid local-disk assumptions in the control plane
  - make health checks and startup migrations safe for multiple instances

- Observability
  - structured logs
  - request latency metrics
  - error counters
  - basic operational dashboard guidance

- Deployment notes
  - minimum instance settings
  - rollout strategy
  - migration-at-startup caveats for multi-instance deploys

Suggested implementation order:

1. SQL pool tuning and docs
2. structured operational metrics
3. multi-instance deployment notes
4. optional dedicated migration job path

## Practical recommendation

If you pause implementation now and come back later, the best next feature order is:

1. security hardening
2. member lifecycle controls
3. alerting
4. backup and recovery
5. node grouping
6. batch config rebuild
7. multi-instance productionization

That order gives the best balance of real operational value, risk reduction, and implementation effort for a small self-operated service.

## Acknowledgements

Special thanks to the AI tools that made building this project faster and more enjoyable:

- **[Anthropic Claude](https://www.anthropic.com/claude)** — for intelligent code generation, architecture guidance, and thoughtful problem-solving throughout the development of this project.
- **[GitHub Copilot](https://github.com/features/copilot)** — for in-editor assistance, code completion, and the GitHub Copilot CLI agent that helped implement features end-to-end.
