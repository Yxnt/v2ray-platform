# CloudFront Wizard Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current CloudFront button panel with a step-by-step wizard that supports first-time setup, returning-user continuation, existing-distribution adoption, managed-distribution creation, and final bind/plan/sync without requiring operators to infer the action order.

**Architecture:** Keep the backend CloudFront APIs as the execution engine and move the primary orchestration into a small front-end wizard state machine inside `internal/api/web/index.html`. Preserve the current single-bound-distribution model, reuse the existing config, scan, bind, plan, and sync endpoints, and expose technical output through a collapsible details panel rather than the main flow.

**Tech Stack:** Go, embedded HTML/CSS/vanilla JavaScript, net/http route tests in Go, existing CloudFront control-plane endpoints

---

## File Structure

### Files to modify

- `internal/api/web/index.html`
  - Replace the CloudFront tab's button-heavy layout with the wizard shell, step panels, current-setup landing state, and technical details drawer.
  - Add a front-end wizard state model, step transition helpers, auto-discovery after credential save, empty-state fallback to managed create, and step-aware bind/plan/sync orchestration.
- `internal/api/controlplane.go`
  - Keep the current CloudFront endpoint behavior aligned with the wizard needs, especially selected-distribution scan handling and any step-specific response normalization.
- `internal/api/controlplane_test.go`
  - Add or update route-level UI contract tests and CloudFront endpoint tests that verify the wizard's expected HTML hooks and selected-distribution behavior.

### Files to inspect while implementing

- `docs/superpowers/specs/2026-06-13-cloudfront-wizard-design.md`
  - Product contract for the wizard flow.
- `internal/cloudfront/scan.go`
  - Current scan semantics used by Step 4 review.
- `internal/store/memory.go`
  - Existing sync invalidation behavior when switching distribution or bindings.

---

### Task 1: Verify and preserve the existing selected-distribution scan contract

**Files:**
- Inspect: `internal/api/controlplane.go`
- Inspect: `internal/api/controlplane_test.go`

- [ ] **Step 1: Verify the selected-distribution scan support already present on this branch**

Confirm the branch already contains both:

- `handleCloudFrontScan` reads optional `distributionId` and calls `ScanDistributionByID(...)`
- `TestCloudFrontScanUsesSelectedDistributionWhenProvided`

- [ ] **Step 2: Run the focused API test and verify the branch still passes**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/api -run TestCloudFrontScanUsesSelectedDistributionWhenProvided -v
```

Expected: PASS.

- [ ] **Step 3: If the test fails, repair the regression in place instead of re-adding duplicate coverage**

If the branch regressed, restore this behavior in `handleCloudFrontScan`:

```go
func (svc *ControlPlaneService) handleCloudFrontScan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DistributionID string `json:"distributionId"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, errors.New("invalid request body"))
			return
		}
	}

	client, cfg, err := svc.newConfiguredCloudFrontClient()
	if err != nil {
		writeCloudFrontClientError(w, err)
		return
	}
	scanSvc := cloudfront.NewScanService(svc.store, client)

	var result *cloudfront.ScanResult
	if strings.TrimSpace(req.DistributionID) != "" {
		result, err = scanSvc.ScanDistributionByID(r.Context(), strings.TrimSpace(req.DistributionID), cfg.Mode)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
	} else {
		result, err = scanSvc.ScanDistribution(r.Context())
		if err != nil {
			writeStoreError(w, err)
			return
		}
	}

	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "cloudfront.scanned", "cloudfront_config", "cf-global", map[string]any{
		"distribution_id": result.DistributionID,
		"origins_count":   len(result.Origins),
	})
	writeJSON(w, http.StatusOK, result)
}
```

