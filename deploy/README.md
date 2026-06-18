# Deployment guide

## Recommended deployment path

For this repository's default production path, use:

- **A normal Linux server reachable over SSH** for the `control-plane`
- **The published image `ghcr.io/yxnt/v2ray-platform-control-plane:latest`**
- **Bundled Postgres in Docker Compose** or an external PostgreSQL DSN if you already have one

Why this is the default:

- Another LLM can deploy end-to-end with only SSH and Docker Compose.
- The same artifact can be reused across servers without rebuilding on the target host.
- The deploy script can optionally restore a PostgreSQL dump during first bring-up.

Cloud Run remains supported as an optional path, but it is no longer the
default assumption for automation agents.

## What gets deployed

This repository ships a container image for the `control-plane`.

Environment variables:

- `DATABASE_URL`
- `PORT` or `CONTROL_PLANE_LISTEN_ADDR`
- `BOOTSTRAP_ADMIN_EMAIL`
- `BOOTSTRAP_ADMIN_PASSWORD`
- `CONTROL_PLANE_SESSION_SECRET`
- `CONTROL_PLANE_ADMIN_TOKEN` for optional legacy fallback only
- `CONTROL_PLANE_ALERT_WEBHOOK_URL` for webhook alert delivery
- `CONTROL_PLANE_DB_MAX_OPEN_CONNS`
- `CONTROL_PLANE_DB_MAX_IDLE_CONNS`
- `CONTROL_PLANE_DB_CONN_MAX_LIFETIME_SECONDS`
- `AGENT_DOWNLOAD_URL` override the node-agent binary download URL (default: GitHub Releases latest)

The app automatically:

- listens on `:$PORT` when `PORT` is provided
- falls back to `:8080` locally
- uses PostgreSQL when `DATABASE_URL` is set
- runs embedded SQL migrations automatically on startup
- falls back to in-memory mode for local development
- records audit logs for core admin actions
- evaluates node/quota alerts and can deliver them by webhook
- runs lifecycle enforcement sweeps for expiry and quota policies

## Build the container locally

```sh
docker build -t v2ray-platform-control-plane .
```

## Deploy to a normal Linux server over SSH

The canonical handoff for other LLMs or automation agents is documented in
[`docs/llm-deploy-handoff.md`](../docs/llm-deploy-handoff.md). Treat that file,
`deploy/deploy-auto.sh`, and `deploy/server.env.example` as the default
deployment surface.

Start from the env template:

```sh
cp deploy/server.env.example /tmp/v2ray-platform-server.env
# edit the copied file with real values
. /tmp/v2ray-platform-server.env
```

Then run:

```sh
bash deploy/preflight-auto.sh
bash deploy/deploy-auto.sh
```

Equivalent `make` entrypoints:

```sh
make deploy-preflight
make deploy
```

The server deploy script will:
1. Validate local env vars, tools, and SSH connectivity.
2. Validate that the remote host has `docker` and `docker compose`.
3. Upload the repository and a generated `.env.server` file to the remote host.
4. Restore `POSTGRES_RESTORE_DUMP` into the bundled Postgres container when provided.
5. Pull `CONTROL_PLANE_IMAGE` from GHCR and start the control-plane container.
6. Verify `GET /healthz` on `CONTROL_PLANE_PUBLIC_URL`.

## Deploy to Cloud Run

There are two ways to deploy: **automatic via GitHub Actions** or **manual via script**.

For a ready-to-fill env template, start from
[`deploy/cloudrun.env.example`](./cloudrun.env.example).

### Option A — GitHub Actions (recommended)

Push to `main` and the workflow in `.github/workflows/deploy.yml` will:
1. Authenticate to Google Cloud with `GCP_SA_KEY`.
2. Validate the required repository secrets and variables.
3. Run `bash deploy/deploy-cloudrun.sh`.
4. Build, push, and deploy the control-plane image to Cloud Run.

