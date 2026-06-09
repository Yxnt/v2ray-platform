# CloudFront Subscription Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the full CloudFront control-plane loop for this project so admins can manage CloudFront from the platform and members can use either direct or CloudFront Clash subscriptions without breaking the existing direct flow.

**Architecture:** Treat direct subscriptions as the compatibility baseline and CloudFront as a second rendering mode plus a control-plane-managed AWS integration. Keep AWS-specific logic in `internal/cloudfront`, build the AWS client from encrypted DB config at request time, and model CloudFront correctly as origins plus cache behaviors keyed by stable node `route_key`.

**Tech Stack:** Go, net/http, PostgreSQL migrations, embedded admin UI, AWS CloudFront REST/XML APIs, table-driven unit tests

---

## Current State And Handoff Assumptions

This repository is not starting from zero.

- Stable node `route_key` already exists in schema and store paths.
- CloudFront config persistence, encryption wiring, CloudFront YAML rendering, and admin UI scaffolding already exist.
- Direct subscription compatibility is already partly protected by tests and must remain a hard rule.
- A deeper refactor has started in `internal/cloudfront` to model real CloudFront distribution state using origins plus behaviors.
- That refactor is incomplete, so the first job is to stabilize compilation and tests before adding more functionality.

Claude Code handoff rule:

- If the current branch already contains CloudFront implementation changes, do not blindly replay this plan from Task 1. First read `docs/superpowers/plans/2026-06-09-cloudfront-support-closure.md`, compare the code to this plan, and continue from the first task that is failing, missing, or not yet verified.
- If the current branch is clean or does not contain CloudFront work, execute this plan task-by-task in order.

Before touching code, inspect the current branch state:

```bash
git status --short
git diff -- internal/cloudfront internal/api/controlplane.go internal/api/web/index.html internal/store
```

Non-negotiable constraints from the approved spec:

- Keep `GET /sub/{token}/clash.yaml` backward compatible.
- Keep `GET /api/admin/members/{memberID}/clash.yaml` backward compatible.
- CloudFront is phase-one control-plane work only. Do not change node-agent registration, heartbeat, usage reporting, or runtime user add/remove flows.
- CloudFront YAML must only become user-visible after config exists, binding is complete, and at least one sync has succeeded.
- Existing distributions must be auto-discoverable; normal UX must not require manual `distribution_id` entry.
- Only platform-managed origins and behaviors may be mutated automatically.

## File Structure

### Existing files to modify

- `AGENTS.md`
  - Keep CloudFront execution rules in sync for future agents.
- `CLAUDE.md`
  - Mirror the same CloudFront handoff rules for Claude Code.
- `docs/superpowers/specs/2026-06-05-cloudfront-subscription-design.md`
  - Reference only; do not drift from it without explicit approval.
- `internal/api/controlplane.go`
  - Keep HTTP handlers thin, build AWS-backed CloudFront services from stored config, add distribution discovery endpoint, preserve direct and CloudFront subscription gating.
- `internal/api/controlplane_test.go`
  - Cover route-level behavior for distribution discovery, bind/plan/sync flows, and public/admin subscription gating.
- `internal/api/web/index.html`
  - Replace manual distribution entry UX with discovery/select flow and expose sync status clearly.
- `internal/cloudfront/bind.go`
  - Persist node-to-route bindings using the real distribution model.
- `internal/cloudfront/client.go`
  - Define the AWS client interface and planner DTOs.
- `internal/cloudfront/plan.go`
  - Compute desired route changes from node bindings versus remote origins plus behaviors.
- `internal/cloudfront/plan_test.go`
  - Update planner tests to the real distribution model.
- `internal/cloudfront/scan.go`
  - Scan distributions and reconstruct platform route state from origins plus cache behaviors.
- `internal/cloudfront/scan_test.go`
  - Cover scanning, discovery, and remote-state reconstruction.
- `internal/cloudfront/sync.go`
  - Execute a plan via the AWS client and update sync status.
- `internal/cloudfront/sync_test.go`
  - Cover sync success, sync failure, and status persistence.
- `internal/store/store.go`
  - Keep CloudFront persistence interfaces aligned with service needs.
- `internal/store/memory.go`
  - Mirror CloudFront config behavior for tests.
- `internal/store/memory_test.go`
  - Preserve config-secret retention and sync-status behavior.

### New files already present but incomplete