- [ ] **Step 4: Re-run the focused test only if Step 3 changed code**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/api -run TestCloudFrontScanUsesSelectedDistributionWhenProvided -v
```

Expected: PASS.

- [ ] **Step 5: Do not create a standalone commit if no code changed in this task**

If Step 3 was a no-op, leave this task uncommitted and continue. The goal of this task is to avoid replaying already-finished work on a partially implemented branch.

---

### Task 2: Replace the current CloudFront action bar with the wizard shell and current-setup landing state

**Files:**
- Modify: `internal/api/web/index.html`
- Modify: `internal/api/controlplane_test.go`

- [ ] **Step 1: Add a failing UI contract test for the wizard shell**

Add this test to `internal/api/controlplane_test.go`:

```go
func TestCloudFrontAdminUIContainsWizardShell(t *testing.T) {
	htmlBytes, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)

	required := []string{
		`id="cf-wizard"`,
		`id="cf-current-setup"`,
		`id="cf-stepper"`,
		`data-cf-step="connect"`,
		`data-cf-step="path"`,
		`data-cf-step="target"`,
		`data-cf-step="review"`,
		`data-cf-step="sync"`,
		`Show technical details`,
	}
	for _, needle := range required {
		if !strings.Contains(html, needle) {
			t.Fatalf("expected CloudFront wizard shell to contain %q", needle)
		}
	}
}
```

- [ ] **Step 2: Run the focused UI contract test and verify it fails**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/api -run TestCloudFrontAdminUIContainsWizardShell -v
```

Expected: FAIL because the current HTML still contains the old action bar layout.

- [ ] **Step 3: Replace the current CloudFront card markup with wizard scaffolding**

In `internal/api/web/index.html`, replace the current CloudFront action bar and credentials block with a structure like this:

```html
<div class="card" style="margin-bottom:20px">
  <div class="card-title">CloudFront Status</div>
  <div class="cf-status-grid">...</div>
</div>

<div class="card" id="cf-wizard" style="margin-bottom:20px">
  <div class="card-title">Setup Wizard</div>
  <div id="cf-current-setup" style="display:none"></div>
  <div id="cf-stepper" class="cf-stepper">
    <button type="button" class="cf-step-chip" data-cf-step="connect">1. Connect AWS</button>
    <button type="button" class="cf-step-chip" data-cf-step="path">2. Choose Path</button>
    <button type="button" class="cf-step-chip" data-cf-step="target">3. Select or Create</button>
    <button type="button" class="cf-step-chip" data-cf-step="review">4. Review and Bind</button>
    <button type="button" class="cf-step-chip" data-cf-step="sync">5. Check and Sync</button>
  </div>

  <div id="cf-step-panels">
    <section id="cf-step-connect"></section>
    <section id="cf-step-path" hidden></section>
    <section id="cf-step-target" hidden></section>
    <section id="cf-step-review" hidden></section>
    <section id="cf-step-sync" hidden></section>
  </div>
  <div id="cf-tech-details-wrap">
    <button type="button" class="btn btn-ghost btn-sm" id="cf-tech-toggle">Show technical details</button>
    <pre class="output-pre" id="cf-action-output" style="display:none"></pre>
  </div>
</div>
```

Also remove the old top-level CloudFront action buttons from the main flow. Do not delete the output `<pre>`; move it under the technical details toggle.

- [ ] **Step 4: Update or remove old action-bar UI contract tests in the same task**

The existing tests still assert the pre-wizard control surface. Replace or delete these old assertions as part of the same HTML transition:

- `TestCloudFrontAdminUIAllowsManagedCreateWithoutSelectedDistribution`
- `TestCloudFrontAdminUIScanUsesSelectedDistribution`

Do not leave the branch in a mixed state where the new wizard shell exists but the test suite still hard-requires:

- `id="cf-bind-btn">Bind / Create`
- `async function scanCloudFrontDistribution()`
- `document.getElementById('cf-scan-btn').addEventListener(...)`

- [ ] **Step 5: Add minimal wizard CSS primitives inside the existing stylesheet**

In the inline CSS section of `internal/api/web/index.html`, add focused rules for:

```css
.cf-stepper { display:flex; gap:10px; flex-wrap:wrap; margin-bottom:18px; }
.cf-step-chip { border:1px solid var(--border); background:var(--surface-2); color:var(--text-dim); padding:10px 14px; border-radius:999px; }
.cf-step-chip.is-active { background:var(--blue); color:white; border-color:transparent; }
.cf-step-chip.is-done { border-color:var(--green); color:var(--green); }
.cf-step-panel { display:grid; gap:16px; }
.cf-choice-grid { display:grid; grid-template-columns:repeat(auto-fit, minmax(220px, 1fr)); gap:12px; }
.cf-choice-card { border:1px solid var(--border); background:var(--surface-2); border-radius:14px; padding:16px; }
.cf-tech-open #cf-action-output { display:block !important; }
```

