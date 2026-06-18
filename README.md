# v2ray-platform

Independent control plane and node agent for managing V2Ray proxy nodes.

## Features

- Admin web UI — nodes, members, groups, grants, tiers, alerts, audit logs, usage summaries
- Node agent — auto-registers, pulls config, reports usage, self-updates from GitHub Releases
- Clash subscription endpoint with live `Subscription-Userinfo` usage header
- Tier system — monthly or fixed bandwidth quotas per member
- Proxy chain — route nodes through relay nodes via Clash `dialer-proxy`
- Server-side BT blocking on all nodes (V2Ray routing rule)
- Config versioning with rollback (last 3 revisions per node)
- Node groups and group-based member authorization
- PostgreSQL persistence (auto-migrated); in-memory store for local dev
- Revocable admin sessions with logout / logout-all

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
docs/
```

## Quick start

```sh
export BOOTSTRAP_ADMIN_EMAIL=admin@example.com
export BOOTSTRAP_ADMIN_PASSWORD=change-me-now
go run ./cmd/control-plane
```

With PostgreSQL:

```sh
export DATABASE_URL='postgres://user:pass@127.0.0.1:5432/v2ray_platform?sslmode=disable'
export BOOTSTRAP_ADMIN_EMAIL=admin@example.com
export BOOTSTRAP_ADMIN_PASSWORD=change-me-now
go run ./cmd/control-plane
```

Open `http://localhost:8080` to access the admin UI. Create a bootstrap token from the UI,
then run the generated install script on each proxy server to register a node.

## Deployment default

The default production path is:

- publish `ghcr.io/yxnt/v2ray-platform-control-plane`
- deploy to a normal Linux server with `bash deploy/deploy-auto.sh`
- bootstrap proxy nodes from the admin UI with `GET /install.sh`

Cloud Run remains supported, but another LLM should not assume GCP unless the
user explicitly asks for it.

## Codex skill

This repository ships a deploy skill for Codex users at
[`skills/deploy-v2ray-platform/`](skills/deploy-v2ray-platform/).

### Install the skill

Copy the skill into your local Codex skills directory:

```sh
mkdir -p ~/.codex/skills
cp -R skills/deploy-v2ray-platform ~/.codex/skills/
```

After that, Codex can discover the skill by name: `deploy-v2ray-platform`.

### Use the skill

Ask Codex to deploy this project with prompts like:

- `Use deploy-v2ray-platform to deploy this project to my Linux server`
- `Use deploy-v2ray-platform and walk me through the SSH deploy`
- `Use deploy-v2ray-platform to deploy this to Cloud Run`

Expected behavior:

- default path: publish or reuse the GHCR image, then deploy over SSH with `deploy/preflight-auto.sh` and `deploy/deploy-auto.sh`
- Cloud Run path: used only when you explicitly ask for `Cloud Run`, `GCP`, or `gcloud`
- verification: the skill expects both a real service health check and, when relevant, separate public entry checks over `http://` and `https://`

### What gets auto-generated

For the SSH server path, you can omit several bootstrap values and let the
deploy flow generate them for you:

- `BOOTSTRAP_ADMIN_PASSWORD`
- `CONTROL_PLANE_SESSION_SECRET`
- `CONTROL_PLANE_ADMIN_TOKEN`
- `POSTGRES_PASSWORD` when `DATABASE_URL` is not set
- `CONTROL_PLANE_PUBLIC_URL` when it can be derived from `DEPLOY_HOST`

The effective values are written to a server-side file at
`/opt/v2ray-platform/deploy-info.txt` by default, so the user can review them
directly on the target host after deploy.

### Node deployment and cleanup notes

For node deployment, the normal path is still:

1. Create a bootstrap token from the control-plane
2. Fetch `GET /install.sh` from the public control-plane URL
3. Run the generated script on the proxy node as root
4. Verify the node shows up as `online` in the control-plane

If you are reinstalling a node and the node-agent reports `unauthorized` after
bootstrap, remove the real persisted state file before retrying:

```sh
systemctl stop v2ray-platform-node-agent v2ray
rm -f /var/lib/v2ray-platform/agent-state.json
systemctl start v2ray v2ray-platform-node-agent
```

That file is the real node-agent registration state and can survive earlier
manual cleanup.

## Documentation

| Doc | Description |
|-----|-------------|
| [docs/configuration.md](docs/configuration.md) | All environment variables |
| [docs/node-agent.md](docs/node-agent.md) | Node agent setup, usage stats, auto-update, troubleshooting |
| [docs/subscription.md](docs/subscription.md) | Clash subscription, tiers, quota, proxy chain |
| [docs/roadmap.md](docs/roadmap.md) | Planned features |
| [deploy/README.md](deploy/README.md) | Deployment guide |
| [docs/llm-deploy-handoff.md](docs/llm-deploy-handoff.md) | LLM-facing deployment handoff and CI contract |
| [deploy/server.env.example](deploy/server.env.example) | SSH server deploy env template |
| [skills/deploy-v2ray-platform/SKILL.md](skills/deploy-v2ray-platform/SKILL.md) | Codex deploy skill for this repository |

## Acknowledgements

Special thanks to the AI tools that made building this project faster and more enjoyable:

- **[Anthropic Claude](https://www.anthropic.com/claude)** — for intelligent code generation, architecture guidance, and thoughtful problem-solving throughout the development of this project.
- **[GitHub Copilot](https://github.com/features/copilot)** — for in-editor assistance, code completion, and the GitHub Copilot CLI agent that helped implement features end-to-end.
