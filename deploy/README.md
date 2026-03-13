# Deployment guide

## Recommended lean hosted setup

For the smallest operational footprint, use:

- **Google Cloud Run** for the `control-plane`
- **Neon** for PostgreSQL

Why this is the best fit:

- Cloud Run can scale to zero, so an infrequently used admin panel does not need an always-on VM.
- Neon has a free PostgreSQL tier and scales compute down when idle.
- You do not need to keep an extra management server running yourself.

## Free-tier reality check

“Free forever” app hosting is unstable across vendors, so the practical answer is:

- **Best current low-ops option:** Cloud Run + Neon
- **Not ideal for true free-only expectations:** Railway, Fly.io, Koyeb

Current caveats:

- **Cloud Run** has a free tier, but usually requires enabling billing on a Google Cloud account.
- **Neon** has a free PostgreSQL tier.
- **Railway** is no longer truly free long term; after the trial, pricing starts at `$1/month` on the free plan page.
- **Fly.io** is usage billed and requires a card on file.
- **Koyeb** pricing currently emphasizes paid usage and serverless billing; it is not the safest assumption for a stable free control plane.

If you want the most realistic “almost free” production-like path, choose **Cloud Run + Neon**.

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

## Deploy to Cloud Run

### 1. Create a Neon database

Create a Neon project and copy the connection string, for example:

```sh
postgres://user:password@ep-xxxxxx.us-east-1.aws.neon.tech/neondb?sslmode=require
```

### 2. Build and push the image

Example with Artifact Registry:

```sh
gcloud auth login
gcloud config set project YOUR_GCP_PROJECT
gcloud auth configure-docker

docker build -t gcr.io/YOUR_GCP_PROJECT/v2ray-platform-control-plane:latest .
docker push gcr.io/YOUR_GCP_PROJECT/v2ray-platform-control-plane:latest
```

### 3. Deploy to Cloud Run

```sh
gcloud run deploy v2ray-platform-control-plane \
  --image gcr.io/YOUR_GCP_PROJECT/v2ray-platform-control-plane:latest \
  --platform managed \
  --region asia-east1 \
  --allow-unauthenticated \
  --set-env-vars DATABASE_URL="$DATABASE_URL",BOOTSTRAP_ADMIN_EMAIL=admin@example.com,BOOTSTRAP_ADMIN_PASSWORD=change-me-now,CONTROL_PLANE_SESSION_SECRET=change-me-too
```

Notes:

- Set a strong `CONTROL_PLANE_SESSION_SECRET`.
- Use `BOOTSTRAP_ADMIN_PASSWORD` only for initial bootstrap; after the first admin is created, rotate or remove it from the deployed environment.
- Only set `CONTROL_PLANE_ADMIN_TOKEN` if you intentionally need the legacy header fallback; leave it unset for normal production deployments.
- Tune the SQL pool env vars conservatively if your PostgreSQL provider has strict connection caps.
- Cloud Run injects `PORT`, so no manual listen flag is needed.
- `--allow-unauthenticated` is acceptable for the current build because admin APIs are still token-protected, but in production you may still want Cloud Run IAM or a reverse proxy in front.

## First login and usage

After deploy:

1. Open the Cloud Run URL.
2. Log in with `BOOTSTRAP_ADMIN_EMAIL` and `BOOTSTRAP_ADMIN_PASSWORD`.
3. Create a bootstrap token.
4. Use that token in your node bootstrap flow.
5. Manage members, grants, revocations, and audit logs in the built-in UI.
6. Review node/member usage summaries in the same UI after agents upload snapshots.
7. Use node/member search filters and batch actions directly in the built-in UI.

## Node bootstrap

The node side still runs on your own V2Ray servers. Use:

```sh
deploy/init/bootstrap.sh
```

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