Keep the look aligned with the existing design system. Do not introduce unrelated visual directions.

- [ ] **Step 6: Re-run the focused UI contract test and the migrated HTML contract tests**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/api -run TestCloudFrontAdminUIContainsWizardShell -v
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/api -run 'TestCloudFrontAdminUI(ContainsWizardShell|AllowsManagedCreateWithoutSelectedDistribution|ScanUsesSelectedDistribution)' -v
```

Expected: PASS.

- [ ] **Step 7: Commit the wizard shell**

```bash
git add internal/api/web/index.html internal/api/controlplane_test.go
git commit -m "feat: add cloudfront wizard shell"
```

---

### Task 3: Implement Step 1 and current-setup entry behavior

**Files:**
- Modify: `internal/api/web/index.html`
- Modify: `internal/api/controlplane_test.go`

- [ ] **Step 1: Add failing UI contract tests for Step 1 and current-setup actions**

Add these tests:

```go
func TestCloudFrontAdminUIContainsCurrentSetupActions(t *testing.T) {
	htmlBytes, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)

	required := []string{
		`Keep current setup`,
		`Change CloudFront setup`,
		`Use existing credentials`,
		`Replace credentials`,
		`Connect and continue`,
	}
	for _, needle := range required {
		if !strings.Contains(html, needle) {
			t.Fatalf("expected CloudFront setup UI to contain %q", needle)
		}
	}
}

func TestCloudFrontAdminUIKeepCurrentSetupGoesToSyncStep(t *testing.T) {
	htmlBytes, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)

	required := []string{
		`id="cf-keep-current-btn"`,
		`async function keepCurrentCloudFrontSetup()`,
		`await prepareCloudFrontSyncStep();`,
		`setCloudFrontStep('sync');`,
	}
	for _, needle := range required {
		if !strings.Contains(html, needle) {
			t.Fatalf("expected CloudFront keep-current flow to contain %q", needle)
		}
	}
}
```

- [ ] **Step 2: Run the focused UI tests and verify they fail**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/api -run 'TestCloudFrontAdminUI(Contains(CurrentSetupActions|WizardShell)|KeepCurrentSetupGoesToSyncStep)' -v
```

Expected: FAIL because the new labels and sections are not fully wired yet.

- [ ] **Step 3: Add the Step 1 renderers and current-setup landing state to the front-end**

Inside the CloudFront JS block in `internal/api/web/index.html`, add a wizard state model and render helpers like:

```js
const cloudFrontWizard = {
  step: 'connect',
  mode: 'managed',
  path: '',
  selectedDistributionId: '',
  detailsOpen: false,
  hasExistingConfig: false,
  currentSetupDismissed: false,
};

function setCloudFrontStep(step) {
  cloudFrontWizard.step = step;
  renderCloudFrontWizard();
}

function renderCloudFrontCurrentSetup(cfg) {
  const wrap = document.getElementById('cf-current-setup');
  if (!cfg?.distributionId || cloudFrontWizard.currentSetupDismissed) {
    wrap.style.display = 'none';
    wrap.innerHTML = '';
    return;
  }
  wrap.style.display = 'block';
  wrap.innerHTML = `
    <div class="cf-choice-card">
      <h3 style="margin:0 0 8px">Current setup</h3>
      <p style="margin:0 0 12px;color:var(--text-dim)">Currently bound to ${escapeHtml(cfg.distributionId)}.</p>
      <div style="display:flex;gap:8px;flex-wrap:wrap">
        <button class="btn btn-secondary btn-sm" id="cf-keep-current-btn">Keep current setup</button>
        <button class="btn btn-primary btn-sm" id="cf-change-setup-btn">Change CloudFront setup</button>
      </div>
    </div>
  `;
}
```

Also render Step 1 with:

- `Use existing credentials`
- `Replace credentials`
- `Connect and continue`

When `Use existing credentials` is chosen, the UI should keep the masked key display and call discovery on continue without forcing the user to type secrets again.

- [ ] **Step 4: Implement the `Keep current setup` fast path required by the spec**

Add a dedicated entry-point that skips the earlier setup steps and opens the sync step with auto-plan behavior:

```js
async function keepCurrentCloudFrontSetup() {
  cloudFrontWizard.currentSetupDismissed = true;
  await prepareCloudFrontSyncStep();
}
```

