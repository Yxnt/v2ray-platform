# CloudFront Subscription and Management Design

## Summary

This project already supports:

- a public Clash subscription endpoint per member
- per-node direct connectivity using each node's `public_host`
- per-member authorization and usage tracking

This design adds first-class AWS CloudFront support with two goals:

1. members can choose between a direct subscription and a CloudFront-backed subscription
2. administrators can configure and manage CloudFront inside this control plane instead of manually editing AWS for each node route

The design keeps the existing direct path unchanged and adds CloudFront as a second subscription rendering mode plus a managed infrastructure integration.

## Goals

- Keep the existing direct subscription flow fully compatible
- Expose two subscription URLs per member: direct and CloudFront
- Route all CloudFront-backed Clash nodes through a single entry hostname
- Reuse current node data where possible, especially `public_host`
- Avoid obvious per-node CloudFront-only fields such as `cf_path` or `cf_host`
- Let the platform either create a new CloudFront distribution or bind to an existing one
- Detect drift when users manually change CloudFront in AWS
- Store AWS credentials in the database using encryption

## Non-Goals

- No changes to node-agent registration, heartbeat, config pull, usage reporting, or dynamic add/remove user behavior
- No replacement of the existing direct subscription URL
- No attempt to fully own every behavior in an existing CloudFront distribution
- No per-member or per-node choice of CloudFront mode inside one subscription URL

## User Experience

Each member has two downloadable subscription outputs:

- direct: `/sub/{token}/clash.yaml`
- CloudFront: `/sub/{token}/clash-cf.yaml`

The admin UI also exposes both variants in the member actions area:

- download direct YAML
- copy direct URL
- download CloudFront YAML
- copy CloudFront URL

The CloudFront subscription is only available after:

- CloudFront has been configured and bound
- at least one successful CloudFront sync has completed
- the current CloudFront config is not in a known-invalid state

If those conditions are not met, the CloudFront subscription endpoint returns an explicit error and the admin UI disables or warns on the CloudFront export actions.

## High-Level Architecture

The feature has three distinct concerns:

1. Subscription rendering
   - direct subscription continues to render nodes using `node.public_host`
   - CloudFront subscription renders the same authorized nodes using one shared entry host

2. CloudFront integration
   - create a distribution or bind to an existing one
   - compute the desired path-to-origin mapping from current nodes
   - sync only platform-managed CloudFront objects

3. CloudFront state management
   - store encrypted AWS credentials and CloudFront settings
   - track last sync status, last successful sync, drift, and errors

Existing grant logic, node grouping, relay logic, member usage, and quota enforcement remain unchanged.

## Routing Model

### Direct Subscription

The direct subscription remains as-is:

- `server = node.public_host`
- `ws path = "/" + node.name`

### CloudFront Subscription

The CloudFront subscription changes only the rendered endpoint data:

- `server = configured CloudFront entry hostname`
- `ws path = "/" + node.route_key`
- `uuid = member.uuid`
- relay `dialer-proxy` logic remains unchanged

CloudFront then routes each request path to the matching node origin:

- `/{route_key}` -> `node.public_host`

Because the current node runtime still listens on the direct WebSocket path `/{node.name}`, the platform-managed CloudFront distribution must also attach a viewer-request CloudFront Function to every platform-managed behavior. The function rewrites the request URI from `/{route_key}` to the node's current direct WebSocket path before the request reaches the origin. CloudFront selects the cache behavior and origin before this function rewrite, so the stable route key remains the external routing key while the origin receives the path it already expects.

This keeps authorization and member-to-node visibility identical between direct and CloudFront variants. Only the connection endpoint rendering changes.

## Stable Route Key

Using `node.name` as a CloudFront path is fragile because renaming a node would change the path and invalidate existing routing behavior. To avoid that, each node gets a stable route key.

Requirements:

- generated once when the node is registered
- persisted in the database
- never changed automatically after creation
- unique across all nodes
- opaque enough to avoid exposing business meaning

Recommended format:

- random 8 to 12 character slug, or
- UUID-derived short stable token

This value is not treated as a CloudFront-only field in the product model. It is the node's stable external route identifier.

## Path Normalization and Uniqueness

CloudFront path routing must be deterministic and collision-free.

Rules:

- the platform uses only the stable `route_key` for CloudFront path generation
- the final path format is `/{route_key}`
- route keys must be unique at insert time and protected by a database unique constraint
- the route key is treated as case-sensitive in storage, but generated values should use a normalized lowercase alphabet to avoid ambiguous operator behavior

Because the path does not come from `node.name`, rename operations do not change CloudFront routes.

## CloudFront Global Configuration

