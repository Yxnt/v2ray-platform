---
id: decision-2026-06-18-default-to-ssh-server-deploys-via-ghcr
type: decision
title: Default to SSH server deploys via GHCR
status: active
created: 2026-06-18
updated: 2026-06-18
tags: [deploy, ghcr, ssh, cloudrun, docs]
---

# Default to SSH server deploys via GHCR

## 一句话结论
> 这个项目的默认真实部署路径是 GHCR 发布镜像后通过 SSH 部署到普通 Linux 服务器；Cloud Run 仅在用户明确要求 GCP 时才作为可选路径。

## 上下文链接
- 基于：[[knowledge/default-server-deploy-handoff/content]]
- 相关：[[decisions/2026-06-09-defer-real-aws-validation-for-local-cloudfront-closure]]
- 相关：[[knowledge/cloudfront-subscription-implementation-map/content]]

## Context

这次部署工作已经把 control-plane 的默认交付面从“可能走 GCP/Cloud Run”收敛成“GHCR 镜像 + SSH 服务器 + Docker Compose”。代码、CI、部署脚本、环境变量模板和 handoff 文档都已经围绕这个路径调整，并且真实目标机已经按这个方式完成过部署与数据库恢复。

同时，仓库仍保留 Cloud Run 脚本与文档，因为有些后续任务可能仍会显式要求 GCP 路径。

## Problem

如果不把默认部署路径明确记下来，后续 agent 很容易重新假设：

- 默认部署目标是 GCP / Cloud Run
- 需要在目标机上本地构建镜像
- 数据库恢复步骤需要另写临时命令

这样会让交接和自动部署再次分叉，增加重复探索成本。

## Alternatives Considered

- 继续把 Cloud Run 和 SSH 服务器视为并列默认路径：看起来更灵活，但会让自动化入口和文档叙事持续分叉
- 完全移除 Cloud Run：能减少复杂度，但会丢失现有可选能力，不符合“仅在显式要求时使用”的产品边界
- 以 SSH server + GHCR 为默认，Cloud Run 作为可选：既能固定默认交付面，也保留用户显式要求 GCP 时的路径

## Decision

以后在这个项目里，除非用户明确提出 GCP/Cloud Run，否则部署相关任务都应默认走：

1. GitHub Actions 发布 `ghcr.io/yxnt/v2ray-platform-control-plane`
2. `deploy/preflight-auto.sh` / `deploy/deploy-auto.sh`
3. SSH 连接目标 Linux 服务器
4. 通过 `deploy/docker-compose.server.yml` 启动 control-plane 和 Postgres
5. 如有需要，使用 `POSTGRES_RESTORE_DUMP` 恢复数据库备份

文档、handoff、CI 入口和后续自动化说明都应围绕这个默认面保持一致。

## Consequence

- 以后接手此仓库的 agent 不需要先猜 GCP 还是普通服务器
- `README.md`、`deploy/README.md`、`docs/llm-deploy-handoff.md`、`docs/operation-guide*.md` 应继续保持这个默认叙事
- Cloud Run 仍保留，但必须作为“用户显式要求时的可选路径”来描述

## 探索减负
- 下次可以少问什么：默认真实部署到底走 GCP 还是 SSH 服务器
- 下次可以少查什么：GHCR 镜像、服务端 compose、数据库恢复脚本分别在哪里
- 失效条件：如果后续正式废弃 SSH 服务器路径，或仓库把默认部署入口重新切回 GCP/Cloud Run