Wire `#cf-keep-current-btn` to this path when rendering the current-setup card. Do not bounce the user back through credential save, path choice, or target selection.

- [ ] **Step 5: Update `loadCloudFrontConfig` to drive the landing-state behavior**

Extend `loadCloudFrontConfig()` so it:

- detects whether a config already exists
- populates `cloudFrontWizard.hasExistingConfig`
- renders the `Current setup` landing state when a bound distribution exists
- keeps the wizard on Step 1 only when the user chooses to change setup

Use logic like:

```js
cloudFrontWizard.hasExistingConfig = !!cfg.accessKeyId;
if (cfg.distributionId && !cloudFrontWizard.currentSetupDismissed) {
  renderCloudFrontCurrentSetup(cfg);
} else {
  document.getElementById('cf-current-setup').style.display = 'none';
}
```

- [ ] **Step 6: Wire Step 1 continue behavior**

Add a single entry-point function:

```js
async function connectCloudFrontWizard() {
  await saveCloudFrontConfig();
  await loadCloudFrontDistributions(document.getElementById('cf-distribution-id').value.trim());
  setCloudFrontStep('path');
}
```

If the user kept existing credentials, this should still run discovery and continue to Step 2.

- [ ] **Step 7: Re-run the focused UI tests and verify they pass**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/api -run 'TestCloudFrontAdminUI(Contains(CurrentSetupActions|WizardShell)|KeepCurrentSetupGoesToSyncStep)' -v
```

Expected: PASS.

- [ ] **Step 8: Commit Step 1 and current-setup behavior**

```bash
git add internal/api/web/index.html internal/api/controlplane_test.go
git commit -m "feat: add cloudfront wizard entry flow"
```

---

### Task 4: Implement path choice, existing-selection flow, and empty-state fallback to managed create

**Files:**
- Modify: `internal/api/web/index.html`
- Modify: `internal/api/controlplane_test.go`

- [ ] **Step 1: Add failing UI contract tests for the path-choice step**

Add this test:

```go
func TestCloudFrontAdminUIContainsWizardPathChoice(t *testing.T) {
	htmlBytes, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)

	required := []string{
		`Use existing distribution`,
		`Create new managed distribution`,
		`No existing distributions found`,
		`Create a managed distribution instead`,
		`Use selected distribution`,
		`Create distribution`,
	}
	for _, needle := range required {
		if !strings.Contains(html, needle) {
			t.Fatalf("expected CloudFront wizard path UI to contain %q", needle)
		}
	}
}
```

- [ ] **Step 2: Run the focused UI test and verify it fails**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/api -run TestCloudFrontAdminUIContainsWizardPathChoice -v
```

Expected: FAIL until the new path-choice content exists.

- [ ] **Step 3: Render Step 2 and Step 3 panels with explicit path branching**

In `internal/api/web/index.html`, add render helpers:

```js
function renderCloudFrontPathStep() {
  return `
    <div class="cf-step-panel">
      <h3 style="margin:0">Choose setup path</h3>
      <div class="cf-choice-grid">
        <button type="button" class="cf-choice-card" id="cf-path-existing">Use existing distribution</button>
        <button type="button" class="cf-choice-card" id="cf-path-managed">Create new managed distribution</button>
      </div>
    </div>
  `;
}

function renderCloudFrontTargetStep() {
  if (cloudFrontWizard.path === 'existing') {
    if (!cloudFrontDistributions.length) {
      return `
        <div class="cf-step-panel">
          <h3 style="margin:0">No existing distributions found</h3>
          <p style="margin:0;color:var(--text-dim)">This account does not currently expose any candidate distributions.</p>
          <button class="btn btn-primary" id="cf-switch-to-managed-btn">Create a managed distribution instead</button>
        </div>
      `;
    }
    return `
      <div class="cf-step-panel">
        <h3 style="margin:0">Select an existing distribution</h3>
        <select id="cf-distribution-select"></select>
        <button class="btn btn-primary" id="cf-use-existing-btn">Use selected distribution</button>
      </div>
    `;
  }
  return `
    <div class="cf-step-panel">
      <h3 style="margin:0">Create a new managed distribution</h3>
      <p style="margin:0;color:var(--text-dim)">The platform will create and then switch to a new managed distribution.</p>
      <button class="btn btn-primary" id="cf-create-managed-btn">Create distribution</button>
    </div>
  `;
}
```

