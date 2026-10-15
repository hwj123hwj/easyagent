# EasyAgent

**Go 驱动的个人 AI 编程助手与 Agent 工作台，支持桌面、Web、TUI、飞书、MCP 和动态工作流。**

在对话中阅读与修改代码、执行工具、查看过程与结果，并保留可继续的历史会话。日常使用以桌面或浏览器工作区为中心，也可以从终端或已配对的飞书账号发起任务。底层 Agent 能力通过公共 Go SDK 复用。

## 能做什么

- **桌面客户端**：青夜界面、此 Mac 与远程主机连接配置、Slash 命令、可搜索模型、工具审批与展开、历史虚拟列表。连接凭据由主进程加密保存，见 [桌面说明](docs/DESKTOP.md)。
- **Web 工作区**：流式对话、Slash 命令提示、模型切换、工具结果展开与复制、历史会话，以及飞书和 MCP 设置。断线后恢复当前任务，发送确认前保留草稿。
- **MCP 工具**：官方 Go SDK 传输，支持 stdio、Streamable HTTP、兼容 SSE、用户/项目配置、逐工具过滤与 OAuth。工具进入同一 Agent 执行流程，见 [MCP](docs/MCP.md)。
- **终端 TUI**：独立全屏界面、Markdown 渲染、多行输入、工具分组折叠、鼠标选区复制。输入框位于快捷键和状态栏上方。
- **飞书对话**：长连接接收消息，支持文本与富文本，卡片随生成过程更新；首次配对后只接受已授权账号的操作。
- **动态工作流**：`/workflow` 将任务交给 Agent 编写 TypeScript 流程，支持多个 Actor、分支、循环、并行执行和受控恢复。复用固定版本的 ZCode 引擎。
- **YAML 流水线**：为预先确定的步骤声明依赖、并发、重试与人工确认门，与动态工作流分别管理。
- **可复用 SDK**：模型 Provider、工具、会话、上下文压缩、Skills、本地/SSH 执行和 AgentSession。

仓库也保留知识库与音乐应用；桌面客户端使用 Electron/React，共用 Go 服务、会话与命令接口。

## 从源码开始

需要 Go（版本以 [go.mod](go.mod) 为准，当前为 1.24.2）。动态工作流还需要 Node.js 22+。

```bash
git clone https://github.com/hwj123hwj/easyagent.git
cd easyagent
make build
cp .env.example .env
# 编辑 .env，填写自己的 Provider、模型、网关地址和密钥
./bin/easyagent
```

当前环境变量使用 `EA_*`，旧的 `PI_GO_*` 配置需要迁移。OpenAI 兼容服务的最小配置：

```dotenv
EA_PROVIDER=openai
EA_BASE_URL=http://localhost:4001
EA_MODEL=your-model-id
EA_API_KEY=your-api-key
```

`EA_API_KEY` 用于 OpenAI 兼容上游鉴权。可用 `EA_SERVER_API_KEY` 单独配置 EasyAgent HTTP API 的 Bearer 令牌；未设置时兼容使用 `EA_API_KEY`。其他 Provider、工具权限、文件路径和运行目录见 [配置说明](docs/CONFIG.md)。

启动网页：

```bash
./bin/easyagent serve --listen 127.0.0.1:8080
```

打开 `http://127.0.0.1:8080`，使用服务令牌登录。局域网部署、开机启动与自动更新见 [迷你主机部署](docs/MINI_DEPLOY.md)。桌面调试与 macOS 打包见 [桌面客户端](docs/DESKTOP.md)。

构建动态工作流组件：

```bash
(cd workflow-runtime && npm ci --ignore-scripts && npm run build)
export EA_WORKFLOW_RUNTIME="$PWD/workflow-runtime/output/workflow-runtime.mjs"
./bin/easyagent serve --listen 127.0.0.1:8080
```

随后在对话输入 `/workflow 分别检查代码质量、测试覆盖和文档，再汇总建议`。构建产物、执行限制与恢复语义见 [动态工作流](docs/DYNAMIC_WORKFLOW.md)。普通对话不要求 Node 组件。

## 终端使用

```bash
./bin/easyagent                       # TUI
./bin/easyagent chat                  # 交互式对话
./bin/easyagent run -p "分析这个项目"   # 单次任务
./bin/easyagent -y                    # 跳过交互确认，不改变文件路径限制
```

TUI 中输入 `/help` 查看命令。`Enter` 发送，`Ctrl+J` 换行，`PgUp/PgDn` 翻页；鼠标拖选后松开复制，`F2` 复制最近回复，`F3` 复制完整对话及工具结果，`Esc` 清除选区。点击工具组或命令标题展开详情，`Ctrl+O` 切换最近工具组。`Ctrl+T` 进入工具浏览，`↑↓` 选择组或命令，`Enter` 展开/折叠，`F2` 复制选中工具的完整参数和结果，`Esc` 返回输入。`Ctrl+P` 或 `/models` 打开模型选择器，可直接输入搜索，`Ctrl+U` 清空搜索。使用 `--session <ID>` 或 `/switch` 恢复会话时会显示当前分支完整历史。等待、输出文字和工具执行分别显示状态与耗时；输入草稿非空时，空闲 `Ctrl+C` 会保留草稿。macOS 使用 `pbcopy`，Linux 使用 `wl-copy`、`xclip` 或 `xsel`；复制失败会显示提示。

`Ctrl+R` 搜索当前会话的输入历史，`↑↓` 选择，`Enter` 回填完整输入供编辑，`Ctrl+Z` 撤销回填；不会自动发送。`Ctrl+G`、`/sessions` 或不带 ID 的 `/switch` 打开会话选择器，可按标题、ID、工作区搜索，查看消息数、最近活动和当前会话标记，`Enter` 恢复完整记录。搜索中 `Ctrl+U` 清空关键词，`Esc` 取消并保留草稿；运行任务时需要先取消才能切换会话。正文标题使用终端主题色，宽窗口中正文及表格最多占 96 列，缩小时自动换行。

## 文档

| 需要做什么 | 文档 |
|---|---|
| 配置模型、权限、路径与认证 | [配置](docs/CONFIG.md) |
| 连接 Mac / 迷你主机、构建桌面应用 | [桌面客户端](docs/DESKTOP.md) |
| 配置外部工具、项目授权与 OAuth | [MCP](docs/MCP.md) |
| 接入飞书、配对与排错 | [飞书](docs/FEISHU.md) |
| 编排多 Agent 动态任务 | [动态工作流](docs/DYNAMIC_WORKFLOW.md) |
| 运行固定步骤 DAG | [YAML 流水线](docs/WORKFLOW.md) |
| 开机启动、同步 main、健康检查与回滚 | [部署运维](docs/MINI_DEPLOY.md) |
| 打 tag、发布和核验下载 | [版本与发布](docs/RELEASING.md) · [更新记录](CHANGELOG.md) |
| 接入 HTTP / SSE / WebSocket | [API](docs/API.md) |
| 理解源码与 SDK 边界 | [架构](docs/ARCHITECTURE.md) |
| 开发、测试、合并和分支收尾 | [贡献指南](docs/CONTRIBUTING.md) |

完整导航见 [文档索引](docs/README.md)。产品与视觉约定分别在 [PRODUCT.md](PRODUCT.md) 和 [DESIGN.md](DESIGN.md)。

## 许可与开源组件

项目代码使用 [MIT License](LICENSE)。第三方代码的来源与许可保留在各自目录，动态工作流尤其参见 [组件来源说明](workflow-runtime/vendor/SOURCE.md)。修改和发布时须保留对应许可及 NOTICE。