**One-time GitHub setup** (Settings → Secrets and variables):

| Secret | Value |
|--------|-------|
| `GCP_SA_KEY` | JSON key of a GCP service account (see below) |
| `DATABASE_URL` | Neon or Cloud SQL connection string |
| `BOOTSTRAP_ADMIN_EMAIL` | First admin email |
| `BOOTSTRAP_ADMIN_PASSWORD` | First admin password (rotate after first login) |
| `CONTROL_PLANE_SESSION_SECRET` | Random 32+ char string (`openssl rand -hex 32`) |
| `CONTROL_PLANE_ALERT_WEBHOOK_URL` | _(optional)_ webhook for alerts |

**Repository variables** (Settings → Variables — not secrets):

| Variable | Default | Override example |
|----------|---------|-----------------|
| `GCP_PROJECT` | none | `my-gcp-project` |
| `GCP_REGION` | `asia-east1` | `us-central1` |
| `CLOUDRUN_SERVICE` | `v2ray-platform` | `my-cp` |
| `IMAGE` | derived by script | `us-central1-docker.pkg.dev/my-gcp-project/v2ray-platform/control-plane` |

**GCP service account minimum roles:**
```sh
gcloud projects add-iam-policy-binding YOUR_PROJECT \
  --member="serviceAccount:YOUR_SA@YOUR_PROJECT.iam.gserviceaccount.com" \
  --role="roles/run.admin"
gcloud projects add-iam-policy-binding YOUR_PROJECT \
  --member="serviceAccount:YOUR_SA@YOUR_PROJECT.iam.gserviceaccount.com" \
  --role="roles/iam.serviceAccountUser"
```

By default the deploy script uses Artifact Registry in the same GCP project and
region. If you override `IMAGE`, make sure the target registry is reachable from
Cloud Run and that your deploy identity can push to it.

### Option B — one-shot local script

```sh
cp deploy/cloudrun.env.example /tmp/v2ray-platform-cloudrun.env
# edit the copied file with real values
. /tmp/v2ray-platform-cloudrun.env

export GCP_PROJECT=your-project-id
export DATABASE_URL='postgres://user:pass@host/db?sslmode=require'
export BOOTSTRAP_ADMIN_EMAIL=admin@example.com
export BOOTSTRAP_ADMIN_PASSWORD=changeme
export CONTROL_PLANE_SESSION_SECRET=$(openssl rand -hex 32)

# optional overrides
export GCP_REGION=asia-east1
export CLOUDRUN_SERVICE=v2ray-platform

bash deploy/preflight-auto.sh
bash deploy/deploy-auto.sh
```

Equivalent `make` entrypoints:

```sh
make deploy-preflight
make deploy
```

The script will:
1. Create an Artifact Registry repository if needed.
2. Build the Docker image and push it tagged with the current git SHA + `latest`.
3. Deploy to Cloud Run with all required env vars.
4. Verify `GET /healthz` on the deployed service.
5. Print the service URL when done.

## First login and usage

After deploy:

1. Open the control-plane URL.
2. Log in with `BOOTSTRAP_ADMIN_EMAIL` and `BOOTSTRAP_ADMIN_PASSWORD`.
3. In the **Nodes** tab, click **＋ Add Node**, fill in the form, and copy the generated one-liner to bootstrap each node.
4. Manage members, grants, revocations, and audit logs in the built-in UI.
5. Review node/member usage summaries in the same UI after agents upload snapshots.
6. Use node/member search filters and batch actions directly in the built-in UI.

## Node bootstrap

### One-click bootstrap from the admin UI (recommended)

1. Open the admin UI and go to the **Nodes** tab.
2. Click **＋ Add Node** to expand the panel.
3. Fill in the node name, region, public host, and any tags.
4. Click **Generate install command**.
5. Copy the displayed `curl | bash` one-liner and run it on your server as root:

