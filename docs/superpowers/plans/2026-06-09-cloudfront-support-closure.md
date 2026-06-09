# CloudFront Support Closure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring the current CloudFront support branch to a verified local logic closure that safely supports both direct and CloudFront Clash subscriptions, with real AWS account validation deferred unless explicitly requested.

**Architecture:** Treat `docs/superpowers/specs/2026-06-05-cloudfront-subscription-design.md` as the product contract and `docs/superpowers/plans/2026-06-05-cloudfront-subscription-implementation.md` as the original build plan. This closure plan starts from the current dirty branch, verifies what already exists, hardens AWS request-shape compatibility through local tests, and records production AWS validation as deferred follow-up work.

**Tech Stack:** Go, net/http, PostgreSQL migrations, embedded admin UI, AWS CloudFront REST/XML APIs, table-driven tests, runtime curl smoke checks

---

## Claude Code Entry Point

This is the preferred entry point when the repository already contains CloudFront-related diffs.

Before changing code, Claude Code should:

- read `AGENTS.md` and `CLAUDE.md`
- read `docs/superpowers/specs/2026-06-05-cloudfront-subscription-design.md`
- read `docs/superpowers/plans/2026-06-05-cloudfront-subscription-implementation.md`
- inspect `git status --short`
- compare the current implementation to this closure plan

Then continue from the first task below that is failing, missing, or not yet verified. Do not reimplement completed tasks simply because the original implementation plan lists them.

## Current Branch Snapshot

This branch is not a greenfield build. Before changing anything, inspect the current worktree and preserve unrelated changes:

```bash
git status --short
git diff -- internal/cloudfront internal/api/controlplane.go internal/api/web/index.html internal/store
```

Expected current shape:

- `internal/cloudfront/aws_client.go` exists and implements `ListDistributions`, `GetDistribution`, `CreateDistribution`, and ETag-aware `ApplyDistributionRoutes`.
- `internal/api/controlplane.go` builds CloudFront AWS clients from encrypted DB config at request time.
- Admin endpoints exist for config, discovery, bind, plan, sync, and both direct/CloudFront YAML export.
- Admin UI has a CloudFront section with credential save, distribution scan/select, bind, plan, sync, and gated CloudFront subscription actions.
- Focused tests and full `go test ./...` pass locally.

Current verification status:

- Local focused tests: verified on 2026-06-09 with `GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/cloudfront ./internal/api ./internal/store`.
- Local full tests: verified on 2026-06-09 with `GOCACHE=/private/tmp/v2ray-platform-go-build go test ./...`.
- Runtime smoke: verified on 2026-06-09 by starting `cmd/control-plane`, checking CloudFront UI controls, confirming default config response, and confirming distribution discovery returns a clear not-configured error before setup.
- Live-state safety: verified on 2026-06-09 with tests proving plan/sync read the live AWS distribution state, unmanaged conflicts stop before mutation, AWS apply refuses unmanaged route overwrites, and changing distribution metadata clears stale successful-sync markers.
- Credential safety: verified on 2026-06-09 with API tests proving saved AWS credentials are encrypted at rest, config responses return only masked access key data, plaintext secrets are not returned, and missing `CLOUDFRONT_MASTER_KEY` fails explicitly.
- Route rewrite safety: verified on 2026-06-09 with tests proving CloudFront create/sync creates, updates, publishes, and associates the platform-managed `v2ray-platform-route-rewrite` CloudFront Function so external `/{route_key}` requests are rewritten to each node's actual `/{node.name}` WebSocket path before origin fetch.
- Route-set safety: verified on 2026-06-09 with tests proving CloudFront binding changes clear stale successful-sync markers, public `clash-cf.yaml` returns unavailable until re-sync, and missing node `public_host` becomes a planner conflict instead of a silent skip.
- Managed create UX: verified on 2026-06-09 with an admin UI contract test proving Managed mode can create a new distribution when no discovered distribution is selected.
- AWS request compatibility: verified on 2026-06-09 with tests proving CloudFront requests are signed with the global endpoint region `us-east-1`, distribution discovery follows paginated `NextMarker` responses, and CloudFront Function/distribution updates use the expected XML request shapes.
- Credential update safety: verified on 2026-06-09 with tests proving partial AK/SK updates and session-token-only updates are rejected without clearing stored credentials.
- Export drift gating: verified on 2026-06-09 with tests proving public `clash-cf.yaml` becomes unavailable when a plan records `drifted` or `conflict` state after a previous successful sync.
- AWS mutation ownership safety: verified on 2026-06-09 with tests proving the AWS client refuses unmanaged route overwrites and unmanaged route removals before issuing distribution update requests.
- Real AWS status: deferred on 2026-06-09 by the current objective. Local logic closure does not require a real AWS account, but production AWS API acceptance still requires a later scan/bind/create/sync run against real CloudFront.
- Custom entry host status: local rendering trusts the configured custom entry host after CloudFront sync gates pass. Alias/DNS validation against a real distribution remains deferred production validation.

