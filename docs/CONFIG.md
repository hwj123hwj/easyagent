# 配置

环境变量示例见 [`.env.example`](../.env.example)，配置实现见 [`sdk/config/config.go`](../sdk/config/config.go)。当前只读取 `EA_*` 名称，不再回退到 `PI_GO_*`；飞书桥接地址 `PI_AGENT_URL` 是仍在使用的独立变量。

## 加载顺序

核心入口先查找当前目录 `easyagent.yaml`，否则查找 `~/.easyagent/config.yaml`，再应用环境变量，最后应用已解析的命令行运行选项。

环境加载会补充 `.env` → `.env.local` → `~/.easyagent/.env` → `~/.easyagent/.env.local`，已有的非空环境值不会被覆盖。因此 `.env.local` **不是覆盖层**。设置 `EA_ENV_FILE` 时仅加载指定文件及其 `.local` 文件。`EA_HOME` 可改变默认用户配置目录。

桥接的 dotenv 加载与核心不同：指定 `EA_ENV_FILE` 时只加载该文件，否则加载当前目录和用户配置目录中的 `.env`；另可读取 `/feishu setup` 保存的凭据。systemd 部署建议使用显式 EnvironmentFile，见 [部署文档](MINI_DEPLOY.md)。

## 模型与认证

OpenAI 兼容服务：

```dotenv
EA_PROVIDER=openai
EA_BASE_URL=http://localhost:4001
EA_MODEL=your-model-id
EA_API_KEY=your-api-key
```

应显式填写自己的模型与地址。`EA_MODEL`、`EA_BASE_URL`、`EA_API_KEY` 分别优先于 `OPENAI_MODEL`、`OPENAI_BASE_URL`、`OPENAI_API_KEY`。

Anthropic 使用 `EA_PROVIDER=anthropic` 和 `ANTHROPIC_API_KEY`、`ANTHROPIC_MODEL`、`ANTHROPIC_BASE_URL`。CLI 当前支持 `openai` / `anthropic`；开发测试注入 mock 或使用本地模拟接口，`EA_PROVIDER=mock` 不是有效的 CLI 配置。Provider 默认为空，正常使用需配置。

`EA_SERVER_API_KEY` 单独设置 HTTP API Bearer 令牌，不改变上游模型密钥。未设置时兼容使用 `EA_API_KEY` 作为服务令牌。使用 Anthropic 时，上游密钥来自 `ANTHROPIC_API_KEY`。

桌面客户端启动自己的本地服务时生成独立的随机 `EA_SERVER_API_KEY`，保留用户模型配置；远程连接使用目标服务的令牌。

| 服务配置 | 默认 / 行为 |
|---|---|
| `EA_HOST` / `EA_PORT` | `127.0.0.1` / `8080`；`serve --listen` 可指定监听地址 |
| `EA_SERVER_API_KEY` | HTTP API 独立令牌，优先于 `EA_API_KEY`；`/health` 公开 |
| `EA_API_KEY` | OpenAI 兼容上游密钥，兼容作为未单独配置时的服务令牌 |
| 未设置 API Key | 默认仅接受 loopback 来源的 API 请求 |
| `EA_ALLOW_NO_AUTH=1` | 显式放开普通 API；不能用于绕过飞书设置管理接口的认证 |
| `EA_ALLOWED_ORIGINS` | 逗号分隔的允许来源，按部署需求配置 |

Web 页面可加载不代表 API 已获授权。远程使用需要登录；WebSocket 的认证细节见 [API](API.md)。

## 工具和文件路径

| 变量 | 默认值 | 作用 |
|---|---|---|
| `EA_WORKSPACE` | 当前工作目录 | 会话工作目录 |
| `EA_DATA_DIR` | `./data` | 会话与工作流运行数据 |
| `EA_HOME` | `~/.easyagent` | 用户配置与凭据目录 |
| `EA_ENABLE_BASH` | `false` | 启用命令执行工具 |
| `EA_AUTO_APPROVE` | `false` | 跳过交互工具确认，对应 `-y` |
| `EA_ALLOW_OUTSIDE_WORKSPACE` | `false` | 允许文件工具访问工作区外路径 |
| `EA_ENABLE_WEB` | `false` | 启用网页抓取工具 |
| `EA_ENABLE_WEB_SEARCH` | `false` | 启用搜索工具 |
| `EA_WEB_TIMEOUT_SECONDS` | `30` | 网页请求超时 |
| `EA_MAX_OUTPUT_LEN` | `30000` | 工具输出长度上限 |
| `EA_ALLOWED_TOOLS` / `EA_BLOCKED_TOOLS` | 空 | 逗号分隔的工具过滤名单 |
| `EA_PROMPT_TEMPLATE` | 空 | 自定义系统提示模板 |

