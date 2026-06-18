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

For installation, usage examples, prompts for any LLM, auto-generated bootstrap
values, and node cleanup notes, see:

- [docs/ai-deploy-guide.md](docs/ai-deploy-guide.md)

## Documentation

| Doc | Description |
|-----|-------------|
| [docs/configuration.md](docs/configuration.md) | All environment variables |
| [docs/node-agent.md](docs/node-agent.md) | Node agent setup, usage stats, auto-update, troubleshooting |
| [docs/subscription.md](docs/subscription.md) | Clash subscription, tiers, quota, proxy chain |
| [docs/roadmap.md](docs/roadmap.md) | Planned features |
| [deploy/README.md](deploy/README.md) | Deployment guide |
| [docs/ai-deploy-guide.md](docs/ai-deploy-guide.md) | AI-facing deploy guide, prompts, and skill usage |
| [docs/llm-deploy-handoff.md](docs/llm-deploy-handoff.md) | LLM-facing deployment handoff and CI contract |
| [deploy/server.env.example](deploy/server.env.example) | SSH server deploy env template |
| [skills/deploy-v2ray-platform/SKILL.md](skills/deploy-v2ray-platform/SKILL.md) | Codex deploy skill for this repository |

## Acknowledgements

Special thanks to the AI tools that made building this project faster and more enjoyable:

- **[Anthropic Claude](https://www.anthropic.com/claude)** — for intelligent code generation, architecture guidance, and thoughtful problem-solving throughout the development of this project.
- **[GitHub Copilot](https://github.com/features/copilot)** — for in-editor assistance, code completion, and the GitHub Copilot CLI agent that helped implement features end-to-end.
