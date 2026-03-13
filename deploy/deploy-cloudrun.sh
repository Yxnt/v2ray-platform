#!/usr/bin/env bash
# deploy/deploy-cloudrun.sh
# One-shot script to build, push, and deploy the control-plane to Cloud Run.
#
# Prerequisites:
#   - gcloud CLI installed and authenticated (gcloud auth login)
#   - Docker installed and logged in to the registry you choose
#   - The following environment variables set (see below)
#
# Usage:
#   export GCP_PROJECT=your-project-id
#   export DATABASE_URL='postgres://...'
#   export BOOTSTRAP_ADMIN_EMAIL=admin@example.com
#   export BOOTSTRAP_ADMIN_PASSWORD=changeme
#   export CONTROL_PLANE_SESSION_SECRET=$(openssl rand -hex 32)
#   bash deploy/deploy-cloudrun.sh

set -euo pipefail

# ── required ─────────────────────────────────────────────────────────────────
: "${GCP_PROJECT:?GCP_PROJECT is required}"
: "${DATABASE_URL:?DATABASE_URL is required}"
: "${BOOTSTRAP_ADMIN_EMAIL:?BOOTSTRAP_ADMIN_EMAIL is required}"
: "${BOOTSTRAP_ADMIN_PASSWORD:?BOOTSTRAP_ADMIN_PASSWORD is required}"
: "${CONTROL_PLANE_SESSION_SECRET:?CONTROL_PLANE_SESSION_SECRET is required}"

# ── optional (with defaults) ──────────────────────────────────────────────────
GCP_REGION="${GCP_REGION:-asia-east1}"
CLOUDRUN_SERVICE="${CLOUDRUN_SERVICE:-v2ray-platform}"
# Image registry: defaults to Artifact Registry in the same project/region.
# Override IMAGE to use gcr.io or ghcr.io if preferred.
IMAGE="${IMAGE:-${GCP_REGION}-docker.pkg.dev/${GCP_PROJECT}/v2ray-platform/control-plane}"
ALERT_WEBHOOK_URL="${CONTROL_PLANE_ALERT_WEBHOOK_URL:-}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# ── make sure Artifact Registry repository exists ────────────────────────────
if [[ "${IMAGE}" == *"docker.pkg.dev"* ]]; then
  REPO_NAME="v2ray-platform"
  echo "Ensuring Artifact Registry repository '${REPO_NAME}' exists..."
  gcloud artifacts repositories describe "${REPO_NAME}" \
    --location="${GCP_REGION}" --project="${GCP_PROJECT}" >/dev/null 2>&1 || \
  gcloud artifacts repositories create "${REPO_NAME}" \
    --repository-format=docker \
    --location="${GCP_REGION}" \
    --project="${GCP_PROJECT}" \
    --description="v2ray-platform control-plane images"
  gcloud auth configure-docker "${GCP_REGION}-docker.pkg.dev" --quiet
fi

# ── build & push ──────────────────────────────────────────────────────────────
GIT_SHA="$(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo "local")"
TAG="${IMAGE}:${GIT_SHA}"
LATEST="${IMAGE}:latest"

echo "Building control-plane image..."
docker build -t "${TAG}" -t "${LATEST}" "${REPO_ROOT}"

echo "Pushing image..."
docker push "${TAG}"
docker push "${LATEST}"

# ── build env-vars string ─────────────────────────────────────────────────────
ENV_VARS="DATABASE_URL=${DATABASE_URL}"
ENV_VARS+=",BOOTSTRAP_ADMIN_EMAIL=${BOOTSTRAP_ADMIN_EMAIL}"
ENV_VARS+=",BOOTSTRAP_ADMIN_PASSWORD=${BOOTSTRAP_ADMIN_PASSWORD}"
ENV_VARS+=",CONTROL_PLANE_SESSION_SECRET=${CONTROL_PLANE_SESSION_SECRET}"
if [[ -n "${ALERT_WEBHOOK_URL}" ]]; then
  ENV_VARS+=",CONTROL_PLANE_ALERT_WEBHOOK_URL=${ALERT_WEBHOOK_URL}"
fi

# ── deploy to Cloud Run ───────────────────────────────────────────────────────
echo "Deploying '${CLOUDRUN_SERVICE}' to Cloud Run (${GCP_REGION})..."
gcloud run deploy "${CLOUDRUN_SERVICE}" \
  --image "${TAG}" \
  --platform managed \
  --region "${GCP_REGION}" \
  --project "${GCP_PROJECT}" \
  --allow-unauthenticated \
  --set-env-vars "${ENV_VARS}"

SERVICE_URL="$(gcloud run services describe "${CLOUDRUN_SERVICE}" \
  --platform managed --region "${GCP_REGION}" --project "${GCP_PROJECT}" \
  --format 'value(status.url)')"

echo ""
echo "✓ Deployed: ${SERVICE_URL}"
echo ""
echo "Next steps:"
echo "  1. Open ${SERVICE_URL} to verify the admin UI loads."
echo "  2. Log in with ${BOOTSTRAP_ADMIN_EMAIL}."
echo "  3. Add nodes via the '+ Add Node' panel in the Nodes tab."
