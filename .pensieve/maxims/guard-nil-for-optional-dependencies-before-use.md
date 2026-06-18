---
id: maxim-guard-nil-optional-deps
type: maxim
title: Guard nil for optional dependencies before use
status: active
created: 2026-06-05
updated: 2026-06-05
tags: [nil-safety, dependency-injection, error-handling]
---

# Guard nil for optional dependencies before use

## 一句话结论
> 当一个依赖可能为 nil（可选配置、延迟初始化、条件注入），在每次使用前显式检查 nil，返回明确错误而非 panic。

## 指导规则
- 依赖注入时如果某个依赖可能未配置（如 master key 未设置），该依赖的指针应为 nil
- handler/service 在调用该依赖前必须检查 nil，返回 HTTP 503 或等效的明确错误
- 不要依赖"文档上说必须配置"来跳过 nil 检查
- nil 检查应放在 handler 入口处，而非嵌套在业务逻辑深处

## 边界
- 当依赖是必须的（如 store），构造时就应该 panic 或返回 error，而不是运行时检查
- 当依赖有默认值（如 fallback 策略），用 option pattern 而非 nil 检查

## 上下文链接
- 基于：[[knowledge/control-plane-global-settings-pattern/content]]
- 相关：[[knowledge/taste-review/content]]
- 相关：[[maxims/preserve-user-visible-behavior-as-a-hard-rule]]
