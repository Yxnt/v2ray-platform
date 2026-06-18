# Cloud Run Deploy

Use this path only when the user explicitly wants `Cloud Run`, `GCP`, or `gcloud`.

## Source of truth

- `docs/llm-deploy-handoff.md`
- `deploy/README.md`
- `deploy/cloudrun.env.example`
- `deploy/preflight-auto.sh`
- `deploy/deploy-auto.sh`
- `deploy/preflight-cloudrun.sh`
- `deploy/deploy-cloudrun.sh`

## Workflow

1. Load Cloud Run env based on `deploy/cloudrun.env.example`.
2. Run `bash deploy/preflight-auto.sh`.
3. Run `bash deploy/deploy-auto.sh`.
4. If you need direct mode-specific checks, use `bash deploy/preflight-cloudrun.sh` and `bash deploy/deploy-cloudrun.sh`.
5. Verify the deployed service URL and `curl -fsSL "$SERVICE_URL/healthz"`.

## Operational expectations

- Cloud Run is supported but not the default assumption for this repository.
- Confirm required GCP auth and env are present before deploy.
- The deployment should stay aligned with `.github/workflows/deploy.yml` and the Cloud Run script rather than creating a second path.
- The automatic server-side deploy info file behavior applies to the SSH server path, not Cloud Run.

## Do not do this by default

- Do not route a normal deploy request to Cloud Run unless the user explicitly asks for the GCP path.