- [ ] **Step 4: Implement path transitions and empty-state fallback**

Add handlers:

```js
function chooseCloudFrontPath(path) {
  cloudFrontWizard.path = path;
  cloudFrontWizard.selectedDistributionId = '';
  setCloudFrontStep('target');
}

function switchToManagedFallback() {
  cloudFrontWizard.path = 'managed';
  setCloudFrontStep('target');
}
```

When the user picks existing and the list is empty, the UI must show the fallback button instead of an error dead-end.

- [ ] **Step 5: Re-run the focused UI test and verify it passes**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/api -run TestCloudFrontAdminUIContainsWizardPathChoice -v
```

Expected: PASS.

- [ ] **Step 6: Commit the path-choice flow**

```bash
git add internal/api/web/index.html internal/api/controlplane_test.go
git commit -m "feat: add cloudfront wizard path selection"
```

---

### Task 5: Implement review/bind/sync steps and collapse the technical output

**Files:**
- Modify: `internal/api/web/index.html`
- Modify: `internal/api/controlplane_test.go`

- [ ] **Step 1: Add failing UI contract tests for review, sync, and technical-details behavior**

Add this test:

```go
func TestCloudFrontAdminUIContainsReviewAndSyncWizardStates(t *testing.T) {
	htmlBytes, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)

	required := []string{
		`Bind distribution`,
		`Apply to CloudFront`,
		`Re-check changes`,
		`Show technical details`,
		`Hide technical details`,
	}
	for _, needle := range required {
		if !strings.Contains(html, needle) {
			t.Fatalf("expected CloudFront wizard review/sync UI to contain %q", needle)
		}
	}
}
```

- [ ] **Step 2: Run the focused UI test and verify it fails**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/api -run TestCloudFrontAdminUIContainsReviewAndSyncWizardStates -v
```

Expected: FAIL until the final wizard controls are rendered.

- [ ] **Step 3: Implement review step orchestration for existing and managed paths**

Add front-end functions like:

```js
async function prepareCloudFrontReviewStep() {
  const selectedID = cloudFrontWizard.selectedDistributionId || document.getElementById('cf-distribution-id').value.trim();
  const body = selectedID ? { distributionId: selectedID } : {};
  const result = await api('/api/admin/cloudfront/scan', {
    method: 'POST',
    body: JSON.stringify(body),
  });
  cloudFrontWizard.review = result;
  setCloudFrontStep('review');
}

async function bindCloudFrontWizardDistribution() {
  const selectedID = cloudFrontWizard.selectedDistributionId;
  const body = selectedID ? { distributionId: selectedID } : {};
  const result = await api('/api/admin/cloudfront/bind', {
    method: 'POST',
    body: JSON.stringify(body),
  });
  cloudFrontWizard.bindResult = result;
  await loadCloudFrontConfig();
  await prepareCloudFrontSyncStep();
}
```

For the managed path:

- Step 3 must call the existing managed-create bind endpoint exactly once to create and select the target distribution
- persist the returned distribution id into `cloudFrontWizard.selectedDistributionId`
- after creation, Step 4 must review the newly selected distribution state instead of trying to create again
- the Step 4 primary action is final bind confirmation against the selected distribution, not a second managed-create attempt

Do not let the wizard issue two create attempts just because both Step 3B and Step 4 touch `/api/admin/cloudfront/bind`.

- [ ] **Step 4: Implement Step 5 auto-plan and final sync**

Add functions:

```js
async function prepareCloudFrontSyncStep() {
  const result = await api('/api/admin/cloudfront/plan', { method: 'POST' });
  cloudFrontWizard.plan = result;
  setCloudFrontStep('sync');
}

async function recheckCloudFrontChanges() {
  return prepareCloudFrontSyncStep();
}

async function applyCloudFrontWizardSync() {
  const result = await api('/api/admin/cloudfront/sync', { method: 'POST' });
  cloudFrontWizard.sync = result;
  await loadCloudFrontConfig();
  renderCloudFrontWizard();
}
```

If `cloudFrontWizard.plan.driftStatus === 'conflict'`, the sync button must render disabled and the UI must show the conflict reason inline.

- [ ] **Step 5: Implement the technical details toggle**

Wire:

