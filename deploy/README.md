# Packaging and bootstrap guide

This repository keeps two distributable artifacts current:

- a `control-plane` container image published to GitHub Container Registry
- `node-agent` binaries published to GitHub Releases

## Control-plane image

Build locally:

```sh
docker build -t v2ray-platform-control-plane .
```

Publish automatically:

- Push to `main`
- GitHub Actions workflow `.github/workflows/deploy.yml` builds and pushes:

```text
ghcr.io/<your-github-owner>/v2ray-platform-control-plane
```

Published tags:

- `latest`
- `${GITHUB_SHA}`

No extra registry secret is required because the workflow uses the built-in
`GITHUB_TOKEN`.

Useful runtime variables for the control plane:

- `DATABASE_URL`
- `PORT` or `CONTROL_PLANE_LISTEN_ADDR`
- `BOOTSTRAP_ADMIN_EMAIL`
- `BOOTSTRAP_ADMIN_PASSWORD`
- `CONTROL_PLANE_SESSION_SECRET`
- `CONTROL_PLANE_ADMIN_TOKEN`
- `CONTROL_PLANE_ALERT_WEBHOOK_URL`
- `CONTROL_PLANE_DB_MAX_OPEN_CONNS`
- `CONTROL_PLANE_DB_MAX_IDLE_CONNS`
- `CONTROL_PLANE_DB_CONN_MAX_LIFETIME_SECONDS`
- `AGENT_DOWNLOAD_URL`

## Node-agent release artifacts

Pushes to `main` and semver tags trigger `.github/workflows/release.yml`, which
builds:

- `node-agent-linux-amd64`
- `node-agent-linux-arm64`
- matching `.md5` files

These artifacts are attached to GitHub Releases and are the default source used
by the node bootstrap flow.

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

## Local smoke test

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
