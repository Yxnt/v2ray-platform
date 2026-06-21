---
id: knowledge-control-plane-secret-deploy-surface
type: knowledge
title: Control-plane secret deploy surface
status: active
created: 2026-06-21
updated: 2026-06-21
tags: [deploy, secrets, cloudfront, docs]
---

# Control-plane secret deploy surface

## Source

- [`deploy/deploy-server.sh`](../../../deploy/deploy-server.sh)
- [`deploy/preflight-server.sh`](../../../deploy/preflight-server.sh)
- [`deploy/deploy-cloudrun.sh`](../../../deploy/deploy-cloudrun.sh)
- [`deploy/server.env.example`](../../../deploy/server.env.example)
- [`deploy/cloudrun.env.example`](../../../deploy/cloudrun.env.example)
- [`docs/configuration.md`](../../../docs/configuration.md)
- [`docs/llm-deploy-handoff.md`](../../../docs/llm-deploy-handoff.md)

## Summary

When a new control-plane secret becomes required for a feature, update every deployment surface, not just the runtime config reader.

## Content

`CLOUDFRONT_MASTER_KEY` exposed a repeated deploy-surface pattern: the runtime could read the variable, but first deploys still missed it because the SSH deploy script, Cloud Run deploy script, env examples, preflight checks, and handoff docs were not updated together.

For SSH server deploys, bootstrap secrets that can be safely generated should be created in `deploy/deploy-server.sh`, advertised by `deploy/preflight-server.sh`, written to `.env.server`, and recorded in `deploy-info.txt` when operators need the effective value.

For Cloud Run deploys, the script does not have a durable server-side env file, so required secrets must be listed in `deploy/preflight-cloudrun.sh`, required by `deploy/deploy-cloudrun.sh`, included in the `--set-env-vars` payload, and documented in `deploy/cloudrun.env.example`.

Docs and AI handoffs are part of the deploy surface. Keep `docs/configuration.md`, `deploy/README.md`, `docs/ai-deploy-guide.md`, `docs/llm-deploy-handoff.md`, operation guides, and `skills/deploy-v2ray-platform/references/ssh-server.md` aligned.

## When to Use

- Before adding or renaming a control-plane environment variable.
- When an API works locally but deployed instances report a missing secret.
- When updating bootstrap/deploy docs for SSH server or Cloud Run targets.

## 上下文链接

- 基于：[[knowledge/default-server-deploy-handoff/content]]
- 基于：[[knowledge/server-deploy-auto-bootstrap-info/content]]
- 相关：[[decisions/2026-06-18-default-to-ssh-server-deploys-via-ghcr]]
