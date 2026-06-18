---
name: deploy-v2ray-platform
description: Use when the user wants to deploy, publish, release,上线, or roll out this v2ray-platform project. Covers the repository's default GHCR plus SSH server deployment path, plus the optional Cloud Run path when the user explicitly asks for GCP or Cloud Run.
---

# Deploy V2Ray Platform

Use this skill for deployment work in this repository. Keep the workflow aligned
with the current `main` branch and the repository's existing deploy scripts.

## Default path

Unless the user explicitly asks for `Cloud Run`, `GCP`, or `gcloud`, default to:

1. Publish or reuse `ghcr.io/yxnt/v2ray-platform-control-plane`
2. Deploy to a normal Linux server over SSH
3. Use `docker compose` on the target host

Do not invent a parallel deployment flow when these repository entrypoints exist:

- `docs/llm-deploy-handoff.md`
- `deploy/preflight-auto.sh`
- `deploy/deploy-auto.sh`
- `deploy/server.env.example`
- `deploy/docker-compose.server.yml`

## First reads

Before taking deployment actions, read:

1. `docs/llm-deploy-handoff.md`
2. `deploy/README.md`
3. The mode-specific reference below

Then inspect the exact scripts you will rely on before executing them.

## Mode selection

Choose the deployment path from the user's request and current env:

- Default: SSH server deploy via GHCR
- Only if the user explicitly asks for GCP or Cloud Run: Cloud Run deploy

Read exactly one reference file:

- SSH server path: `references/ssh-server.md`
- Cloud Run path: `references/cloud-run.md`

## Shared rules

- Treat `docs/llm-deploy-handoff.md` as the deployment source of truth.
- Prefer `deploy/preflight-auto.sh` and `deploy/deploy-auto.sh` over ad hoc shell.
- Do not switch the default path back to remote `docker build` unless GHCR is unavailable.
- Verify with the real target URL after deploy. At minimum, check `/healthz`.
- Treat remote health and public entry checks as separate verification layers:
  `127.0.0.1` or container-local `/healthz` proves the service is running, but
  public `http://` and `https://` checks prove whether the outside entrypoint is
  actually usable.
- If the deployment depends on secrets or env files that are not present, stop and report the missing inputs clearly.
- If the repo docs and scripts disagree, fix that drift before claiming deployment is ready.
- For SSH deploys, prefer the built-in behavior that can auto-generate missing
  bootstrap secrets and write the effective values to a server-side deploy info
  file for the user to inspect.
- For node reinstall or cleanup tasks, remember that the real node-agent state
  path is `/var/lib/v2ray-platform/agent-state.json`; removing only binaries or
  unit files may still leave an old registration token behind.

## Output expectations

When using this skill, the final result should state:

- which deployment mode was chosen
- which script entrypoints were used
- what was verified
- what remains deferred, if anything