```js
function toggleCloudFrontTechnicalDetails() {
  cloudFrontWizard.detailsOpen = !cloudFrontWizard.detailsOpen;
  const wrap = document.getElementById('cf-tech-details-wrap');
  const toggle = document.getElementById('cf-tech-toggle');
  wrap.classList.toggle('cf-tech-open', cloudFrontWizard.detailsOpen);
  toggle.textContent = cloudFrontWizard.detailsOpen ? 'Hide technical details' : 'Show technical details';
}
```

Keep writing raw API responses into `#cf-action-output`, but make the panel visually secondary.

- [ ] **Step 6: Re-run the focused UI test and the broader CloudFront route tests**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/api -run 'TestCloudFrontAdminUIContains(ReviewAndSyncWizardStates|WizardPathChoice|CurrentSetupActions|WizardShell)' -v
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/api -run 'TestCloudFront(Bind|Scan|Plan|Sync)' -v
```

Expected: PASS.

- [ ] **Step 7: Commit the review/sync flow**

```bash
git add internal/api/web/index.html internal/api/controlplane_test.go
git commit -m "feat: complete cloudfront wizard flow"
```

---

### Task 6: Final verification and cleanup

**Files:**
- Modify: `internal/api/web/index.html`
- Modify: `internal/api/controlplane.go`
- Modify: `internal/api/controlplane_test.go`

- [ ] **Step 1: Run the focused CloudFront verification suite**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./internal/cloudfront ./internal/api ./internal/store
```

Expected: PASS.

- [ ] **Step 2: Run full regression verification**

Run:

```bash
GOCACHE=/private/tmp/v2ray-platform-go-build go test ./...
```

Expected: PASS.

- [ ] **Step 3: Run a local browser smoke flow against the wizard**

Start the control plane with the local Postgres setup already used in this checkout:

```bash
DATABASE_URL='postgres://postgres:postgres@127.0.0.1:55432/v2ray_platform?sslmode=disable' \
BOOTSTRAP_ADMIN_EMAIL='local-admin@example.com' \
BOOTSTRAP_ADMIN_PASSWORD='change-me-now' \
CONTROL_PLANE_LISTEN_ADDR='127.0.0.1:18080' \
CONTROL_PLANE_ADMIN_TOKEN='dev-admin' \
CLOUDFRONT_MASTER_KEY='0123456789abcdef0123456789abcdef' \
go run ./cmd/control-plane
```

Then verify in the browser:

- existing configured state shows the `Current setup` landing card
- choosing `Keep current setup` jumps straight to the sync step and auto-runs plan
- choosing `Change CloudFront setup` opens the wizard
- Step 1 can reuse existing credentials
- Step 2 allows existing vs managed path choice
- choosing existing with zero discovered distributions shows the managed fallback action
- technical details are collapsed by default

- [ ] **Step 4: Remove any dead CloudFront action-bar hooks left behind**

Search for legacy unused CloudFront button handlers and IDs:

```bash
rg -n "cf-discover-btn|cf-scan-btn|cf-bind-btn|cf-plan-btn|cf-sync-btn" internal/api/web/index.html
```

Expected: either no matches remain, or only matches that are intentionally reused inside the wizard. Remove dead hooks if found.

- [ ] **Step 5: Commit the verification pass**

```bash
git add internal/api/web/index.html internal/api/controlplane.go internal/api/controlplane_test.go
git commit -m "chore: verify cloudfront wizard redesign"
```

---

## Self-Review

### Spec coverage

- Wizard shell and stepper: covered in Task 2.
- Existing-config landing state: covered in Task 3.
- Existing vs managed path choice: covered in Task 4.
- Empty-state fallback to managed create: covered in Task 4.
- Review/bind/auto-plan/final sync: covered in Task 5.
- Technical details collapse: covered in Task 5.
- Verification and regression coverage: covered in Task 6.

### Placeholder scan

- No `TBD`, `TODO`, or "implement later" placeholders remain.
- Each code-changing task includes specific file targets, commands, and concrete code blocks.

### Type consistency

- Wizard step ids are consistently named `connect`, `path`, `target`, `review`, and `sync`.
- Review orchestration consistently uses `selectedDistributionId`, `prepareCloudFrontReviewStep`, and `prepareCloudFrontSyncStep`.
- The plan consistently treats `Current setup` as a pre-stepper landing state, not a stepper step.
