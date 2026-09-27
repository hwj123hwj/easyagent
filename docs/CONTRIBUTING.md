# 开发指南

EasyAgent 的主要入口是 Go 服务内嵌 Web、终端 TUI 和飞书桥接。先阅读 [AGENTS.md](../AGENTS.md) 与 [架构](ARCHITECTURE.md)，当前使用说明从 [文档索引](README.md) 查找。

## 环境与目录

Go 版本以 [go.mod](../go.mod) 为准（当前 1.24.2），动态工作流使用 Node.js 22+。

| 目录 | 用途 |
|---|---|
| `cmd/` | 核心与飞书桥接入口 |
| `sdk/` | 可由其他 Go 模块导入的公共能力 |
| `internal/` | EasyAgent 应用、协议、TUI、Web 与领域实现 |
| `internal/web/static/` | 主 Web 界面的原生 HTML / CSS / JavaScript，嵌入 Go 程序 |
| `workflow-runtime/` | 动态工作流 Node 组件与固定版本的 ZCode 源码 |
| `third_party/bubbletea/` | TUI 依赖的本地模块及测试 |
| `desktop/` | 独立 Electron / React 客户端 |
| `deploy/`、`scripts/` | 服务模板、部署与验证脚本 |

构建 Go 程序使用 `make build`，产出 `bin/easyagent` 和 `bin/easyagent-bridge`。`make install` 仅安装核心 CLI，不安装桥接或工作流 bundle。工作流组件需单独构建；桌面客户端的脚本见其 `package.json`。

## 本地验证

从仓库根目录执行，与 [CI](../.github/workflows/verify.yml) 对齐：

```bash
(cd workflow-runtime && npm ci --ignore-scripts && npm run build && npm test)
go test ./...
go vet ./...
(cd third_party/bubbletea && go test ./...)
node --test scripts/web-test.mjs
python3 scripts/test_update_mini.py
git diff --check
```

单元测试通过依赖注入使用 mock；CLI 联调使用本地模拟的 OpenAI / Anthropic HTTP 接口，不调用真实模型。CLI 不支持 `EA_PROVIDER=mock`。只针对文档的改动可先验证链接、配置名和示例；合并仍需通过仓库要求的 CI。涉及流式传输、取消、工具权限、会话持久化时，应添加能覆盖失败路径的回归测试。

`sdk/` 不得 import `internal/`；领域能力留在应用层，公共接口通过 `sdk/arch_test.go` 验证。Go 代码用 gofmt，禁止把真实密钥、配对码、运行数据或本机构建产物写入提交。

## 提交、合并与分支收尾

1. 在真实项目副本检查工作区和远端状态，保留已有改动。长期分支只有 `main`。
2. 当前任务可使用短期 `feat/`、`fix/` 或 `docs/` 分支。提交标题描述实际改变，例如 `docs: refresh configuration and deployment guides`。
3. PR 说明问题、最终行为、验证和必要限制；修改配置或 API 时同步更新文档。
4. CI 通过后合并。回到并同步 `main`，确认提交已包含在 main 中，再删除对应本地与远端任务分支，运行 `git fetch --prune origin`。
5. 收尾检查本地与远端分支。未合并提交、未提交改动或仍在使用的 worktree 必须保留并说明，不能强制删除来制造干净状态。

## 发布与数据

迷你主机更新器跟踪 `main`，使用确定 SHA 独立构建并验证核心、桥接和工作流 bundle；健康检查失败回滚。它不覆盖运行配置和会话目录。更新会重启服务，长任务期间需暂停更新 timer，详见 [部署运维](MINI_DEPLOY.md)。

飞书配对、HTTP 认证、路径开关和工具确认分别验证。涉及这些功能的改动必须保持默认限制与授权检查；不得用关闭认证或跳过 owner 校验来修复接入问题。
