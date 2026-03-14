# Roadmap

Features are listed in recommended implementation order for a self-operated service.

## 1. Security hardening

- Secret rotation guidance for `CONTROL_PLANE_SESSION_SECRET` and admin credentials
- HTTPS / reverse-proxy deployment defaults
- Optional IP allowlist or identity-aware proxy in front of admin UI
- Optional second admin role (`owner` / `operator`)

## 2. Member lifecycle

- Per-member expiry timestamps with automatic enforcement
- Traffic quota auto-disable / auto-re-enable policies
- Package / plan notes and metadata

## 3. Alerting

- Node offline after heartbeat timeout
- Sync failure after config change
- Traffic spike / zero-traffic anomaly detection
- Quota threshold warnings (80%, 95%)
- Webhook sink + Telegram / Feishu adapters

## 4. Backup and recovery

- Documented `pg_dump` / restore path
- Restore validation checklist
- Disaster-recovery runbooks: database lost, secret leaked, admin password lost

## 5. Multi-instance productionization

- Go SQL pool tuning
- Structured logs and request latency metrics
- Startup migration safety for multiple concurrent instances
- Cloud Run / serverless scaling notes