## Non-Negotiable Product Rules

- Keep direct subscription behavior backward compatible: `GET /sub/{token}/clash.yaml` and `GET /api/admin/members/{memberID}/clash.yaml` must not change unexpectedly.
- Keep CloudFront as control-plane phase-one work only. Do not change node-agent registration, heartbeat, usage reporting, runtime add/remove user flows, or node config generation unless a new task explicitly asks for it.
- Use stable node `route_key` for CloudFront paths. Never derive CloudFront paths from `node.name`.
- Keep the platform-managed CloudFront Function `v2ray-platform-route-rewrite` attached to platform-managed route behaviors. CloudFront behavior matching selects the origin by `route_key`, then this function rewrites the URI to the node's direct WebSocket path.
- Treat live AWS distribution state as authoritative for `plan` and `sync`; `origins_json` is only a scan cache.
- Only mutate CloudFront origins and cache behaviors that are positively identified as platform-managed.
- Changing the bound distribution must invalidate old sync success metadata.
- Changing CloudFront bindings or the desired route set must invalidate old sync success metadata.
- Missing node `public_host` must surface as a CloudFront plan/sync conflict, not as a skipped route.
- CloudFront AWS REST requests must be signed for `us-east-1`/`cloudfront` even if the stored UI region differs.
- Distribution discovery must follow all paginated CloudFront list responses.
- Credential updates must reject session-token-only changes unless AK/SK are also supplied.
- Known `drifted` or `conflict` state must disable CloudFront subscription export until sync resolves it.
- Do not expose usable CloudFront YAML until CloudFront is enabled, a distribution is bound or created, and at least one sync has succeeded.
- Real AWS credentials/network are not required for local logic closure unless the task explicitly asks for them. Do not claim production AWS API acceptance without a real AWS scan/bind/create/sync validation.
- Custom entry host alias/DNS validation is not part of local closure. Treat it as production validation unless a later task explicitly requires platform-side enforcement.

## File Structure

### Files to inspect before implementation

- `AGENTS.md`
  - Future-agent workstream rules.
- `CLAUDE.md`
  - Claude Code mirror of the same workstream rules.
- `docs/superpowers/specs/2026-06-05-cloudfront-subscription-design.md`
  - Approved product and architecture contract.
- `docs/superpowers/plans/2026-06-05-cloudfront-subscription-implementation.md`
  - Original implementation plan.
- `internal/cloudfront/aws_client.go`
  - Real CloudFront XML/REST client.
- `internal/cloudfront/plan.go`
  - Desired-vs-remote route reconciliation.
- `internal/cloudfront/scan.go`
  - Remote distribution state reconstruction.
- `internal/cloudfront/sync.go`
  - Plan execution and sync status updates.
- `internal/api/controlplane.go`
  - Admin/public API handlers and request-time CloudFront client wiring.
- `internal/api/web/index.html`
  - Admin UI flow.
- `internal/store/*`
  - CloudFront config, bindings, plan, and sync metadata persistence.
- `migrations/0012_cloudfront_route_keys_and_config.sql`
  - Stable route key and CloudFront config schema.

### Files likely to change during closure

- `internal/cloudfront/aws_client.go`
- `internal/cloudfront/aws_client_test.go`
- `internal/api/controlplane.go`
- `internal/api/controlplane_test.go`
- `internal/api/web/index.html`
- `AGENTS.md`
- `CLAUDE.md`
- `docs/superpowers/plans/2026-06-09-cloudfront-support-closure.md`