CloudFront configuration is global to the control plane, not per node.

Suggested stored settings:

- `enabled`
- `mode`
  - `create`
  - `bind`
- encrypted `aws_access_key_id`
- encrypted `aws_secret_access_key`
- `aws_region` for SDK initialization
- optional custom entry hostname such as `edge.example.com`
- bound `distribution_id`
- bound `distribution_domain_name`
- `last_sync_status`
- `last_sync_error`
- `last_sync_at`
- `last_successful_sync_at`
- `drift_status`

The effective CloudFront entry host used in rendered subscriptions is:

1. custom entry hostname if configured and usable
2. otherwise the CloudFront distribution domain name

## AWS Credential Storage

AWS credentials are stored in the database in encrypted form.

Requirements:

- plaintext secrets are never returned by the API after save
- the secret field is masked in UI responses
- encryption uses a server-side master key from environment configuration
- updates support "keep existing secret" semantics when only non-secret fields change
- all credential changes are audited

If the master key is missing, CloudFront configuration save and use must fail explicitly.

## Distribution Management Modes

### Mode A: Create New Distribution

The platform:

- validates AWS credentials
- creates a CloudFront distribution
- creates platform-managed origins for nodes
- creates platform-managed path behaviors for node route keys
- stores the resulting `distribution_id` and `distribution_domain_name`

### Mode B: Bind Existing Distribution

The platform:

- scans available distributions using AWS APIs
- shows candidates in the admin UI
- allows the administrator to select one
- reads the selected distribution configuration
- computes a management plan that only affects platform-managed objects

The admin never needs to manually enter a `distribution_id` in the normal UI flow.

## Distribution Discovery UX

For existing distributions, the admin flow is:

1. save AWS credentials
2. test AWS connectivity
3. click scan distributions
4. choose one distribution from the returned list
5. inspect the sync plan
6. bind and sync

Candidate distributions should display:

- distribution domain name
- aliases
- comment/description
- status
- number of origins
- whether platform-managed resources were detected

## Managed Resource Identification

The platform must be able to identify which CloudFront resources it owns, especially when binding to an existing distribution.

Recommended origin ID pattern:

- `v2ray-platform-node-{nodeID}`

Managed behaviors are identified by:

- path pattern using `/{route_key}`
- origin target ID matching the platform origin naming convention

The platform also owns one CloudFront Function named `v2ray-platform-route-rewrite`. It contains only the route-key-to-node-path rewrite map for platform-managed CloudFront behaviors.

Rules:

- only platform-managed origins and behaviors may be updated or removed automatically
- only the platform-managed rewrite function may be created, updated, published, or attached automatically
- unmanaged user-defined distribution items are read but not modified
- if unmanaged items conflict with a planned managed path, sync must stop with a conflict error

## Sync Plan and Drift Detection

Every sync operation begins with a live read from AWS and a computed plan. The platform never assumes remote CloudFront state is unchanged.

Possible plan statuses:

- `in_sync`
- `create`
- `update`
- `delete`
- `drifted`
- `conflict`

Definitions:

- `drifted`: a platform-managed CloudFront object still exists and is recognizable, but its details differ from the locally expected config
- `conflict`: the desired platform path or origin cannot be applied safely because another remote object already occupies that space and is not clearly platform-managed

Admin actions for drift:

- overwrite remote with platform state
- accept remote as current baseline and re-bind

The platform should surface a sync preview showing:

- paths to create
- origins to create
- paths to update
- origins to update
- managed paths/origins to delete
- detected drift/conflicts

## Sync Triggers

Initial implementation should prefer explicit sync from the UI.

Rationale:

- CloudFront updates are external infrastructure changes with slower deployment and heavier failure modes than local DB writes
- explicit sync is easier to reason about during rollout

Automatic sync can be added later after the manual flow is proven stable.

Any of these events may produce pending CloudFront changes:

- node added
- node deleted
- node `public_host` changed
- route key creation for existing nodes during migration

Node rename does not affect CloudFront routing because route keys are stable.

## Subscription Rendering Rules

### Direct YAML

No behavioral changes from current implementation.

### CloudFront YAML

For each authorized node:

- `server` uses the CloudFront entry host
- `ws-opts.path` uses `/{route_key}`
- `uuid` uses the member UUID
- `dialer-proxy` remains resolved by accessible relay node name

The direct and CloudFront subscriptions are just two renderings of the same authorized node set.

## CloudFront Endpoint Availability

The public endpoint `GET /sub/{token}/clash-cf.yaml` is enabled only when:

- CloudFront config exists
- a distribution has been bound or created
- at least one successful sync has completed

Failure modes:

- no CloudFront configured: return a clear not-configured error
- member inactive or expired: keep current member validation behavior
- sync never succeeded: return a clear unavailable error

## Admin UI

Add a dedicated `CloudFront` section to the admin UI.

Suggested sections:

1. Credentials
   - access key ID
   - secret access key
   - region

2. Entry host
   - optional custom hostname
   - current effective distribution domain name

3. Distribution mode
   - create new distribution
   - scan and bind existing distribution

4. Discovery
   - test AWS connectivity
   - scan distributions
   - choose a distribution candidate

5. Sync preview
   - list planned creates, updates, deletes
   - list drift/conflicts

6. Execution status
   - last sync status
   - last sync time
   - last success time
   - current drift state
   - last error

## API Additions

Suggested admin endpoints:

- `GET /api/admin/cloudfront/config`
- `PUT /api/admin/cloudfront/config`
- `POST /api/admin/cloudfront/test`
- `GET /api/admin/cloudfront/distributions`
- `POST /api/admin/cloudfront/bind`
- `GET /api/admin/cloudfront/plan`
- `POST /api/admin/cloudfront/sync`
- `GET /api/admin/cloudfront/status`

Suggested member/admin YAML download endpoints:

- existing `GET /api/admin/members/{memberID}/clash.yaml`
- new `GET /api/admin/members/{memberID}/clash-cf.yaml`

Suggested public endpoints:

- existing `GET /sub/{token}/clash.yaml`
- new `GET /sub/{token}/clash-cf.yaml`

## Data Model Changes

### Node

Add:

- `route_key`

Properties:

- required after migration
- unique
- generated once and immutable

### CloudFront Config

Add a new persisted configuration object or table containing:

- encrypted AWS credential fields
- distribution mode
- distribution binding metadata
- effective entry host metadata
- sync status and timestamps
- drift status

### Audit Log

Audit these actions:

- CloudFront credentials saved/updated
- AWS connectivity test
- distribution scan
- distribution bind
- sync plan generation
- sync execution
- drift override or baseline acceptance

## Migration Strategy

A migration is required to add stable route keys to existing nodes.

Recommended approach:

1. add nullable `route_key`
2. backfill all existing nodes with generated unique values
3. add unique index
4. make `route_key` non-null

This migration must run before CloudFront sync is usable.

Existing direct subscriptions continue to function during and after migration.

## Error Handling

### Validation Errors

- missing or invalid AWS credentials
- missing encryption master key
- no selected distribution in bind mode
- missing node `public_host`
- duplicate route key

### Plan Errors

- remote distribution inaccessible
- conflicting unmanaged path/origin detected
- drift prevents safe mutation without explicit operator choice

### Sync Errors

- AWS API call failure
- distribution update rejection
- partial update failure
- deployment not reaching a healthy state in expected time

The platform should keep the last successful sync metadata even when a new sync attempt fails.

## Operational Constraints

- CloudFront deployments are not instant; the UI must communicate that propagation can lag behind sync completion
- deleting a node should show the pending CloudFront removals in the sync preview before execution
- changing `public_host` requires another CloudFront sync before CloudFront subscriptions reflect the new origin target
- manual AWS-side edits are expected and must be surfaced as drift, not silently overwritten

## Testing Strategy

Testing should cover:

- direct subscription unchanged behavior
- CloudFront subscription rendering
- route key generation and uniqueness
- migration backfill behavior
- config encryption and masked reads
- AWS integration mocking for test, scan, bind, plan, and sync
- drift detection
- conflict detection with unmanaged remote resources
- endpoint availability gating for `clash-cf.yaml`

## Risks

1. CloudFront sync complexity is higher than current local-only features
2. encrypted credential storage introduces key management requirements
3. binding to existing distributions can be dangerous without strict ownership boundaries
4. CloudFront rollout lag may confuse operators if status messages are vague

## Recommended Implementation Order

1. add stable `route_key` to nodes
2. add CloudFront config persistence with encryption support
3. add CloudFront subscription rendering endpoint
4. add admin CloudFront config APIs
5. add AWS test/scan/bind flows
6. add sync planning and managed-resource reconciliation
7. add UI for CloudFront configuration and preview
8. add drift handling and operator decision flows

## Open Decisions Resolved in This Spec

- two subscription URLs: yes
- CloudFront managed in phase one: yes
- AWS auth method: Access Key
- support both create-new and bind-existing distribution modes: yes
- existing distributions are selected from scan results, not by manual ID entry
- CloudFront routing path uses a stable node route key, not node name
- credentials are stored in the database using encryption
- CloudFront subscription becomes user-visible only after at least one successful sync