```sh
curl -fsSL "https://<your-cp>/install.sh?token=<TOKEN>&name=<NAME>&region=<REGION>" | bash
```

The script will:
- Download the `node-agent` binary from the GitHub Releases for this repository.
- Write `/etc/default/v2ray-platform-node-agent` with all required environment variables.
- Install and start the `v2ray-platform-node-agent` systemd service.

The `node-agent` binaries for `linux/amd64` and `linux/arm64` are published automatically
to GitHub Releases by the CI workflow on every push to `main` and on semver tags.

To override the download source (e.g. for air-gapped environments), set
`AGENT_DOWNLOAD_URL` on the control plane:

```sh
AGENT_DOWNLOAD_URL=https://your-mirror.example.com/node-agent-linux-amd64
```

### Manual bootstrap (alternative)

You can still run the legacy bootstrap script directly:

Important variables:

- `CONTROL_PLANE_URL`
- `BOOTSTRAP_TOKEN`
- `NODE_NAME`
- `NODE_REGION`
- `NODE_PUBLIC_HOST`
- `NODE_TAGS`
- `NODE_USAGE_SOURCE`
- `NODE_USAGE_QUERY_SERVER`
- `NODE_USAGE_COLLECTION_INTERVAL_SECONDS`
- `NODE_USAGE_QUERY_COMMAND`

Recommended real collector settings:

```sh
NODE_USAGE_SOURCE=runtime
NODE_USAGE_QUERY_SERVER=127.0.0.1:10085
NODE_USAGE_COLLECTION_INTERVAL_SECONDS=60
```

The generated V2Ray/Xray config now opens the stats API on `127.0.0.1:10085` and uses credential UUID as the stats identity, so the node-agent can collect accurate per-user uplink/downlink deltas every minute.

For existing nodes upgraded from the earlier file-bridge build, ensure the node gets one newly rendered config revision after deploying this version.

If your runtime package does not expose the expected default CLI, override the query command explicitly:

```sh
NODE_USAGE_QUERY_COMMAND='xray api statsquery --server=127.0.0.1:10085'
```

The older file-based bridge is still available with `NODE_USAGE_SOURCE=file` plus `NODE_USAGE_INPUT_PATH`.

## Local PostgreSQL smoke test

If you already have a local PostgreSQL image, you can validate the full flow with:

```sh
POSTGRES_IMAGE=docker.ispider.io/postgres:latest ./deploy/smoke-postgres.sh
```

If your local tag is different, override it:

```sh
POSTGRES_IMAGE=postgres:16 ./deploy/smoke-postgres.sh
```

If `55432` is already in use locally, also override the port:

```sh
POSTGRES_IMAGE=docker.ispider.io/postgres:latest POSTGRES_PORT=55434 SMOKE_PORT=18084 ./deploy/smoke-postgres.sh
```

## Future deployment backlog

These items are intentionally documented here for later production hardening rather than implemented now.

### 1. Safer public exposure

- put Cloud Run behind a custom domain and managed TLS
- optionally front it with Cloud Armor, reverse proxy rules, or identity-aware access
- remove reliance on the legacy admin token in internet-facing environments

### 2. Secret handling

- move bootstrap admin password and session secret to a managed secret store
- document rotation procedure and recovery procedure
- avoid keeping bootstrap credentials permanently in the deployed service config

### 3. Database operations

- add backup schedule guidance for Neon or external PostgreSQL
- document restore drill steps
- document retention strategy for usage snapshots and audit logs if data size grows

### 4. Monitoring

- add a real metrics sink for control-plane request rates and failures
- add alert rules for startup failures, migration failures, and prolonged node offline state
- document how to inspect node-agent logs separately from control-plane logs

### 5. Scaling considerations

- review SQL pool sizing against Neon connection limits
- add multi-instance migration strategy if the service stops being single-instance
- decide when PgBouncer or another pooler becomes necessary