## Task 1: Confirm The Branch Starts From A Passing Local Baseline

**Files:**
- Inspect: `internal/cloudfront/*`
- Inspect: `internal/api/controlplane.go`
- Inspect: `internal/api/web/index.html`
- Inspect: `internal/store/*`

- [ ] **Step 1: Read the workstream docs**

Run:

```bash
sed -n '1,220p' AGENTS.md
sed -n '1,220p' CLAUDE.md
sed -n '1,260p' docs/superpowers/specs/2026-06-05-cloudfront-subscription-design.md
sed -n '1,260p' docs/superpowers/plans/2026-06-05-cloudfront-subscription-implementation.md
```

Expected: Both agent instruction files tell workers to preserve direct subscriptions, use stable `route_key`, avoid node-agent changes, and mutate only platform-managed CloudFront resources.

- [ ] **Step 2: Run focused CloudFront verification**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/cloudfront ./internal/api ./internal/store
```

Expected: PASS.

- [ ] **Step 3: Run full regression verification**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./...
```

Expected: PASS.

- [ ] **Step 4: If tests fail, stop and debug before adding scope**

Use `superpowers:systematic-debugging` before writing fixes. Do not add new behavior while the existing CloudFront branch is red.

## Task 2: Verify Real AWS XML Compatibility

**Files:**
- Modify: `internal/cloudfront/aws_client.go`
- Modify: `internal/cloudfront/aws_client_test.go`

- [ ] **Step 1: Confirm managed distribution creation includes a cache policy**

Inspect `newManagedDistributionConfig` in `internal/cloudfront/aws_client.go`.

Required invariant:

```go
const managedCachingDisabledPolicyID = "4135ea2d-6df8-44a3-9df3-4b5a84be39ad"
```

Both `DefaultCacheBehavior` and every platform-managed route `CacheBehavior` created by this project must set:

```go
CachePolicyID: managedCachingDisabledPolicyID
```

Expected: Managed CloudFront distributions use AWS managed CachingDisabled policy, because proxy/websocket traffic must not be cached and CloudFront requires either `CachePolicyId` or legacy `ForwardedValues`.

- [ ] **Step 2: Confirm custom origins include SSL protocol settings**

Inspect `defaultCustomOriginConfig`.

Required implementation:

```go
func defaultCustomOriginConfig() customOriginConfigXML {
	return customOriginConfigXML{
		HTTPPort:             80,
		HTTPSPort:            443,
		OriginProtocolPolicy: "https-only",
		OriginSslProtocols: passthroughXML{
			Inner: `<Quantity>1</Quantity><Items><SslProtocol>TLSv1.2</SslProtocol></Items>`,
		},
	}
}
```

Expected: New platform-managed custom origins explicitly use TLSv1.2 when CloudFront connects to node origins.

- [ ] **Step 3: Confirm legacy `ForwardedValues` are preserved**

Inspect `cacheBehaviorXML`, `defaultCacheBehaviorXML`, `defaultBehaviorTemplate`, and `copyBehaviorDefaults`.

Required fields:

```go
ForwardedValues passthroughXML `xml:"ForwardedValues,omitempty"`
```

Required fallback:

```go
if dst.ForwardedValues.Inner == "" {
	dst.ForwardedValues = template.ForwardedValues
}
if dst.CachePolicyID == "" && dst.ForwardedValues.Inner == "" {
	dst.CachePolicyID = managedCachingDisabledPolicyID
}
```

Expected: Binding to older distributions that still use legacy `ForwardedValues` does not drop that XML during read-modify-write.

- [ ] **Step 4: Run AWS client compatibility tests**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/cloudfront -run 'TestAWSClient(CreateDistribution|ApplyDistributionRoutes)' -v
```

Expected: PASS, including these tests:

- `TestAWSClientCreateDistributionBuildsManagedConfig`
- `TestAWSClientApplyDistributionRoutesUpsertsOriginAndBehavior`
- `TestAWSClientApplyDistributionRoutesPreservesForwardedValuesTemplate`
- `TestAWSClientApplyDistributionRoutesRemovesManagedBehaviorAndOrphanOrigin`

## Task 3: Run Local Runtime Smoke Checks

**Files:**
- Inspect: `internal/api/controlplane.go`
- Inspect: `internal/api/web/index.html`

- [ ] **Step 1: Start the control plane locally**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build CONTROL_PLANE_LISTEN_ADDR=127.0.0.1:18080 CONTROL_PLANE_ADMIN_TOKEN=dev-admin CLOUDFRONT_MASTER_KEY=0123456789abcdef0123456789abcdef BOOTSTRAP_ADMIN_EMAIL=admin@example.com BOOTSTRAP_ADMIN_PASSWORD=change-me-now go run ./cmd/control-plane
```

