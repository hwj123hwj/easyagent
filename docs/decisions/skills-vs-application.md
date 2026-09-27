# Skills 与 Application 的边界

两者解决不同层面的问题。

- **Skill**：为已有 Agent 提供任务方法、提示、参考文件和脚本，复用当前会话与工具权限。适用于规范、流程和领域知识。
- **Application**：需要独立工具集、系统提示或会话扩展状态时，使用 `runtime.Application` 的 `BuildTools`、`BuildPrompt`、`NewSessionExt`。当前入口注册 coding、music、kb。
- **Workflow**：任务需要多个 Agent 会话协作、并行或分支时使用工作流；它既不替代 Application，也不自动授予 Skill 更高权限。

先复用已有工具与 Skill；只有独立运行状态和能力边界足以支撑时才增加 Application。无论入口如何变化，文件策略、API 认证和确认回调应保持明确。接口位置见 [架构](../ARCHITECTURE.md)，编排取舍见 [工作流设计](workflow-engine.md)。
