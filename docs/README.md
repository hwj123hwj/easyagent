# EasyAgent 文档

使用说明以当前源码为准。项目定位见 [项目首页](../README.md)，Web 产品与视觉约定见 [PRODUCT.md](../PRODUCT.md) 和 [DESIGN.md](../DESIGN.md)。

## 使用与运行

| 文档 | 内容 |
|---|---|
| [配置](CONFIG.md) | 模型、环境变量、文件访问策略、认证 |
| [飞书](FEISHU.md) | 网页配置、长连接、首次配对、故障定位 |
| [动态工作流](DYNAMIC_WORKFLOW.md) | `/workflow`、ZCode 引擎接入、并发与恢复 |
| [YAML 流水线](WORKFLOW.md) | 固定 DAG、模板、重试、审批与缓存 |
| [迷你主机部署](MINI_DEPLOY.md) | 自动更新、systemd 自启动、健康检查与回滚 |
| [版本与发布](RELEASING.md) | tag、版本说明、发布校验与安装完整性 |
| [API](API.md) | 对话、流式事件、会话和管理接口 |

## 开发与设计

- [架构](ARCHITECTURE.md)：源码分层、数据流和公共 SDK。
- [核心机制](INTERNALS.md)：工具生命周期、确认门、循环检测、双层压缩与事件流。
- [贡献指南](CONTRIBUTING.md)：本地验证、PR、合并及分支清理。
- [SDK 边界](decisions/sdk-extraction.md)、[Skills 与 Application](decisions/skills-vs-application.md)、[两类工作流](decisions/workflow-engine.md)、[移除 LSP 工具](decisions/remove-lsp-tools.md)：仍适用的设计取舍。

## 历史调研

[research/](research/) 和 [references/](references/) 保存外部项目分析与技术资料，仅反映各文档记录时的版本；其中的路径、功能比较和计划不代表 EasyAgent 当前实现，也不作为操作指令。当前接入方式请查阅上面的使用文档。

过时的执行计划、重复能力快照和已失效路线图已移出当前目录树。需要追溯时使用 Git 历史，例如 `git log --all -- docs/archive`，不再为被删除文件创建空的归档跳转页。

## 维护规则

- 改动用户可见行为、配置、API 或部署流程时，同时更新对应文档与示例。
- 一项事实保留一个主要说明入口，其他文档链接引用，避免重复维护能力清单和路线图。
- 删除失效文档时修复入口与相对链接；有价值的历史研究明确标注时效。
- 示例不得包含真实密钥、已使用的配对码或个人运行凭据。
- 文档验证以源码、可运行示例和检查结果为依据，不把旧方案中的“计划完成”当成事实。
