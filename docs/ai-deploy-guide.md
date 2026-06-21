# AI Deploy Guide

This guide explains how Codex or any other coding-focused LLM should deploy
this repository without inventing a parallel flow.

## Codex skill

This repository ships a deploy skill for Codex users at
[`skills/deploy-v2ray-platform/`](../skills/deploy-v2ray-platform/).

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

## Prompt for any LLM

Use the version that matches your starting point.

### Prompt for any LLM (repo already local)

```text
Install and deploy this v2ray-platform repository by following the repository's existing deployment flow instead of inventing a new one.

Requirements:
1. Read README.md, docs/llm-deploy-handoff.md, deploy/README.md, and skills/deploy-v2ray-platform/SKILL.md first.
2. Default to the SSH server deployment path unless I explicitly ask for Cloud Run or GCP.
3. Use the repository entrypoints deploy/preflight-auto.sh and deploy/deploy-auto.sh.
4. If bootstrap secrets are omitted, allow the deploy scripts to auto-generate them.
5. After control-plane deploy, read /opt/v2ray-platform/deploy-info.txt on the server and report the generated bootstrap information location.
6. If I also want a node deployed, create a bootstrap token from the control-plane and install the node via the public GET /install.sh path.
7. Verify separately:
   - remote or container-local /healthz
   - public http:// entry
   - public https:// entry
   - node status=online in the control-plane when a node is deployed
8. If a node reinstall reports unauthorized, clear /var/lib/v2ray-platform/agent-state.json before retrying.

Do not stop at code changes or local tests. Use the real target servers and report exactly what was verified and what is still broken or deferred.
```

### Prompt for any LLM (start from Git clone)

Repository URLs:

- SSH: `git@github.com:Yxnt/v2ray-platform.git`
- HTTPS: `https://github.com/Yxnt/v2ray-platform.git`

```text
Clone the v2ray-platform repository first, then install and deploy it by following the repository's existing deployment flow instead of inventing a new one.

Repository URLs:
- git@github.com:Yxnt/v2ray-platform.git
- https://github.com/Yxnt/v2ray-platform.git

Requirements:
1. Clone the repository locally and work from that checkout.
2. Read README.md, docs/llm-deploy-handoff.md, deploy/README.md, and skills/deploy-v2ray-platform/SKILL.md first.
3. Default to the SSH server deployment path unless I explicitly ask for Cloud Run or GCP.
4. Use the repository entrypoints deploy/preflight-auto.sh and deploy/deploy-auto.sh.
5. If bootstrap secrets are omitted, allow the deploy scripts to auto-generate them.
6. After control-plane deploy, read /opt/v2ray-platform/deploy-info.txt on the server and report the generated bootstrap information location.
7. If I also want a node deployed, create a bootstrap token from the control-plane and install the node via the public GET /install.sh path.
8. Verify separately:
   - remote or container-local /healthz
   - public http:// entry
   - public https:// entry
   - node status=online in the control-plane when a node is deployed
9. If a node reinstall reports unauthorized, clear /var/lib/v2ray-platform/agent-state.json before retrying.

Do not stop at code changes or local tests. Use the real target servers and report exactly what was verified and what is still broken or deferred.
```

## What gets auto-generated

For the SSH server path, you can omit several bootstrap values and let the
deploy flow generate them for you:

- `BOOTSTRAP_ADMIN_PASSWORD`
- `CONTROL_PLANE_SESSION_SECRET`
- `CONTROL_PLANE_ADMIN_TOKEN`
- `CLOUDFRONT_MASTER_KEY`
- `POSTGRES_PASSWORD` when `DATABASE_URL` is not set
- `CONTROL_PLANE_PUBLIC_URL` when it can be derived from `DEPLOY_HOST`

The effective values are written to a server-side file at
`/opt/v2ray-platform/deploy-info.txt` by default, so the user can review them
directly on the target host after deploy.

`CLOUDFRONT_MASTER_KEY` protects saved CloudFront AWS credentials. Keep the
generated value stable after credentials have been saved; replacing it requires
re-saving CloudFront credentials in the admin UI.

## Node deployment and cleanup notes

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
