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
8. Separately check whether the public entrypoint works over both `http://` and `https://` if the deploy is supposed to be internet-facing.

## Operational expectations

- The target host should have SSH access plus `docker` and `docker compose`.
- The deploy flow uploads the repo, generates `.env.server`, optionally restores `POSTGRES_RESTORE_DUMP`, pulls `CONTROL_PLANE_IMAGE`, and starts services with `deploy/docker-compose.server.yml`.
- When some bootstrap values are omitted, the deploy flow generates them automatically and records the effective values in the server-side deploy info file.
- `CLOUDFRONT_MASTER_KEY` is one of the generated bootstrap secrets. Keep it stable after CloudFront AWS credentials have been saved; changing it requires re-saving those credentials.
- The default image is `ghcr.io/yxnt/v2ray-platform-control-plane`.

## Node deployment follow-up

After the control-plane is healthy, the normal node path is still:

1. Create a bootstrap token from the admin API or UI.
2. Fetch `GET /install.sh` from the public control-plane URL, not from a
   loopback-only URL on the control-plane host.
3. Run the generated install script on the proxy node as root.
4. Verify on the node:
   - `systemctl is-active v2ray v2ray-platform-node-agent nginx`
   - `/etc/default/v2ray-platform-node-agent` contains the expected `CONTROL_PLANE_URL`
5. Verify on the control-plane:
   - `GET /api/admin/nodes` shows the node
   - the node reaches `status=online`

## Cleanup note

If you are reinstalling a node and see `unauthorized` from the node-agent after
bootstrap, remove the real state file before retrying:

```sh
systemctl stop v2ray-platform-node-agent v2ray
rm -f /var/lib/v2ray-platform/agent-state.json
systemctl start v2ray v2ray-platform-node-agent
```

This state file survives more often than the binaries and can keep an old
registration token alive.

## Do not do this by default

- Do not prefer Cloud Run unless the user explicitly asks for it.
- Do not replace the deploy scripts with handwritten remote commands when the existing entrypoints work.
- Do not claim success without checking the real service URL.
