---
id: short-term-knowledge-admin-ui-copy-fallbacks
type: knowledge
title: Admin UI copy fallbacks for subscription and install links
status: active
created: 2026-06-19
updated: 2026-06-19
tags: [admin-ui, clipboard, subscription, cloudfront, debugging]
---

# Admin UI copy fallbacks for subscription and install links

## Source

- UI implementation: [/Users/yxn/.codex/worktrees/98a5/v2ray-platform/internal/api/web/index.html](/Users/yxn/.codex/worktrees/98a5/v2ray-platform/internal/api/web/index.html)
- UI contract test: [/Users/yxn/.codex/worktrees/98a5/v2ray-platform/internal/api/controlplane_test.go](/Users/yxn/.codex/worktrees/98a5/v2ray-platform/internal/api/controlplane_test.go)

## Summary

管理页里的复制类按钮不能假设 `navigator.clipboard.writeText` 一定存在；订阅链接和安装命令必须在复制失败或 clipboard 不可用时降级为可见、可手动复制的文本提示。

## Content

### Symptom

- 点击 `Sub🔗` 没有生成可见链接
- 浏览器控制台报 `(index):1662 TypeError: Cannot read properties of undefined (reading 'writeText')`

### Root cause

- 单文件管理页直接调用 `navigator.clipboard.writeText(...)`
- 某些浏览器上下文、扩展注入环境或非安全上下文里，`navigator.clipboard` 可能不存在
- 复制逻辑抛异常后，后续的手动复制提示不会执行，用户体感就是按钮无效

### Durable fix pattern

- 将复制逻辑收口到公共 helper，例如 `copyTextWithFallback(text, promptMessage, onSuccess)`
- 先检查 `globalThis.navigator?.clipboard?.writeText`
- 可用时执行复制并触发成功提示
- 不可用或复制失败时，使用 `prompt(...)` 直接展示完整文本，让用户仍然能拿到链接或命令

### Current coverage

- direct subscription `Sub🔗`
- CloudFront subscription `Sub CF🔗`
- node install command copy button

### Regression signal

- 页面源码应继续保留 `copyTextWithFallback(...)`
- 订阅复制入口应调用该 helper，而不是再次直接写 `navigator.clipboard.writeText(...)`
- `TestAdminUISubscriptionCopyFallsBackWithoutClipboard` 用于锁定这个契约

## When to Use

- 当管理页里新增任何“复制到剪贴板”按钮时先读
- 当用户反馈“按钮点了没反应，但控制台有 clipboard / writeText 报错”时先读
- 当需要判断这是后端没生成链接还是前端复制链路中断时先读

## 上下文链接
- 基于：[[knowledge/cloudfront-subscription-implementation-map/content]]
- 相关：[[decisions/2026-06-05-keep-dual-subscription-exports-for-node-access-modes]]
