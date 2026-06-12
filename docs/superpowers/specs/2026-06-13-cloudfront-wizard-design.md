# CloudFront Wizard Redesign

## Summary

Redesign the CloudFront admin tab from a button-heavy control panel into a guided multi-step wizard. The new flow should let operators move from AWS credentials to distribution selection or creation, binding, drift review, and sync without having to mentally compose the sequence of backend actions.

This redesign keeps the existing single-bound-distribution product model. The system may scan or create many CloudFront distributions over time, but only one distribution is the current active binding at once.

## Goals

- Replace the current fragmented CloudFront flow with a clear step-by-step wizard.
- Keep the control-plane data model and backend CloudFront APIs mostly unchanged.
- Support both first-time setup and already-configured setups without forcing operators to start from scratch every time.
- Make "adopt existing" and "create new managed distribution" first-class choices.
- If the operator chooses "adopt existing" but no candidate distributions are found, transition them directly into the managed-create path.
- Preserve existing guardrails around sync gating, drift handling, and distribution switching.

## Non-Goals

- No support for multiple simultaneously active CloudFront distributions.
- No redesign of the underlying CloudFront planner, sync engine, or persistence model beyond what is necessary to support the wizard UX.
- No changes to node-agent registration, runtime routing, or non-CloudFront admin surfaces.

## Product Constraints

- The active CloudFront model remains a single current `distribution_id`.
- Switching to a different distribution invalidates previous successful sync state.
- CloudFront export remains unavailable until the bound distribution has completed a successful sync and is not in `drifted` or `conflict` state.
- Existing backend APIs for config, scan, bind, plan, and sync remain the main execution path.

## User Personas

### First-Time Operator

Needs a sequential setup path:

1. enter AWS credentials
2. choose whether to adopt an existing distribution or create a new one
3. bind the target distribution
4. review drift
5. sync

### Returning Operator

Needs to understand the current setup quickly and decide whether to:

- keep the current binding and re-check drift/sync
- switch to another existing distribution
- create and switch to a new managed distribution
- replace AWS credentials

## High-Level UX

The CloudFront tab becomes a wizard-centered screen with three layers:

1. a compact status summary at the top
2. a main stepper-based wizard in the center
3. a collapsible technical details panel at the bottom

The technical operations still exist, but they are no longer the primary interaction model.

## Layout

### Top Summary Strip

Show a compact read-only summary:

- enabled
- current distribution id
- current distribution domain
- sync status
- drift status
- last successful sync time
- masked access key id

This area communicates state only. It does not contain the main workflow buttons.

### Main Wizard Area

Render a five-step stepper with one active step at a time:

1. Connect AWS
2. Choose Setup Path
3. Select Existing or Create New
4. Review and Bind
5. Check Changes and Sync

Each step has:

- a short purpose statement
- only the fields and actions relevant to that step
- one primary action
- optional secondary back action

### Technical Details Area

Move the raw JSON and lower-level action output into a collapsible details panel:

- default collapsed
- expandable via "Show technical details"
- keeps response payloads and debug output available for troubleshooting

## Step Definitions

### Step 1: Connect AWS

Fields:

- access key id
- secret access key
- optional session token
- region
- optional custom entry host

Primary action:

- `Connect and continue`

Behavior:

- save CloudFront config
- immediately load distributions from AWS
- move to Step 2 on success
- stay on Step 1 and show inline error on failure

If a config already exists:

- show current connected state using the masked access key id
- offer:
  - `Use existing credentials`
  - `Replace credentials`

If the operator keeps existing credentials, Step 1 still performs a fresh distribution load before continuing.

### Step 2: Choose Setup Path

Present two large path choices:

- `Use existing distribution`
- `Create new managed distribution`

Selecting either path advances immediately to the matching Step 3 variant.

### Step 3A: Use Existing Distribution

Show:

- discovered distributions selector
- currently bound distribution highlighted if one exists and is still present in the list

Primary action:

- `Use selected distribution`

If the list is empty:

- do not show an error dead-end
- show empty state copy explaining that no distributions were found
- show primary action:
  - `Create a managed distribution instead`

That action automatically transitions to Step 3B.

### Step 3B: Create New Managed Distribution

Show:

- a concise explanation that the platform will create and then switch to a new managed distribution

Primary action:

- `Create distribution`

Behavior:

- call the existing managed create path
- persist the created distribution as the current target
- treat the created distribution as the selected target for the next step
- do not consider the setup fully bound or ready for sync yet
- advance to Step 4

### Step 4: Review and Bind

Show:

- selected or created distribution id and domain
- scanned origin summary
- node-to-origin matching summary
- warnings for unmatched nodes or missing routable data

Primary action:

- `Bind distribution`

Behavior:

- for existing distribution path, entering Step 4 auto-runs scan against the selected distribution
- for new managed distribution path, entering Step 4 uses the freshly created distribution state and presents the same review surface before final bind confirmation
- binding confirms that this distribution is now the active target

### Step 5: Check Changes and Sync

Show:

- drift or conflict summary
- route-level action summary
- final sync readiness state

Primary action:

- `Apply to CloudFront`

Behavior:

- auto-run plan when entering Step 5
- if the result is `conflict`, disable sync and show a clear blocking reason
- if the result is `drifted` or actionable but safe, enable final sync
- if the operator wants to refresh the plan manually, expose a secondary `Re-check changes` action rather than making it the primary path
- on sync success, render a completion state indicating CloudFront export readiness

## Existing Config Behavior

If the system already has credentials and a bound distribution:

- do not force the operator back to a blank Step 1 form
- show a `Current setup` entry state before the five-step flow
- provide these top-level choices:
  - `Keep current setup`
  - `Change CloudFront setup`

The `Current setup` state is not Step 0 inside the stepper. It is a lightweight landing state shown before the five-step wizard is entered for returning users. Once the operator chooses either path, the UI enters the normal five-step stepper.

### Keep Current Setup

Jump near the end of the flow:

- show current distribution
- show current sync state
- enter Step 5 directly with auto-plan behavior
- allow `Apply to CloudFront`
- expose `Re-check changes` as a secondary action if the operator wants to rerun plan manually

### Change CloudFront Setup

Re-enter the full wizard:

- reuse existing credentials unless the operator explicitly replaces them
- preselect the current distribution if it still exists
- allow switching to another existing distribution or creating and switching to a new one

## Distribution Switching Semantics

The redesign must explicitly communicate that:

- only one distribution is active at a time
- selecting another distribution is a switch, not an additive configuration

Recommended language:

- `Change distribution`
- `Switch to another existing distribution`
- `Create and switch to a new managed distribution`

Avoid language that implies multiple active distributions.

## Reset and Invalidation Rules

Changes to critical inputs must invalidate downstream wizard results:

- changing credentials clears loaded distributions and all downstream state
- changing custom entry host clears successful sync state
- changing selected distribution clears bind, drift, and sync results
- switching path from existing to managed or back clears target-specific downstream state

These invalidations should be reflected visually by locking later steps again until the required step is rerun.

## Action Mapping To Existing APIs

The wizard should orchestrate existing endpoints rather than replacing them:

- Step 1: `POST /api/admin/cloudfront/config` and `GET /api/admin/cloudfront/distributions`
- Step 3A or Step 4: `POST /api/admin/cloudfront/scan` with selected distribution
- Step 3B: `POST /api/admin/cloudfront/bind` with managed-create path to create and select the new target distribution
- Step 4: `POST /api/admin/cloudfront/bind`
- Step 5 planning: `POST /api/admin/cloudfront/plan`
- Step 5 sync: `POST /api/admin/cloudfront/sync`

The existing scan enhancement that accepts a selected `distributionId` remains part of the intended path.

## Visual and Interaction Rules

- The stepper should always communicate where the user is in the process.
- Each step should expose one obvious primary action.
- Advanced or raw output must not dominate the screen.
- Errors should appear inline in the current step and should not require interpretation of raw JSON.
- Completed steps should remain reviewable but visually distinct from the active step.

## Accessibility and Usability

- Buttons and step labels must be keyboard accessible.
- Step transitions should preserve focus in the active panel.
- Inline errors must be readable and associated with the relevant fields.
- Distribution selectors and path choices should remain usable on narrow laptop screens.

## Technical Implementation Scope

Primary implementation surface:

- `internal/api/web/index.html`

Expected supporting changes:

- minimal front-end state machine for wizard progression
- small API glue changes only where existing endpoints need step-aware parameters

Avoid major backend refactors unless a current endpoint shape blocks the wizard flow.

## Risks

### Wizard State Complexity

Risk:

- front-end state can become inconsistent when operators move backward or change choices

Mitigation:

- centralize wizard state transitions
- explicitly reset downstream state when upstream inputs change

### Returning User Ambiguity

Risk:

- returning users may not understand whether they are editing, reusing, or replacing the current setup

Mitigation:

- add a clear current setup entry state
- use "switch distribution" wording consistently

### Technical Debt Hiding

Risk:

- hiding all low-level details can make troubleshooting harder

Mitigation:

- keep an expandable technical details section with raw output

## Acceptance Criteria

- A first-time operator can complete CloudFront setup without manually inferring the correct button order.
- A returning operator can understand the current bound distribution and decide whether to keep or change it.
- Choosing existing distribution with zero discovered results automatically routes the operator toward managed creation.
- Changing the active distribution clears stale successful sync readiness.
- The screen still exposes enough detail for debugging through an advanced technical details area.
