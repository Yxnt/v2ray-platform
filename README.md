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

```
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

## Documentation

| Doc | Description |
|-----|-------------|
| [docs/configuration.md](docs/configuration.md) | All environment variables |
| [docs/node-agent.md](docs/node-agent.md) | Node agent setup, usage stats, auto-update, troubleshooting |
| [docs/subscription.md](docs/subscription.md) | Clash subscription, tiers, quota, proxy chain |
| [docs/roadmap.md](docs/roadmap.md) | Planned features |
| [deploy/README.md](deploy/README.md) | Deployment guide |

## Acknowledgements

Special thanks to the AI tools that made building this project faster and more enjoyable:

- **[Anthropic Claude](https://www.anthropic.com/claude)** — for intelligent code generation, architecture guidance, and thoughtful problem-solving throughout the development of this project.
- **[GitHub Copilot](https://github.com/features/copilot)** — for in-editor assistance, code completion, and the GitHub Copilot CLI agent that helped implement features end-to-end.
