---
id: knowledge-cloudfront-subscription-implementation-map
type: knowledge
title: CloudFront subscription implementation map
status: active
created: 2026-06-05
updated: 2026-06-09
tags: [cloudfront, subscription, control-plane, nodes]
---

# CloudFront subscription implementation map

## Source

- 设计文档：[/Users/yxn/Github/v2ray-platform/docs/superpowers/specs/2026-06-05-cloudfront-subscription-design.md](/Users/yxn/Github/v2ray-platform/docs/superpowers/specs/2026-06-05-cloudfront-subscription-design.md)
- 实现计划：[/Users/yxn/Github/v2ray-platform/docs/superpowers/plans/2026-06-05-cloudfront-subscription-implementation.md](/Users/yxn/Github/v2ray-platform/docs/superpowers/plans/2026-06-05-cloudfront-subscription-implementation.md)
- 收口计划：[/Users/yxn/Github/v2ray-platform/docs/superpowers/plans/2026-06-09-cloudfront-support-closure.md](/Users/yxn/Github/v2ray-platform/docs/superpowers/plans/2026-06-09-cloudfront-support-closure.md)

## Summary

CloudFront 首期是控制面能力。它新增 CloudFront config CRUD、distribution discovery/create/bind、scan/bind/plan/sync 服务管线、Clash-CF YAML 渲染、admin UI 和加密存储；node-agent 注册、心跳、用量上报、runtime 用户增删和 V2Ray config generation 不属于首期变更范围。

## Content

### Architecture: Scan → Bind → Plan → Sync pipeline

```
Admin UI
  ├─ POST /api/admin/cloudfront/config     → 保存加密凭证
  ├─ POST /api/admin/cloudfront/toggle     → 启用/禁用
  ├─ GET  /api/admin/cloudfront/distributions → AWS ListDistributions()
  ├─ POST /api/admin/cloudfront/scan       → ScanService.ScanDistribution()
  ├─ POST /api/admin/cloudfront/bind       → scan selected distribution or create managed distribution, then BindService.BindNodes()
  ├─ POST /api/admin/cloudfront/plan       → fetch live distribution, then cloudfront.Plan()
  └─ POST /api/admin/cloudfront/sync       → fetch live distribution, plan, then SyncService.ExecutePlan()
```

### File map

| Layer | File | What |
|-------|------|------|
| Domain | `internal/domain/types.go` | `CloudFrontConfig`, `CloudFrontOrigin`, `CloudFrontBinding`, `CloudFrontSyncAction`; `Node.RouteKey` |
| Store DTO | `internal/store/store.go` | `SaveCloudFrontConfigInput`, `UpdateCloudFrontSyncInput` and related CloudFront store methods |
| Store PG | `internal/store/postgres.go` | `INSERT ... ON CONFLICT` upsert; conditional `COALESCE(NULLIF(...))` for sync fields |
| Store Mem | `internal/store/memory.go` | In-memory mirror with `sync.RWMutex` |
| Migration | `migrations/0012_cloudfront_route_keys_and_config.sql` | `route_key` column + `cloudfront_configs` singleton table |
| Related pattern | `knowledge/control-plane-global-settings-pattern/content.md` | Control-plane singleton config pattern reused by CloudFront and later platform settings |
| Crypto | `internal/crypto/secrets.go` | `SecretCodec` — AES-256-GCM, base64 encoding, empty-string passthrough |
| Config | `internal/config/config.go` | `CloudFrontMasterKey` env var |
| Client iface | `internal/cloudfront/client.go` | `Client` interface: `ListDistributions`, `GetDistribution`, `ApplyDistributionRoutes`; `AWSClient` also supports `CreateDistribution` |
| AWS client | `internal/cloudfront/aws_client.go` | CloudFront REST/XML client, SigV4 signed as `us-east-1/cloudfront`, ETag `If-Match` distribution updates, managed rewrite function |
| Scan | `internal/cloudfront/scan.go` | Fetch AWS distribution state, reconstruct route table from cache behaviors plus origins, persist scan cache |
| Bind | `internal/cloudfront/bind.go` | `BindService.BindNodes()` — match nodes to origins by `route_key` |
| Plan | `internal/cloudfront/plan.go` | Compare desired bindings vs live distribution origins/behaviors, produce `RouteAction` list plus rewrite map |
| Sync | `internal/cloudfront/sync.go` | `SyncService.ExecutePlan()` — execute route actions and rewrite function updates, then update sync status |
| Handlers | `internal/api/controlplane.go` | Config CRUD/toggle, discovery, scan, bind, plan, sync, clash-cf.yaml public + admin endpoints |
| UI | `internal/api/web/index.html` | CloudFront section: credentials, discovery, bind/create, plan, sync, gated CF download/copy actions |
| Main | `cmd/control-plane/main.go` | `SecretCodec` init from `CLOUDFRONT_MASTER_KEY` |

