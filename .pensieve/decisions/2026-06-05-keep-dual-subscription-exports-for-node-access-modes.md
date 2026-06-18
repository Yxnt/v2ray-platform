---
id: decision-2026-06-05-dual-subscription-exports
type: decision
title: Keep dual subscription exports for node access modes
status: active
created: 2026-06-05
updated: 2026-06-05
tags: [cloudfront, subscription, clash, decision]
---

# Keep dual subscription exports for node access modes

## 一句话结论
> 保留普通订阅和 CloudFront 订阅两条导出链路，而不是把两种访问模式混进同一个 Clash 订阅。

## 上下文链接
- 基于：[[knowledge/cloudfront-subscription-implementation-map/content]]
- 导致：[[decisions/2026-06-05-use-stable-route-keys-for-cloudfront-node-routing]]
- 相关：[[knowledge/control-plane-global-settings-pattern/content]]
- 相关：[[knowledge/taste-review/content]]

## Context

当前项目已经有稳定工作的公开 Clash 订阅入口和按成员授权生成节点列表的逻辑。用户希望新增 CloudFront 统一入口加速，但也明确要求保留不走 CloudFront 的普通使用方式。

## Problem

如果把直连节点和 CloudFront 节点混到同一个订阅里：

- 节点列表会膨胀
- 用户容易误选
- 后续排查问题时很难快速判断是直连链路还是 CloudFront 链路

如果直接把现有订阅整体切到 CloudFront，又会破坏现有用户的直接连接方式。

## Alternatives Considered

- 单一订阅同时包含直连和 CloudFront 节点：配置冗余，用户体验差，排障边界不清
- 单一订阅完全切换到 CloudFront：破坏现有直连兼容性
- 每个节点单独配置是否走 CloudFront：模型扩散到节点层，增加管理和渲染复杂度

## Decision

保留两种订阅导出视图：

- direct: `/sub/{token}/clash.yaml`
- CloudFront: `/sub/{token}/clash-cf.yaml`

它们共享同一套成员授权、节点分组、relay 关系和流量统计，只在最终 YAML 渲染时改变连接入口。

## Consequence

- 现有 direct 逻辑可以保持稳定
- CloudFront 作为增量能力接入，迁移风险低
- 成员页面和公开订阅都需要同时支持两个 URL 和两个 YAML 下载动作
- CloudFront 订阅是否可见需要受同步状态约束

## 探索减负
- 下次可以少问什么：是否要把直连和 CloudFront 混在同一份订阅里
- 下次可以少查什么：为什么 CloudFront 模式没有扩散到节点授权模型里
- 失效条件：如果未来产品明确要求“一个订阅内动态切换多种入口模式”，这条决策需要重审
