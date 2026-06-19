---
id: knowledge-server-deploy-auto-bootstrap-info
type: knowledge
title: Server deploy auto-generates bootstrap info
status: active
created: 2026-06-19
updated: 2026-06-19
tags: [deploy, ssh, bootstrap, secrets, server]
---

# Server deploy auto-generates bootstrap info

## Source

- [`deploy/deploy-server.sh`](/Users/yxn/.codex/worktrees/b0ac/v2ray-platform/deploy/deploy-server.sh)
- [`deploy/preflight-server.sh`](/Users/yxn/.codex/worktrees/b0ac/v2ray-platform/deploy/preflight-server.sh)
- [`deploy/server.env.example`](/Users/yxn/.codex/worktrees/b0ac/v2ray-platform/deploy/server.env.example)
- [`skills/deploy-v2ray-platform/SKILL.md`](/Users/yxn/.codex/worktrees/b0ac/v2ray-platform/skills/deploy-v2ray-platform/SKILL.md)

## Summary

默认 SSH 服务器部署现在只强制要求 `DEPLOY_HOST`。当用户未提供管理员密码、session secret、admin token、内置 Postgres 密码或公网 URL 时，部署脚本会自动生成或推导这些值，并把最终结果写到服务器上的 `deploy-info.txt` 供用户自行查看。

## Content

### State transition

- 旧状态：服务器部署要求用户先手工填完大部分 bootstrap 和 secret 环境变量
- 新状态：只要具备 `DEPLOY_HOST` 和基本 SSH/Docker 前提，脚本就能补齐常见 bootstrap 值并继续部署

### Generated values

`deploy/deploy-server.sh` 现在会在以下场景自动生成或推导：

- `CONTROL_PLANE_PUBLIC_URL`：从 `DEPLOY_HOST` 推导 `https://<host>`
- `BOOTSTRAP_ADMIN_EMAIL`：默认 `admin@local.invalid`
- `BOOTSTRAP_ADMIN_PASSWORD`：随机生成
- `CONTROL_PLANE_SESSION_SECRET`：随机生成
- `CONTROL_PLANE_ADMIN_TOKEN`：随机生成
- `POSTGRES_PASSWORD`：未设置 `DATABASE_URL` 时随机生成

### User-visible result path

服务器部署完成后，脚本会在以下默认路径写入结果摘要文件：

- `/opt/v2ray-platform/deploy-info.txt`

文件内容包括：

- control-plane 访问地址
- 远端 healthcheck 地址
- 管理员邮箱
- 自动生成的管理员密码
- admin token
- `.env.server` 路径
- 使用内置 Postgres 时的数据库账号信息

文件权限会被收紧到 `600`。

### Ownership boundary

- 自动生成和结果落盘只适用于 SSH 服务器部署路径
- Cloud Run 路径仍要求显式环境变量，不共享这套服务器端落盘行为

### Verification signal

本地可复用的高信号验证是：

- `bash -n deploy/preflight-server.sh`
- `bash -n deploy/deploy-server.sh`
- `env -i PATH="$PATH" DEPLOY_HOST='user@example.com' bash deploy/preflight-server.sh`

最后一个命令应显示哪些值会被 `auto env` 自动补齐。

### Anti-pattern

- 不要在后续接手里继续要求用户预先填写所有 bootstrap secrets，除非任务明确要求自定义固定值
- 不要把这套“自动生成并写到服务器”的行为误套到 Cloud Run 流程

## When to Use

- 当任务是“帮我把项目部署到服务器”
- 当需要判断服务器部署最少必须提供哪些输入
- 当用户问自动生成的管理员密码或 token 去哪里看

## 上下文链接
- 基于：[[knowledge/default-server-deploy-handoff/content]]
- 相关：[[decisions/2026-06-18-default-to-ssh-server-deploys-via-ghcr]]
