# 更新记录

版本规则与操作步骤见 [发布指南](docs/RELEASING.md)。正式版本以不可移动的 Git tag 为准；本文件记录面向使用者的变化，不代替提交历史。

## [Unreleased]

### 新增与改进

- Web 对话工作区、Slash 命令提示、会话查看、工具折叠与复制，以及托管飞书设置。
- TUI 独立屏幕、输入与鼠标事件处理、文本选区复制和工具分组展开。
- 飞书使用者首次配对、富文本消息解析与持续卡片更新。
- 基于固定版本 ZCode 引擎的动态 Actor 工作流，与原有 YAML 流水线分开管理。
- 迷你主机按 main 的确定 SHA 构建、验证、更新与回滚。
- 发布流程校验 tag、来源提交、版本说明和完整产物；通过完整 CI 后才公开 Release。

### 兼容与运行要求

- 项目与可执行文件改名为 EasyAgent；配置使用 `EA_*`，旧 `PI_GO_*` 需迁移。飞书桥接的 `PI_AGENT_URL` 保留。
- 动态工作流额外需要 Node.js 22+、同版 bundle 与第三方许可文件；普通 Go 对话不要求 Node。
- 工作区外文件访问使用独立显式开关，默认限制、API 认证与飞书使用者控制继续生效。

## 历史版本

现有 `v0.10.0` 至 `v0.11.0` 的 tag 与资产保持原样，说明见 [GitHub Releases](https://github.com/hwj123hwj/easyagent/releases)。旧 Release 使用 `pi-agent` 等资产名称；不补写或覆盖旧二进制。
