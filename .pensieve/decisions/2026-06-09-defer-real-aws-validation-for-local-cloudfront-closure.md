---
id: decision-2026-06-09-defer-real-aws-validation-for-local-cloudfront-closure
type: decision
title: Defer real AWS validation for local CloudFront closure
status: active
created: 2026-06-09
updated: 2026-06-09
tags: [cloudfront, aws, verification, decision]
---

# Defer real AWS validation for local CloudFront closure

## 一句话结论
> CloudFront 本地逻辑闭环不要求真实 AWS 账号接入；真实 CloudFront scan/bind/create/sync 仍是后续生产验收项。

## 上下文链接
- 基于：[[knowledge/cloudfront-subscription-implementation-map/content]]
- 相关：[[decisions/2026-06-05-keep-dual-subscription-exports-for-node-access-modes]]
- 相关：[[decisions/2026-06-05-use-stable-route-keys-for-cloudfront-node-routing]]

## Context

CloudFront 支持已经覆盖了本地控制面逻辑：双订阅导出、稳定 `route_key`、配置加密、scan/bind/plan/sync API、AWS request shape 测试、ownership guard、drift/conflict gating 和 admin UI 交接规则。当前目标明确忽略真实 AWS 账号接入。

## Problem

如果把真实 AWS E2E 验证继续作为当前本地闭环 blocker，后续 agent 会卡在没有账号或网络权限的问题上，反而无法完成可在本地证明的逻辑闭环。

如果完全删除真实 AWS 验证要求，又容易让人误以为已经证明了生产 CloudFront API 接受度。

## Alternatives Considered

- 保持真实 AWS 为当前 blocker：对生产安全最保守，但和当前目标冲突，也会让无账号环境无法收敛
- 完全移除真实 AWS 验证：执行顺畅，但会过度承诺生产 API 接受度
- 拆分本地逻辑闭环和生产 AWS 验收：当前任务可完成，同时保留上线前验证边界

## Decision

当前 CloudFront closure 以本地逻辑、测试、API/UI 状态机和文档交接为验收范围。真实 AWS scan/bind/create/sync 不阻断本地 closure，但必须继续记录为生产验证项，且不能在未跑真实 AWS 的情况下声称 production AWS API acceptance 已通过。

## Consequence

- Claude Code 可以继续从 closure plan 执行本地补缺和验证，不需要 AWS 账号
- `AGENTS.md` 和 `CLAUDE.md` 必须明确区分 local closure 与 production AWS acceptance
- 自定义入口域名 alias/DNS 可用性也归入生产验证，除非后续任务明确要求平台侧强校验

## 探索减负
- 下次可以少问什么：没有真实 AWS 账号时 CloudFront 本地闭环是否可以完成
- 下次可以少查什么：为什么 closure plan 仍保留真实 AWS 验证步骤
- 失效条件：如果后续任务明确要求上线验收、真实 AWS 账号接入、或 production API acceptance，这条决策不再免除真实 AWS 验证
