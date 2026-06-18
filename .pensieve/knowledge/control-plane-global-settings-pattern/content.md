---
id: knowledge-control-plane-global-settings-pattern
type: knowledge
title: Control-plane global settings pattern
status: active
created: 2026-06-18
updated: 2026-06-18
tags: [control-plane, settings, singleton, persistence]
---

# Control-plane global settings pattern

## Source

- `internal/store/store.go`
- `internal/store/memory.go`
- `internal/store/postgres.go`
- `migrations/0012_cloudfront_route_keys_and_config.sql`
- `migrations/0013_platform_settings.sql`

## Summary

当控制面需要保存“整个平台只有一份”的后台配置时，使用单例表 + store upsert + API 默认响应的模式，比把开关塞进现有业务对象或散落在环境变量里更稳定。

## Content

### Pattern

控制面全局设置统一采用下面的结构：

1. 数据库存一张单例表，主键固定，例如 `cf-global`、`platform-global`
2. store 暴露 `Get...Settings()` / `Save...Settings()`，内存和 Postgres 两套实现保持同构
3. 读取不到记录时，API 返回显式默认值，而不是把“未配置”传播到前端
4. 安装脚本或后台行为读取这个全局设置，再决定默认输出

### Why this pattern exists

- 避免把“平台级开关”错误地下沉到 node/member 等局部模型
- 避免前端自己推断默认值，导致 UI 和后端行为漂移
- 避免把运行时默认行为硬编码在安装脚本里，改一次要追多处
- 让 memory store 和 postgres store 的测试边界保持一致

### Current examples

- `cloudfront_configs`: CloudFront 凭证、distribution 绑定、sync 状态
- `platform_settings`: 流量采集默认开关 `usageCollectionEnabled`

### Boundaries and ownership

- 平台级设置归 control-plane 持有；node-agent 只消费最终下发的结果
- 这类设置应该影响“默认行为”或“后台能力开关”，不应该承载成员级、节点级、授权级状态
- 如果某个开关只对单个节点生效，它不该进入单例表

### Anti-patterns

- 把新的后台开关塞进不相关的配置表，只因为“那里已经有一张单例表”
- UI 默认值写死，但 API 默认值不同
- 安装脚本硬编码默认行为，后台配置无法改变后续生成结果

### Verification signals

- 新增设置时，memory store / postgres store / migration / admin API / UI 都能一一对应
- 没有数据库记录时，`GET` API 仍返回稳定默认值
- 改动设置后，新生成的 install/export 结果会跟着变化

## When to Use

- 当要新增“后台可切换、全平台共用”的配置
- 当安装脚本、导出配置或后台默认行为需要持久化开关
- 当不确定某个新配置该放在环境变量、节点模型还是独立单例表时

## 上下文链接
- 基于：[[knowledge/taste-review/content]]
- 相关：[[knowledge/cloudfront-subscription-implementation-map/content]]
- 相关：[[decisions/2026-06-05-keep-dual-subscription-exports-for-node-access-modes]]