- `internal/cloudfront/aws_client.go`
  - Real AWS client for CloudFront list/get/apply behavior; `ApplyDistributionRoutes` still needs full implementation.
- `internal/cloudfront/aws_client_test.go`
  - Parsing and request-shape coverage for the AWS client.

### Notes for decomposition

- Do not add new CloudFront logic directly into `cmd/control-plane/main.go`; AWS clients should be built from decrypted config inside service or API helpers.
- Do not continue the older “origin-only” model. CloudFront path routing is defined by cache behaviors targeting origins.
- Do not model CloudFront path routing as selection-only. The platform must also manage the `v2ray-platform-route-rewrite` CloudFront Function so external `/{route_key}` requests are rewritten to each node's actual direct WebSocket path before origin fetch.
- Prefer finishing the partially started refactor over creating a second parallel abstraction.

### Task 1: Stabilize the current CloudFront refactor

**Files:**
- Modify: `internal/cloudfront/plan_test.go`
- Modify: `internal/cloudfront/scan_test.go`
- Modify: `internal/cloudfront/sync_test.go`
- Modify: `internal/cloudfront/client.go`
- Modify: `internal/cloudfront/plan.go`
- Modify: `internal/cloudfront/scan.go`
- Modify: `internal/cloudfront/sync.go`
- Test: `internal/cloudfront/aws_client_test.go`

- [ ] **Step 1: Review the real CloudFront model already introduced**

Read these files and confirm the intended types before editing tests:

```bash
sed -n '1,220p' internal/cloudfront/client.go
sed -n '1,260p' internal/cloudfront/plan.go
sed -n '1,260p' internal/cloudfront/scan.go
sed -n '1,220p' internal/cloudfront/sync.go
```

Expected: `DistributionState`, `OriginState`, `BehaviorState`, `DistributionSummary`, and `RouteAction` exist and are the source of truth for planner-facing state.

- [ ] **Step 2: Rewrite planner tests to match the new distribution model**

Replace origin-only expectations in `internal/cloudfront/plan_test.go` with `DistributionState`-based cases. The test structure should look like this:

```go
func TestPlanCreatesMissingRoute(t *testing.T) {
	bindings := []domain.CloudFrontBinding{
		{NodeID: "node-1", RouteKey: "rk-1", OriginID: "v2ray-platform-node-node-1"},
	}
	dist := &DistributionState{
		DistributionID: "E1234",
		Origins: []OriginState{
			{ID: "v2ray-platform-node-node-1", DomainName: "node1.example.com"},
		},
		Behaviors: nil,
	}

	plan := Plan(bindings, dist, map[string]string{"node-1": "node1.example.com"})
	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %#v", plan.Actions)
	}
	if plan.Actions[0].Type != RouteActionUpsertBehavior {
		t.Fatalf("expected upsert behavior action, got %#v", plan.Actions[0])
	}
	if plan.Actions[0].RouteKey != "rk-1" {
		t.Fatalf("unexpected route key: %#v", plan.Actions[0])
	}
}
```

Also cover:

- route already in sync
- origin host drift requiring origin update
- managed route removed locally requiring behavior delete
- unmanaged conflicting route causing `HasConflicts`

- [ ] **Step 3: Run focused CloudFront tests to capture the current failure set**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/cloudfront -v
```

Expected: FAIL, likely from stale tests or incomplete `ApplyDistributionRoutes`.

- [ ] **Step 4: Make the planner, scanner, and sync service compile and agree on one route model**

Use these invariants while fixing code:

```go
// Path routing is always derived from the stable route key.
func routePattern(routeKey string) string {
	return "/" + routeKey
}

// Platform-managed origins must remain recognizable across scans and syncs.
func managedOriginID(nodeID string) string {
	return "v2ray-platform-node-" + nodeID
}
```

Implementation requirements:

- `scan.go` must reconstruct route mappings from cache behaviors to origin IDs.
- `plan.go` must compare desired bindings against the reconstructed route table.
- `sync.go` must pass `RouteAction` values straight to the client without re-deriving route semantics.

- [ ] **Step 5: Re-run focused CloudFront tests**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/cloudfront -v
```

Expected: PASS except possibly tests blocked by the still-unimplemented AWS apply path. If failures remain, they should be isolated to `ApplyDistributionRoutes`.

- [ ] **Step 6: Commit the stabilization checkpoint**