### Key design patterns

- **Singleton config**: `cloudfront_configs` has exactly one row with `id = 'cf-global'`
- **Encrypted credentials**: `EncryptedAccessKeyID`, `EncryptedSecretAccessKey`, `EncryptedSessionToken` — AES-256-GCM, never returned to UI after save
- **Credential masking**: GET endpoint decrypts AccessKeyID, shows last 4 chars only
- **Global settings authority**: CloudFront follows the control-plane singleton settings pattern instead of hiding platform-wide defaults in node/member models or install-script constants
- **Route key**: `randomToken(8)` → 16-char hex, generated at node registration, immutable
- **Routing model**: CloudFront cache behavior selects origin by `/{route_key}`; the managed CloudFront Function `v2ray-platform-route-rewrite` rewrites the URI to the node's direct websocket path `/{node.name}` before origin fetch
- **Live AWS state authority**: plan/sync fetch the live distribution every time; stored `origins_json` is scan cache only
- **Drift detection**: `Plan()` compares desired bindings against live origins/behaviors, produces `add_route | replace_route | remove_route | conflict | noop`
- **Ownership guard**: sync mutates only platform-managed origins/behaviors and refuses unmanaged route overwrite/removal
- **Sync status**: `idle → synced/failed`; `drift_status` records `in_sync | drifted | conflict`; `last_successful_sync_at` updates only on successful sync and is cleared when distribution metadata or bindings change
- **Enabled toggle**: explicit `enabled` boolean; CloudFront subscription export remains unavailable until enabled, bound/created, successfully synced, and not known `drifted`/`conflict`

### Subscription rendering

- Direct: `GET /sub/{token}/clash.yaml` → `server = node.PublicHost`, `ws-path = /{node.Name}`
- CloudFront: `GET /sub/{token}/clash-cf.yaml` → `server = config.CustomEntryHost` when set, otherwise `config.DistributionDomainName`; `ws-path = /{node.RouteKey}`
- Gating: if CloudFront is disabled, unbound, never successfully synced, latest sync failed, or stored drift status is `drifted/conflict`, the CloudFront endpoint returns an explicit unavailable error instead of rendering a direct fallback

### API field naming convention

All API request/response fields use **camelCase** (Go JSON tags). The admin UI must send and read camelCase, not snake_case. This was a critical bug found in final review.

## When to Use

- 当任务涉及 CloudFront 订阅导出
- 当需要定位"CloudFront 该改哪里，哪些链路不用碰"
- 当需要理解 scan/bind/plan/sync 管线
- 当需要新增 CloudFront 相关 API 或修改渲染逻辑

## 上下文链接
- 基于：[[knowledge/taste-review/content]]
- 导致：[[decisions/2026-06-05-keep-dual-subscription-exports-for-node-access-modes]]
- 相关：[[knowledge/control-plane-global-settings-pattern/content]]
- 相关：[[decisions/2026-06-05-use-stable-route-keys-for-cloudfront-node-routing]]
