# LLM Deployment Handoff

This repository supports both SSH server deploys and Cloud Run deploys. If
another LLM or automation agent picks up this repo, it should treat this file
plus `deploy/deploy-auto.sh` as the default deployment entrypoint and prefer
the SSH server path unless the user explicitly asks for Cloud Run.

## Goal

Deploy the control-plane to a normal Linux server with PostgreSQL persistence,
using a published `ghcr.io` image and no manual guesswork about the
build/push/deploy steps.

## Single source of truth

- CI entrypoint: `.github/workflows/deploy.yml`
- Default manual entrypoint: `deploy/deploy-auto.sh`
- Default preflight entrypoint: `deploy/preflight-auto.sh`
- Primary image pipeline: `.github/workflows/deploy.yml`
- SSH server entrypoint: `deploy/deploy-server.sh`
- SSH server env file: `deploy/server.env.example`
- Cloud Run entrypoint: `deploy/deploy-cloudrun.sh`
- Cloud Run env file: `deploy/cloudrun.env.example`
- Runtime config reference: `docs/configuration.md`

If these files disagree, fix them before deploying. Do not invent a parallel
deployment path.

## Required GitHub repository configuration

### Actions secrets

| Secret | Required | Notes |
|--------|----------|-------|
| none required for image publish | - | `.github/workflows/deploy.yml` uses the built-in `GITHUB_TOKEN` to push `ghcr.io/yxnt/v2ray-platform-control-plane` |

### Actions variables

No repository variables are required for the default image publish workflow.
Cloud Run-specific variables only matter when you intentionally choose the Cloud
Run deploy path.

## Deployment modes

### Mode A: publish the reusable image from GitHub Actions

1. Push to `main`.
2. GitHub Actions runs `.github/workflows/deploy.yml`.
3. The workflow builds the control-plane image and pushes:
   - `ghcr.io/yxnt/v2ray-platform-control-plane:latest`
   - `ghcr.io/yxnt/v2ray-platform-control-plane:sha-<shortsha>`

Future LLMs should assume the SSH server deploy path pulls this GHCR image by
default. Do not switch back to remote `docker build` unless GHCR is unavailable.

### Mode B: deploy to a normal Linux server over SSH

Use this when the final target is a VM or bare-metal host. This is the default
real deployment path for this repository.

```sh
cp deploy/server.env.example /tmp/v2ray-platform-server.env
# edit the copied file with real values
. /tmp/v2ray-platform-server.env

bash deploy/preflight-auto.sh
bash deploy/deploy-auto.sh
```

If you prefer repo-level entrypoints, the exact same sequence is:

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

### Mode C: deploy to Cloud Run only when explicitly requested

Use this only when the user explicitly wants GCP or Cloud Run:

1. `bash deploy/deploy-cloudrun.sh` completed without error in CI logs or your terminal.
2. `gcloud run services describe "$CLOUDRUN_SERVICE" --region "$GCP_REGION" --project "$GCP_PROJECT"` returns a URL.
3. `curl -fsSL "$SERVICE_URL/healthz"` succeeds.
4. The admin login page loads in a browser.
5. You can log in with `BOOTSTRAP_ADMIN_EMAIL` and `BOOTSTRAP_ADMIN_PASSWORD` if the database is new.

## Verification checklist

For SSH server deploys, verify all of the following:

1. `bash deploy/preflight-server.sh` completed without error.
2. `bash deploy/deploy-server.sh` completed without error.
3. `curl -fsSL "$CONTROL_PLANE_PUBLIC_URL/healthz"` succeeds.
4. The admin login page loads in a browser.
5. You can log in with `BOOTSTRAP_ADMIN_EMAIL` and `BOOTSTRAP_ADMIN_PASSWORD` if the database is new.

## Node bootstrap after control-plane deploy

Once the control-plane is healthy:

1. Open the admin UI.
2. Create or reuse a bootstrap token.
3. Add a node in the **Nodes** tab.
4. Run the generated `curl | bash` command on the proxy server as root.

The node bootstrap path is already implemented in `GET /install.sh`; do not
build a second installer unless the runtime design changes.

## Troubleshooting

- Auto preflight cannot choose a mode: load either `deploy/server.env.example` or `deploy/cloudrun.env.example`, then rerun `bash deploy/preflight-auto.sh`.
- SSH server preflight fails: run `bash deploy/preflight-server.sh` directly and fix the reported env vars, missing commands, SSH connectivity, or missing remote `docker compose`.
- GHCR image missing: inspect `.github/workflows/deploy.yml`, then push `ghcr.io/yxnt/v2ray-platform-control-plane` manually before retrying.
- Service starts but login fails: confirm `DATABASE_URL` is reachable and that the bootstrap admin credentials are only expected to seed a brand-new database.
- Nodes cannot register: verify `CONTROL_PLANE_URL` resolves publicly and that the generated install script can be fetched from `/install.sh`.