`easyagent serve --allow-outside-workspace` 与环境开关等效。默认检查绝对路径、相对路径及符号链接，防止文件工具逃出工作区；放开后仍受运行用户的系统权限限制。此开关与 `-y`、API 认证独立。HTTP 工作区文件接口仍有自己的路径检查，不能把文件工具开关理解为所有 API 的任意路径访问许可。

Bash 并非文件路径沙箱；启用它就授予相应系统命令能力。serve 模式的 Web 与桌面客户端提供工具审批，等待批准会在两分钟后自动拒绝。SDK 调用者须提供自己的确认回调；不能依赖 TUI 的提示保护服务。`-y` / `EA_AUTO_APPROVE=true` 会显式跳过审批，包括未信任的 MCP 工具。

## 可选能力

| 配置 | 用途 |
|---|---|
| `EA_EXECUTION_MODE=ssh`、`EA_SSH_HOST`、`EA_SSH_PORT`、`EA_SSH_WORKDIR` | SSH 执行后端；端口默认 22 |
| `EA_WORKFLOW_RUNTIME` | 动态工作流 bundle 绝对路径，见 [构建说明](DYNAMIC_WORKFLOW.md) |
| `EA_MCP_CONFIG` / `--mcp-config` / YAML `mcp_config` | 用户 MCP 配置文件，默认 `EA_HOME/mcp.json`；项目文件与授权见 [MCP](MCP.md) |
| `EA_KB_REPO_PATH` | 知识库目录；未设置时运行入口使用 `~/agent-lessons` |
| `SILICONFLOW_API_KEY`、`SILICONFLOW_EMBEDDING_MODEL`、`SILICONFLOW_BASE_URL` | 可选知识库向量搜索 |
| `ASR_API_KEY`、`ASR_MODEL`、`ASR_BASE_URL` | 语音识别；未设置 ASR Key 时可回退到 SiliconFlow Key |
| `EA_FEISHU_ENV_FILE`、`FEISHU_OWNER_STATE_FILE` | 托管飞书配置与配对状态，见 [飞书](FEISHU.md) |

不要将运行 `.env`、访问令牌或配对状态提交到 Git。部署更新保留这些独立文件。

### 语音识别的网关回退

`ASR_API_KEY` 与 `ASR_BASE_URL` 可显式配置专用语音服务；文件中的专用 ASR 凭据、地址与模型会保留。没有专用配置时，`SILICONFLOW_API_KEY` 使用默认 SiliconFlow 地址；否则复用 OpenAI 兼容网关的密钥和地址。显式设置其他 `ASR_BASE_URL` 时必须同时提供语音密钥，不会将网关或 SiliconFlow 凭据转发到该地址。地址可包含 `/v1`，网关需支持 `audio/transcriptions` 与 `ASR_MODEL`（默认 `TeleAI/TeleSpeechASR`）。

本机飞书设置支持扫码授权并确认保存应用凭据；授权凭据存于该主机的 EasyAgent 配置目录，不回显密钥。保存不会自动启动桥接，需另行启动或重启 `easyagent-bridge`。托管 systemd 桥接继续使用已有自建应用和使用者配对设置。扫码使用飞书注册接口，服务不可用时可使用自建应用。

## 实验性电脑控制

`EA_ENABLE_COMPUTER_USE`（默认 `false`）与 `EA_COMPUTER_APPROVAL_POLICY`（`ask` 默认，或 `auto`）控制新建/重新加载的编码会话。桌面设置保存在数据目录的 `settings.json`，启动时覆盖环境与 YAML 中的对应项；已加载会话保持配置快照。

v0.9.0 只提供可选 `easyagent-cua-helper` 的 CLI 协议，安装包没有原生 helper。仅 macOS 主机安装兼容 helper 后可执行，权限检测不可用时界面明确提示。截图结果当前只是本地路径。审批策略独立于编码会话的完全权限；`ask` 批次及 `auto` 中退出/注销/锁屏/强制退出组合键必须经交互批准。审计列表表示实际控制过的应用，不是允许列表，也不限制目标应用。原生实现与基于应用/元素的控制需要后续完善。

兼容 helper 的 CLI 契约：`status` 返回 `accessibility` / `screen_recording` 布尔值；`request-permissions` 打开权限面板；`tool screenshot --args '{"output_dir":"…"}'` 返回 `path`；`tool app_state` 返回 JSON 应用状态；`tool perform_action --args '{"actions":[…]}'` 仅整批完成时返回 `{"success":true,"focused_app":"应用 ID"}`，失败返回非零退出码或 `{"success":false,"error":"原因"}`。批次按顺序执行，不保证回滚，失败不可盲目重放。所有输出走 stdout，诊断走 stderr。
