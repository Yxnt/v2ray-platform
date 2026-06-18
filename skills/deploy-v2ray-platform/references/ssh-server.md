# SSH Server Deploy

This is the default real deployment path for this repository.

## Source of truth

- `docs/llm-deploy-handoff.md`
- `deploy/README.md`
- `deploy/server.env.example`
- `deploy/preflight-auto.sh`
- `deploy/deploy-auto.sh`
- `deploy/preflight-server.sh`
- `deploy/deploy-server.sh`

## Workflow

1. Load server env based on `deploy/server.env.example`.
2. Run `bash deploy/preflight-auto.sh`.
3. Run `bash deploy/deploy-auto.sh`.
4. If auto mode is ambiguous, run `bash deploy/preflight-server.sh` and then `bash deploy/deploy-server.sh`.
5. Verify `curl -fsSL "$CONTROL_PLANE_PUBLIC_URL/healthz"`.
6. If the database is new, verify the admin login path with `BOOTSTRAP_ADMIN_EMAIL` and `BOOTSTRAP_ADMIN_PASSWORD`.
7. Read the generated deploy summary on the server, which defaults to `/opt/v2ray-platform/deploy-info.txt`.

## Operational expectations

- The target host should have SSH access plus `docker` and `docker compose`.
- The deploy flow uploads the repo, generates `.env.server`, optionally restores `POSTGRES_RESTORE_DUMP`, pulls `CONTROL_PLANE_IMAGE`, and starts services with `deploy/docker-compose.server.yml`.
- When some bootstrap values are omitted, the deploy flow generates them automatically and records the effective values in the server-side deploy info file.
- The default image is `ghcr.io/yxnt/v2ray-platform-control-plane`.

## Do not do this by default

- Do not prefer Cloud Run unless the user explicitly asks for it.
- Do not replace the deploy scripts with handwritten remote commands when the existing entrypoints work.
- Do not claim success without checking the real service URL.
