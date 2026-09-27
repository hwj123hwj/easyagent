# 工作流：动态 Actor 与 YAML DAG 并存

早期仅采用 Go 原生 YAML DAG；当前动态工作流已调整为复用固定版本的 ZCode TypeScript 引擎。原先“不引入 Node”的选择不再适用于动态编排。

| 需求 | 引擎与入口 |
|---|---|
| Agent 根据中间结果分支、循环、并行组织多个 Actor | ZCode 动态引擎 + Go 会话适配；对话 `/workflow`，Web「工作流」 |
| 预先确定的步骤依赖、重试、fan-out、人工确认与缓存 | Go `sdk/workflow`；`/workflows` API，Web「流水线」 |

Go 保留模型、会话与工具权限的控制；Node 组件只接通已约定的调度宿主接口。动态运行日志支持受控恢复，但未完成 ask 的副作用可能重复，不能承诺恰好执行一次。YAML 引擎的运行和恢复限制独立维护。

这项选择增加了 Node.js 22+ 与 bundle 的发布要求；核心、桥接、bundle 按同一 SHA 验证与更新。上游代码来源和许可保留在 [vendor/SOURCE.md](../../workflow-runtime/vendor/SOURCE.md)，发布必须带上相应许可与 NOTICE。

实际接入契约见 [动态工作流](../DYNAMIC_WORKFLOW.md)，固定流程语法见 [YAML 流水线](../WORKFLOW.md)。