Expected: The server starts and listens on `127.0.0.1:18080`.

- [ ] **Step 2: Confirm the admin UI includes CloudFront workflow controls**

Run in another shell:

```bash
curl -sSf http://127.0.0.1:18080/ | rg 'CloudFront|List Distributions|cf-distribution-select|Sub CF'
```

Expected: The command prints matching CloudFront UI labels/selectors.

- [ ] **Step 3: Confirm default config response is safe**

Run:

```bash
curl -sSf -H 'X-Admin-Token: dev-admin' http://127.0.0.1:18080/api/admin/cloudfront/config
```

Expected: JSON response has `id: "cf-global"`, `region: "us-east-1"`, `mode: "managed"`, and `enabled: false`.

- [ ] **Step 4: Confirm distribution discovery fails clearly before config**

Run:

```bash
curl -sS -i -H 'X-Admin-Token: dev-admin' http://127.0.0.1:18080/api/admin/cloudfront/distributions
```

Expected: HTTP `400` with an explicit error like `cloudfront is not configured`.

- [ ] **Step 5: Stop the local server**

Send `Ctrl-C` to the server process.

Expected: The process exits cleanly.

## Task 4: Deferred Production AWS Verification

**Files:**
- Inspect: `internal/cloudfront/aws_client.go`
- Inspect: `internal/api/controlplane.go`
- Inspect: `internal/api/web/index.html`

This task requires real AWS credentials with permission to list, read, create, and update CloudFront distributions. It is not required for the current local logic closure objective. If credentials/network are unavailable, or the current objective explicitly ignores real AWS account access, mark this task as deferred and continue to final local verification.

- [ ] **Step 1: Save CloudFront credentials through the platform**

Use the admin UI CloudFront section or `POST /api/admin/cloudfront/config`.

Expected:

- Plaintext secrets are accepted only on save.
- `GET /api/admin/cloudfront/config` returns a masked access key and never returns plaintext secret access key.
- `CLOUDFRONT_MASTER_KEY` must be configured; otherwise the API returns a clear service-unavailable error.

- [ ] **Step 2: Verify distribution auto-discovery**

Call:

```bash
curl -sSf -H 'X-Admin-Token: dev-admin' http://127.0.0.1:18080/api/admin/cloudfront/distributions
```

Expected:

- Existing CloudFront distributions are listed.
- Each item includes `distributionId`, `domainName`, `status`, optional `aliases`, optional `comment`, and `managedResourcesDetected`.
- The normal flow does not require manually typing a distribution ID.

- [ ] **Step 3: Verify bind-existing flow**

Select one discovered distribution and call `POST /api/admin/cloudfront/bind` with:

```json
{
  "distributionId": "E2ABCDEF123456"
}
```

Expected:

- Replace `E2ABCDEF123456` with the exact `distributionId` value returned by Step 2; do not type IDs by hand in the normal UI flow.
- The platform fetches the selected distribution from AWS before persisting binding state.
- Existing unmanaged routes are preserved.
- Unmanaged route conflicts are surfaced and not overwritten.
- Platform bindings use `v2ray-platform-node-<nodeID>` origins and stable `route_key` paths.

- [ ] **Step 4: Verify plan and sync for the bound distribution**

Run:

```bash
curl -sSf -X POST -H 'X-Admin-Token: dev-admin' http://127.0.0.1:18080/api/admin/cloudfront/plan
curl -sSf -X POST -H 'X-Admin-Token: dev-admin' http://127.0.0.1:18080/api/admin/cloudfront/sync
```

Expected:

- Plan returns only platform-managed creates, updates, deletes, or conflicts.
- Sync stops before AWS mutation if plan has `conflict`.
- Sync uses CloudFront `If-Match` ETag on distribution config update.
- After successful sync, config has `lastSuccessfulSyncAt` and `syncStatus` indicates success/synced.

- [ ] **Step 5: Verify create-new distribution flow**

Use a clean CloudFront config in `managed` mode with no bound `distributionId`, then call:

```bash
curl -sSf -X POST -H 'X-Admin-Token: dev-admin' http://127.0.0.1:18080/api/admin/cloudfront/bind
```

Expected:

- The platform creates a CloudFront distribution.
- The create request is accepted by AWS.
- The created distribution has platform-managed origins and cache behaviors for current routable nodes.
- The created distribution is scanned and bound back into the local CloudFront config.

- [ ] **Step 6: Verify CloudFront subscription availability after sync**

After a successful sync, request a member CloudFront subscription:

```bash
curl -sS -H 'X-Admin-Token: dev-admin' http://127.0.0.1:18080/api/admin/members/member-example-1/clash-cf.yaml
```

Expected:

- Replace `member-example-1` with an actual member ID from `GET /api/admin/members`.
- YAML `server` uses the CloudFront custom entry host when configured, otherwise the distribution domain.
- YAML websocket path uses `/{route_key}`.
- Direct subscription YAML still uses node direct host/path behavior.
- CloudFront YAML was unavailable before the first successful sync and becomes available only after it.

## Task 5: Final Documentation And Completion

**Files:**
- Modify: `AGENTS.md`
- Modify: `CLAUDE.md`
- Modify: `docs/superpowers/plans/2026-06-09-cloudfront-support-closure.md`

- [ ] **Step 1: Update agent instructions if the closure plan changes**

Confirm `AGENTS.md` and `CLAUDE.md` both mention this closure plan:

```markdown
- If continuing an already-started CloudFront branch, read `docs/superpowers/plans/2026-06-09-cloudfront-support-closure.md` first.
```

Expected: Future agents read the original spec, original implementation plan, and this current-state closure plan. The instructions also state that real AWS verification is deferred unless explicitly requested.

- [ ] **Step 2: Re-run final local verification**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/cloudfront ./internal/api ./internal/store
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./...
```

Expected: Both commands PASS.

- [ ] **Step 3: Record the real AWS verification result or deferral**

Update this plan with one of these exact outcomes:

```markdown
Real AWS status: verified on 2026-06-09 with bind-existing and create-new flows.
```

or:

```markdown
Real AWS status: blocked on 2026-06-09 because real AWS credentials/network were not available.
```

or:

```markdown
Real AWS status: deferred on 2026-06-09 by the current objective. Local logic closure was verified without a real AWS account.
```

Expected: The final handoff is honest about whether production CloudFront API acceptance has been proven.

- [ ] **Step 4: Commit the closure checkpoint**

Run:

```bash
git add AGENTS.md CLAUDE.md docs/superpowers/plans/2026-06-09-cloudfront-support-closure.md internal/cloudfront/aws_client.go internal/cloudfront/aws_client_test.go internal/api/controlplane.go
git commit -m "chore: close cloudfront support verification plan"
```

Expected: One atomic commit contains the closure docs and any final AWS compatibility hardening.

## Self-Review

Spec coverage check:

- Dual direct and CloudFront subscription outputs are preserved.
- Stable `route_key` routing is explicitly required.
- Encrypted CloudFront config and request-time AWS client construction are explicitly required.
- Distribution scan/select/bind/plan/sync flow is covered.
- Ownership boundaries and unmanaged conflict handling are covered.
- CloudFront YAML gating after first successful sync is covered.
- Real AWS verification is called out as the remaining production proof gate, not a local logic closure blocker.
- Custom entry host alias/DNS validation is explicitly deferred as production validation.

Placeholder scan:

- No banned placeholder markers or unspecified implementation gaps remain.
- Secret values and AWS account-specific IDs are intentionally supplied at execution time by the operator and must not be invented by an agent.

Type consistency check:

- CloudFront routing state consistently uses `DistributionState`, `OriginState`, `BehaviorState`, and `RouteAction`.
- CloudFront bind requests consistently use `distributionId`.
- CloudFront route paths consistently use `"/" + routeKey`.