```bash
git add internal/cloudfront/client.go internal/cloudfront/plan.go internal/cloudfront/plan_test.go internal/cloudfront/scan.go internal/cloudfront/scan_test.go internal/cloudfront/sync.go internal/cloudfront/sync_test.go internal/cloudfront/aws_client_test.go
git commit -m "refactor: stabilize cloudfront route planning model"
```

### Task 2: Build AWS-backed distribution discovery from stored config

**Files:**
- Modify: `internal/api/controlplane.go`
- Modify: `internal/api/controlplane_test.go`
- Modify: `internal/cloudfront/aws_client.go`
- Modify: `internal/cloudfront/scan.go`
- Modify: `internal/store/store.go`
- Modify: `internal/store/memory.go`
- Test: `internal/cloudfront/aws_client_test.go`

- [ ] **Step 1: Add failing API tests for distribution discovery**

Add route tests for `GET /api/admin/cloudfront/distributions` covering:

- missing CloudFront config returns `400` or `409`
- stored config without usable credentials returns `400`
- AWS client list success returns distribution summaries
- AWS client failure returns a surfaced error payload

Recommended shape:

```go
func TestListCloudFrontDistributions(t *testing.T) {
	st := store.NewMemoryStore()
	seedCloudFrontConfig(t, st)

	svc := newTestControlPlaneService(t, st)
	svc.newCloudFrontClient = func(context.Context) (cloudfront.Client, error) {
		return &mockDiscoveryClient{
			items: []cloudfront.DistributionSummary{
				{ID: "E1234", DomainName: "d1234.cloudfront.net", Status: "Deployed"},
			},
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/cloudfront/distributions", nil)
	rr := authedAdminRequest(t, svc, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rr.Code, rr.Body.String())
	}
}
```

- [ ] **Step 2: Run the focused API test and verify it fails**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/api -run TestListCloudFrontDistributions -v
```

Expected: FAIL because the route and helper do not exist yet.

- [ ] **Step 3: Add a request-time CloudFront client builder in the control plane service**

Add a helper on `ControlPlaneService` with this behavior:

```go
func (svc *ControlPlaneService) newConfiguredCloudFrontClient(ctx context.Context) (cloudfront.Client, *domain.CloudFrontConfig, error) {
	cfg, err := svc.store.GetCloudFrontConfig()
	if err != nil {
		return nil, nil, err
	}
	if cfg == nil {
		return nil, nil, apiError(http.StatusBadRequest, "cloudfront_not_configured", "CloudFront is not configured")
	}
	accessKey, secretKey, sessionToken, err := svc.decryptCloudFrontCredentials(cfg)
	if err != nil {
		return nil, nil, err
	}
	client := cloudfront.NewAWSClient(cloudfront.AWSClientConfig{
		AccessKeyID: accessKey,
		SecretAccessKey: secretKey,
		SessionToken: sessionToken,
		Region: cfg.AWSRegion,
		HTTPClient: svc.httpClient,
	})
	return client, cfg, nil
}
```

Requirements:

- Build from decrypted DB values, not boot-time singletons.
- Use the same helper for discovery, scan, bind-prep, plan refresh, and sync.
- If the master key is missing or invalid, fail explicitly.

- [ ] **Step 4: Add `GET /api/admin/cloudfront/distributions`**

Wire the route and return the list from `client.ListDistributions(ctx)`. Response shape should be a thin JSON wrapper around `[]cloudfront.DistributionSummary`.

- [ ] **Step 5: Re-run focused API tests**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/api -run TestListCloudFrontDistributions -v
```

Expected: PASS.

- [ ] **Step 6: Commit distribution discovery**

```bash
git add internal/api/controlplane.go internal/api/controlplane_test.go internal/cloudfront/aws_client.go internal/cloudfront/aws_client_test.go internal/cloudfront/scan.go internal/store/store.go internal/store/memory.go
git commit -m "feat: add cloudfront distribution discovery"
```

### Task 3: Fix bind flow so existing distributions can be adopted safely

**Files:**
- Modify: `internal/api/controlplane.go`
- Modify: `internal/api/controlplane_test.go`
- Modify: `internal/cloudfront/bind.go`
- Modify: `internal/cloudfront/scan.go`
- Modify: `internal/cloudfront/scan_test.go`

- [ ] **Step 1: Add failing bind-flow tests**

Cover:

- bind request with selected `distributionId`
- live scan by ID before persisting bindings
- unmatched nodes get platform-managed origin IDs
- remote unmanaged conflicting routes are preserved and surfaced

Recommended assertion pattern:

```go
func TestCloudFrontBindScansDistributionBeforePersistingBindings(t *testing.T) {
	// Seed config, return a remote distribution with one behavior,
	// then assert bindings/origins persisted from the live scan result.
}
```

- [ ] **Step 2: Run focused bind tests and verify they fail**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/cloudfront ./internal/api -run 'TestCloudFrontBind|TestBindNodes' -v
```

Expected: FAIL because the current API flow does not yet perform live distribution selection plus scan plus bind as one consistent sequence.

- [ ] **Step 3: Update the bind request contract**

Use a request body like:

```go
type bindCloudFrontRequest struct {
	DistributionID string `json:"distributionId"`
}
```

Then implement this order:

1. load configured AWS client
2. fetch the selected distribution by ID
3. reconstruct remote route state via `ScanDistributionByID`
4. persist `distribution_id`, `distribution_domain_name`, and remote origin snapshot
5. run `BindNodes`
6. return bindings plus scan summary

- [ ] **Step 4: Keep ownership boundaries strict during binding**

Binding rules:

- If a remote behavior path matches `/{route_key}` and points at a managed origin ID, treat it as managed.
- If a remote behavior path matches `/{route_key}` but points at an unmanaged origin, surface a conflict and do not auto-overwrite during bind.
- If a node has no remote match, create a binding placeholder using `v2ray-platform-node-<nodeID>`.

- [ ] **Step 5: Re-run bind tests**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/cloudfront ./internal/api -run 'TestCloudFrontBind|TestBindNodes' -v
```

Expected: PASS.

- [ ] **Step 6: Commit safe bind flow**

```bash
git add internal/api/controlplane.go internal/api/controlplane_test.go internal/cloudfront/bind.go internal/cloudfront/scan.go internal/cloudfront/scan_test.go
git commit -m "feat: support binding existing cloudfront distributions"
```

### Task 4: Implement real CloudFront sync against origins plus cache behaviors

**Files:**
- Modify: `internal/cloudfront/aws_client.go`
- Modify: `internal/cloudfront/aws_client_test.go`
- Modify: `internal/cloudfront/sync.go`
- Modify: `internal/cloudfront/sync_test.go`
- Modify: `internal/api/controlplane.go`
- Modify: `internal/api/controlplane_test.go`

- [ ] **Step 1: Add failing AWS client tests for apply**

Cover request/response behavior for:

- GET distribution config including ETag
- origin upsert
- behavior upsert
- behavior delete
- conflict-safe no-op when nothing changes

Sketch:

```go
func TestAWSClientApplyDistributionRoutesBuildsUpdatedDistributionConfig(t *testing.T) {
	// Stub GET distribution-config + GET distribution,
	// assert PUT /distribution/{id}/config includes updated origins and cache behaviors.
}
```

- [ ] **Step 2: Run focused apply tests and verify they fail**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/cloudfront -run 'TestAWSClientApplyDistributionRoutes|TestExecutePlan' -v
```

Expected: FAIL because `ApplyDistributionRoutes` is still stubbed.

- [ ] **Step 3: Implement `ApplyDistributionRoutes` with ETag-aware read-modify-write**

Required sequence:

1. `GetDistribution` or distribution-config fetch for current config plus ETag
2. derive mutable origin and cache-behavior collections
3. apply `RouteAction` list in memory
4. keep unmanaged origins and unmanaged behaviors untouched
5. update `Quantity` and `Items` consistently
6. `PUT` updated config with `If-Match: <etag>`

Action handling rules:

- origin upsert updates domain name or creates the managed origin if missing
- behavior upsert ensures `PathPattern == "/" + routeKey` and `TargetOriginId == originID`
- behavior delete only removes a managed behavior for that route
- deleting an origin is allowed only if no remaining managed behavior targets it

- [ ] **Step 4: Update sync status semantics**

On sync success:

- set `last_sync_status = "success"`
- clear `last_sync_error`
- set `last_sync_at`
- set `last_successful_sync_at`

On sync failure:

- set `last_sync_status = "error"`
- set `last_sync_error`
- set `last_sync_at`
- preserve `last_successful_sync_at`

On plan refresh with no execution:

- do not wipe the previous sync success marker

- [ ] **Step 5: Re-run focused CloudFront tests**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/cloudfront -v
```

