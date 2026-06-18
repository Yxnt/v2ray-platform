---
id: decision-2026-06-05-stable-route-key
type: decision
title: Use stable route keys for CloudFront node routing
status: active
created: 2026-06-05
updated: 2026-06-05
tags: [cloudfront, routing, nodes, decision]
---

# Use stable route keys for CloudFront node routing

## 一句话结论
> CloudFront 节点路径使用节点持久化的稳定 `route_key`，而不是直接使用 `node.name`。

## 上下文链接
- 基于：[[knowledge/cloudfront-subscription-implementation-map/content]]
- 相关：[[decisions/2026-06-05-keep-dual-subscription-exports-for-node-access-modes]]
- 相关：[[knowledge/taste-review/content]]

## Context

CloudFront 方案需要把统一入口域名下的不同 path 转发到不同节点源站。最初讨论里尝试过直接用 `node.name` 作为路径，因为当前 V2Ray 渲染也在用 `"/" + node.name"` 作为 websocket path。

## Problem

`node.name` 是人类可读名称，适合作为 UI 标识，但不适合作为长期稳定的外部路由键：

- 改名会导致 CloudFront path 变化
- 路由规则和用户订阅会一起失效
- 名称可能暴露业务含义
- 名称去重和字符规范比随机稳定标识更脆弱

## Alternatives Considered

- 直接使用 `node.name`：实现快，但重命名风险高
- 每次动态生成 UUID4：无法稳定，重复同步会漂移
- 为节点新增稳定 `route_key`：需要 migration，但边界最清晰

## Decision

给每个节点增加一个持久化的稳定 `route_key`：

- 节点注册时生成一次
- 写入数据库
- 后续不自动变化
- CloudFront 路由路径统一使用 `/{route_key}`

direct 模式可以继续保持现有 `node.name` 驱动的渲染，不强制一起改。

## Consequence

- 需要一次 schema migration 和历史节点 backfill
- 节点重命名不再影响 CloudFront 路由
- CloudFront 托管规则更容易做 drift 检测和资源识别
- 后续如果 direct 模式也想摆脱 `node.name`，可以独立评估，不与 CloudFront 首期耦合

## 探索减负
- 下次可以少问什么：CloudFront path 是否应该跟着节点名变化
- 下次可以少查什么：为什么不能每次同步时临时生成 path
- 失效条件：如果未来节点对外访问协议完全不再依赖 path 路由，这条决策可能失效
