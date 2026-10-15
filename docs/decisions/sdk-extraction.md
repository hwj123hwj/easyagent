# SDK 边界

2026-09-06 的 SDK 抽取已经实施；以下为当前维护约定。

`sdk/` 提供外部 Go 模块可导入的 Agent 原子能力，模块路径为 `github.com/hwj123hwj/easyagent/sdk/...`。`internal/` 保留 EasyAgent 自身的入口适配与领域应用。SDK 不得反向依赖 internal，由 [架构测试](../../sdk/arch_test.go) 约束。

模型、工具、会话与执行后端通过接口扩展；音乐、飞书和产品界面不进入 SDK。需要嵌入的使用者可参考 [SDK 示例测试](../../sdk/example_test.go)，避免复制旧内部目录 import 路径。整体分层见 [架构](../ARCHITECTURE.md)。