Expected: PASS.

- [ ] **Step 6: Commit real sync support**

```bash
git add internal/cloudfront/aws_client.go internal/cloudfront/aws_client_test.go internal/cloudfront/sync.go internal/cloudfront/sync_test.go internal/api/controlplane.go internal/api/controlplane_test.go
git commit -m "feat: sync cloudfront routes through aws api"
```

### Task 5: Finish admin UI flow for scan, select, plan, and sync

**Files:**
- Modify: `internal/api/web/index.html`
- Modify: `internal/api/controlplane.go`
- Modify: `internal/api/controlplane_test.go`

- [ ] **Step 1: Add failing UI-contract tests where practical and define the API expectations**

Even if the HTML itself is not unit-tested deeply, add API tests for the UI-facing workflow:

- load config
- list distributions
- bind selected distribution
- compute plan
- execute sync
- show current sync status and last successful sync

- [ ] **Step 2: Replace manual distribution entry UX with scan-and-select**

UI requirements:

- keep raw `distributionId` visible as status, not as the primary input
- add a “Scan distributions” action
- populate a dropdown or selection list from `GET /api/admin/cloudfront/distributions`
- show `domainName`, `aliases`, `status`, `comment`, and whether managed resources were detected

- [ ] **Step 3: Keep CloudFront export affordances gated**

Member actions may show both:

- direct download / direct subscription URL
- CloudFront download / CloudFront subscription URL

But the CloudFront variant must remain disabled or error clearly unless:

- CloudFront config is enabled
- a distribution is bound
- `last_successful_sync_at` is non-nil
- `last_sync_status` is not currently invalidating the configuration

- [ ] **Step 4: Re-run focused API tests and do a lightweight manual UI check**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/api -v
```

Then, if you can run the app locally, verify:

1. save CloudFront config
2. scan distributions
3. bind one distribution
4. preview plan
5. sync successfully
6. copy `Sub CF🔗` only after successful sync

- [ ] **Step 5: Commit the admin UX finish**

```bash
git add internal/api/web/index.html internal/api/controlplane.go internal/api/controlplane_test.go
git commit -m "feat: finish cloudfront admin workflow"
```

### Task 6: End-to-end regression verification and documentation alignment

**Files:**
- Modify: `AGENTS.md`
- Modify: `CLAUDE.md`
- Modify: `docs/superpowers/plans/2026-06-05-cloudfront-subscription-implementation.md`

- [ ] **Step 1: Run focused verification for the CloudFront workstream**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/cloudfront ./internal/api ./internal/store -v
```

Expected: PASS.

- [ ] **Step 2: Run full regression verification**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./... -v
```

Expected: PASS.

- [ ] **Step 3: Reconcile docs with the final behavior**

Confirm these statements remain true:

- direct subscription URLs are unchanged
- CloudFront path routing uses stable `route_key`
- CloudFront distribution discovery is scan-first, not manual-ID-first
- the platform only mutates platform-managed origins and behaviors
- CloudFront subscription export is gated by successful sync

- [ ] **Step 4: Commit final doc alignment if needed**

```bash
git add AGENTS.md CLAUDE.md docs/superpowers/plans/2026-06-05-cloudfront-subscription-implementation.md
git commit -m "docs: align cloudfront execution guidance"
```

## Self-Review

Spec coverage check:

- stable route-key behavior: covered by the current schema plus stabilization guardrails
- encrypted config usage: covered by request-time client construction
- dual subscription outputs: preserved throughout and re-verified in Task 5 and Task 6
- distribution discovery and bind flow: covered by Task 2 and Task 3
- live plan and sync against AWS: covered by Task 4
- admin UX and gating: covered by Task 5
- regression verification: covered by Task 6

Placeholder scan:

- No banned placeholder markers or shortcut references remain.

Type consistency check:

- The plan consistently uses `DistributionState`, `OriginState`, `BehaviorState`, `DistributionSummary`, and `RouteAction` as the CloudFront route model.
- The bind request consistently uses `distributionId`.
- The CloudFront route pattern is consistently `"/" + routeKey`.

## Execution Handoff

This plan is ready for Claude Code or another implementation agent.

Execute tasks in order when starting from a clean CloudFront branch. When continuing the current dirty branch, use `docs/superpowers/plans/2026-06-09-cloudfront-support-closure.md` as the entry point and use this document as the original product implementation plan.
