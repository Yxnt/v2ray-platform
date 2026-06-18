---
id: knowledge-default-server-deploy-handoff
type: knowledge
title: Default server deploy handoff
status: active
created: 2026-06-18
updated: 2026-06-18
tags: [deploy, ghcr, ssh, handoff, docs]
---

# Default server deploy handoff

## Source

- [`README.md`](/Users/yxn/.codex/worktrees/ee58/v2ray-platform/README.md)
- [`deploy/README.md`](/Users/yxn/.codex/worktrees/ee58/v2ray-platform/deploy/README.md)
- [`docs/llm-deploy-handoff.md`](/Users/yxn/.codex/worktrees/ee58/v2ray-platform/docs/llm-deploy-handoff.md)
- [`deploy/deploy-auto.sh`](/Users/yxn/.codex/worktrees/ee58/v2ray-platform/deploy/deploy-auto.sh)
- [`deploy/deploy-server.sh`](/Users/yxn/.codex/worktrees/ee58/v2ray-platform/deploy/deploy-server.sh)
- [`deploy/preflight-auto.sh`](/Users/yxn/.codex/worktrees/ee58/v2ray-platform/deploy/preflight-auto.sh)
- [`deploy/preflight-server.sh`](/Users/yxn/.codex/worktrees/ee58/v2ray-platform/deploy/preflight-server.sh)
- [`deploy/docker-compose.server.yml`](/Users/yxn/.codex/worktrees/ee58/v2ray-platform/deploy/docker-compose.server.yml)
- [`deploy/server.env.example`](/Users/yxn/.codex/worktrees/ee58/v2ray-platform/deploy/server.env.example)
- [`docs/operation-guide.md`](/Users/yxn/.codex/worktrees/ee58/v2ray-platform/docs/operation-guide.md)
- [`docs/operation-guide-zh.md`](/Users/yxn/.codex/worktrees/ee58/v2ray-platform/docs/operation-guide-zh.md)

## Summary

这个仓库现在有一条明确的默认部署面：GHCR 发布 control-plane 镜像，再通过 SSH 部署到普通 Linux 服务器；Cloud Run 仅在用户明确要求 GCP 时才使用。

## Content

### Default deploy path

默认真实部署顺序是：

1. 推送到 `main` 后由 `.github/workflows/deploy.yml` 发布 GHCR 镜像
2. 通过 `deploy/preflight-auto.sh` 自动识别部署模式
3. 当设置了 `DEPLOY_HOST` 时，`deploy/deploy-auto.sh` 会选择 SSH server 路径
4. `deploy/deploy-server.sh` 上传仓库、生成 `.env.server`、按需恢复数据库备份、然后用 `docker compose` 启动服务

### Primary files

- 自动入口：`deploy/preflight-auto.sh`、`deploy/deploy-auto.sh`
- 服务端入口：`deploy/preflight-server.sh`、`deploy/deploy-server.sh`
- 服务端模板：`deploy/server.env.example`
- 运行时编排：`deploy/docker-compose.server.yml`
- LLM 交接：`docs/llm-deploy-handoff.md`

### Server deploy behavior

`deploy/deploy-server.sh` 负责：

- 校验 SSH、Docker、Docker Compose 和关键环境变量
- 上传仓库归档和 `.env.server`
- 启动 bundled Postgres
- 当设置了 `POSTGRES_RESTORE_DUMP` 时恢复数据库备份
- `docker compose pull` 最新 `CONTROL_PLANE_IMAGE`
- 启动 control-plane 并验证 `/healthz`

### Cloud Run status

Cloud Run 仍保留：

- `deploy/deploy-cloudrun.sh`
- `deploy/preflight-cloudrun.sh`
- `deploy/cloudrun.env.example`

但这些文件现在是可选路径，不应再被当成默认真实部署目标。

### Documentation constraint

`docs/operation-guide.md` 和 `docs/operation-guide-zh.md` 的前半部分已经增加了新的默认部署说明，但 `6. Node Management` / `6. 节点管理` 之后的原有运维、CloudFront、API、故障排查、维护、环境变量章节仍然是有效文档，不应在后续编辑中被截断。

### Pensieve state constraint

`.pensieve/state.md` 里的 `Project Root` 应保持稳定仓库路径 `/Users/yxn/Github/v2ray-platform`，不要改成临时 worktree 路径，否则会增加知识库路径漂移。

## When to Use

- 当任务涉及“默认怎么部署这个项目”
- 当另一个 agent 需要接手真实服务器部署
- 当需要确认 Cloud Run 现在是不是默认路径
- 当修改 operation guide 或 `.pensieve/state.md` 时

## 上下文链接
- 导致：[[decisions/2026-06-18-default-to-ssh-server-deploys-via-ghcr]]
- 相关：[[knowledge/cloudfront-subscription-implementation-map/content]]
