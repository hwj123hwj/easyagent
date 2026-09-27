# EasyAgent 贡献指南

> 本文档帮助新开发者快速了解项目结构、开发流程和规范，使你能高效参与协作。

---

## 一、项目简介

EasyAgent 是一个 AI Coding Agent，由 **Go 后端** + **Electron/React 桌面前端** 组成。

- **Go 后端**（`cmd/easyagent`）：Agent 循环、LLM 调用、工具执行、会话管理、HTTP/WebSocket API
- **飞书桥接**（`cmd/easyagent-bridge`）：将 Agent 接入飞书群聊的独立服务
- **桌面客户端**（`desktop/`）：Electron 壳 + React UI，内嵌 Go 二进制，开箱即用

项目目前处于 **v0.1 阶段**——核心功能可用，正在持续迭代产品和体验。

---

## 二、环境准备

### 必需

| 工具 | 版本要求 | 用途 |
|------|----------|------|
| Go | 1.22+ | 后端开发 |
| Node.js | 20+ | 前端开发 |
| npm | 随 Node.js | 前端依赖管理 |
| Git | 任意 | 版本控制 |
| macOS | Apple Silicon 优先 | 桌面客户端打包（Linux/Windows 暂不支持） |

### 可选

| 工具 | 用途 |
|------|------|
| Xcode Command Line Tools | `xcode-select --install`，编译 C 依赖时需要 |
| Go IDE 插件 | VS Code Go / GoLand |

---

## 三、项目结构

```
EasyAgent/
├── cmd/
│   ├── easyagent/           # Agent CLI 入口
│   └── easyagent-bridge/   # 飞书桥接入口
├── internal/                # Go 内部包（不对外暴露）
│   ├── agent/              #   Agent 循环（双层：外层 follow-up + 内层 tool call + goal-driven）
│   ├── agents/             #   Agent 应用层
│   │   └── coding/         #     Coding Agent（CLI、命令、SessionExt、Profile）
│   ├── ai/                 #   统一 LLM 流式 API
│   │   └── providers/      #     Provider 实现（anthropic / openai / deepv / mock）
│   ├── app/                #   应用组装（依赖注入、Provider 注册）
│   ├── compaction/         #   上下文压缩（长对话摘要）
│   ├── config/             #   配置管理（.env + 环境变量）
│   ├── extensions/         #   扩展系统
│   ├── feishu/             #   飞书集成（gateway、handler、markdown 渲染）
│   ├── mode/               #   运行模式（run / chat / serve）
│   ├── operations/         #   工具操作抽象（本地/SSH 远程执行）
│   ├── prompt/             #   系统提示构建
│   ├── runtime/            #   会话运行时（AgentSession、Application 接口）
│   ├── server/             #   HTTP + WebSocket 服务
│   ├── session/            #   JSONL 会话持久化
│   ├── sessionmgr/         #   会话管理器
│   ├── skill/              #   技能系统（.claude/skills/）
│   ├── slashcmd/           #   斜杠命令注册
│   ├── tools/              #   内置工具（bash/read/write/edit/grep/find/ls）
│   ├── ui/                 #   UI 抽象层
│   └── util/               #   工具函数（git、shell）
├── desktop/                 # Electron + React 桌面客户端
│   ├── electron/           #   Electron 主进程（窗口管理、Go 进程管理）
│   ├── src/                #   React 前端
│   │   ├── components/     #     UI 组件
│   │   ├── stores/         #     Zustand 状态管理
│   │   ├── services/       #     API 和 WebSocket 客户端
│   │   └── styles/         #     CSS Modules + CSS Variables（青夜主题）
│   ├── electron-builder.yml      # electron-builder 打包配置
│   ├── vite.config.ts            # Vite 构建配置
│   └── tsconfig.electron.json    # Electron 进程 TypeScript 配置
├── scripts/
│   └── build-desktop.sh    # 一键构建 macOS DMG
├── docs/                    # 项目文档
├── .env.example             # 环境变量模板
└── go.mod                   # Go 模块定义
```

### 核心依赖关系

```
cmd/easyagent
  └── internal/app          ← 组装层，连接所有组件
        ├── internal/ai     ← LLM Provider 抽象
        ├── internal/agent  ← Agent 循环引擎
        ├── internal/tools  ← 工具实现
        ├── internal/server ← HTTP/WS API
        └── internal/config ← 配置
```

---

## 四、开发流程

### 4.1 Fork & Clone

```bash
# Fork 后 clone 你自己的仓库
git clone https://github.com/<your-username>/easyagent.git
cd easyagent
```

### 4.2 分支规范

| 分支 | 用途 | 命名示例 |
|------|------|----------|
| `main` | 稳定版本 | — |
| `feat/xxx` | 新功能 | `feat/goal-driven-loop` |
| `fix/xxx` | Bug 修复 | `fix/websocket-reconnect` |
| `refactor/xxx` | 重构 | `refactor/agent-loop` |
| `docs/xxx` | 文档 | `docs/api-reference` |
| `chore/xxx` | 构建/工具 | `chore/update-deps` |

**最佳实践**：一个分支只做一件事。完成 → PR → 合并 → 拉新分支。避免在同一个分支上堆积不相关的功能。

```bash
# 正确流程：从最新 main 创建功能分支
git checkout main
git pull origin main
git checkout -b feat/your-feature

# 开发完成后推送到远程并发 PR
git push -u origin feat/your-feature
gh pr create --title "feat(scope): what changed" --base main

# PR 合并后，拉取新 main 再开下一个分支
git checkout main
git pull origin main
git checkout -b feat/next-feature
```

### 4.3 后端开发（Go）

```bash
# 编译
go build -o easyagent ./cmd/easyagent

# 运行测试
go test ./...

# 运行单个包的测试
go test ./sdk/tools/ -v

# 开发模式：交互式聊天
./easyagent -mode chat

# 开发模式：HTTP 服务（配合前端开发）
./easyagent -mode serve -listen 127.0.0.1:8080
```

**配置**：复制 `.env.example` 为 `.env`，根据需要修改：

```bash
cp .env.example .env
```

可用的 Provider：
- `mock` — 不调用真实 LLM，用于本地开发测试
- `anthropic` — Anthropic Messages API（或兼容端点）
- `openai` — OpenAI Chat Completions API（或兼容端点）
- `deepv` — DeepV Code Server（公司内部）

文件工具默认限制在 workspace 内。显式设置 `EA_ALLOW_OUTSIDE_WORKSPACE=true`、YAML `allow_outside_workspace: true` 或 CLI `--allow-outside-workspace` 可允许工作区外绝对/相对路径（含符号链接）；相对路径仍基于原 workspace，cwd 不变。该设置适用于 `serve`，不改变 OS 权限。

`-y` / `EA_AUTO_APPROVE` 只控制交互确认，与路径策略严格独立；`serve` 原有无交互确认行为也不解除路径限制。Bash 另由 `EA_ENABLE_BASH` 控制，路径限制不是 shell/OS 沙箱。放开路径意味着 Agent 可读写运行用户有权限的文件（包括凭据），仅在可信用户、API 认证和入站 owner 门禁下启用。`read_many_files` 的旧 `allowLocalExecution` 参数不再授予越界权限。

飞书桥接在接收消息、媒体、文本选择和卡片操作前检查 owner。可用 `FEISHU_OWNER_OPEN_ID`（或已保存注册凭据中的用户）明确指定可信 owner；否则启动时在 `FEISHU_OWNER_STATE_FILE`（默认 `~/.easyagent/feishu-owner.json`）生成 0600 私有配对状态。运维者仅向目标用户私下交付其中的 code，用户私聊机器人精确发送 `/pair <code>`。24 小时有效、首个持码者单次绑定，原子保存 open_id 并删除 code；过期后重启桥接可重新发码。未配对时普通消息和卡片不执行，不主动发启动欢迎。bridge 使用 `PI_AGENT_URL` 和 `EA_API_KEY` 连接核心；可选 HTTP 工具回调也必须携带认证并具备该会话的 owner 上下文。

### 4.4 前端开发（Electron + React）

```bash
cd desktop

# 安装依赖
npm install

# 纯前端开发（浏览器模式，需要后端单独运行）
npm run dev

# Electron 开发模式（自动拉起 Go 后端）
npm run electron:dev
```

前端开发时，Go 后端需要单独运行：

```bash
# 另一个终端
EA_ENABLE_BASH=true ./easyagent -mode serve -listen 127.0.0.1:8080
```

### 4.5 构建桌面安装包

```bash
# 一键构建（推荐）
./scripts/build-desktop.sh        # arm64（默认）
./scripts/build-desktop.sh --x64  # x64

# 或手动分步
go build -o easyagent ./cmd/easyagent
cd desktop
npm run electron:build:arm64

# 产物在 desktop/release/
```

---

## 五、代码规范

### 5.1 Go 代码

| 规范 | 说明 |
|------|------|
| 包组织 | 全部放在 `internal/` 下，不暴露公共 API |
| 错误处理 | 显式 `if err != nil`，不用 panic |
| 日志 | 使用 `log/slog`，不用 `fmt.Println` |
| 命名 | Go 标准驼峰：`getGitInfo`、`DeepVProvider` |
| 注释 | 导出函数/类型必须有 godoc 注释 |
| 测试 | 新增功能需附带 `_test.go` |

### 5.2 TypeScript / React 代码

| 规范 | 说明 |
|------|------|
| 状态管理 | Zustand store，放在 `src/stores/` |
| 样式 | CSS Modules + CSS Variables（主题变量在 `styles/variables.css`） |
| 组件 | 函数组件 + hooks，放在 `src/components/` |
| 服务层 | API 调用封装在 `src/services/`，不直接在组件里 fetch |
| 类型 | TypeScript 严格模式，避免 `any` |

### 5.3 样式规范

项目使用 **青夜（Qingye）** 主题，所有颜色通过 CSS 变量引用：

```css
/* 正确 — 使用 CSS 变量 */
background: var(--bg-primary);
color: var(--text-primary);
border: 1px solid var(--border-primary);

/* 错误 — 硬编码颜色值 */
background: #3B4A54;
```

完整变量定义见 `desktop/src/styles/variables.css`。

---

## 六、Git 规范

### 6.1 Commit Message

使用 Conventional Commits 格式：

```
<type>(<scope>): <subject>

<body>
```

**type**：

| 类型 | 用途 |
|------|------|
| `feat` | 新功能 |
| `fix` | Bug 修复 |
| `refactor` | 重构（不改行为） |
| `style` | 样式调整 |
| `docs` | 文档 |
| `test` | 测试 |
| `chore` | 构建/工具/依赖 |

**scope** 可选，常用值：

| scope | 说明 |
|-------|------|
| `agent` | Agent 循环（loop、goal evaluator、tool 执行） |
| `ai` | LLM Provider 抽象层 |
| `config` | 配置管理 |
| `deepv` | DeepV Provider |
| `desktop` | 桌面客户端 |
| `feishu` | 飞书桥接 |
| `runtime` | 会话运行时（AgentSession、Application 接口） |
| `server` | HTTP/WebSocket 服务 |
| `session` | 会话持久化 |
| `slashcmd` | 斜杠命令 |
| `tools` | 内置工具 |
| `compaction` | 上下文压缩 |

**示例**：

```
feat(tools): add image reading support to read tool
feat(agent): implement goal-driven loop with LLM evaluator
fix(deepv): auto-fake gitlab remote when work dir has no git remote
refactor(runtime): extract SessionExt to decouple from coding-agent
feat(feishu): add markdown renderer for agent responses
style(desktop): apply 青夜 theme from OpenHanako
docs: add contributing guide
chore: update Go dependencies
```

### 6.2 Pull Request

1. 从功能分支向 `main` 发 PR
2. PR 标题遵循 commit message 格式
3. PR 描述包含：
   - **What**：改了什么
   - **Why**：为什么改
   - **How**：怎么改的（关键实现细节）
   - **Test**：怎么验证的
4. 至少一人 Review 后合并

---

## 七、架构决策记录

重要设计决策记录在 `docs/` 下：

| 文档 | 说明 |
|------|------|
| `docs/archive/desktop-golang/changes.md` | 桌面客户端涉及的 Go 后端改动详解 |
| `docs/dev/coding-agent/spec.md` | Coding Agent 功能规格 |


新增重大架构决策时，请在 `docs/` 下新增或更新对应文档。

---

## 八、环境变量速查

### Go 后端

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `EA_PROVIDER` | `mock` | LLM Provider：`mock` / `anthropic` / `openai` / `deepv` |
| `ANTHROPIC_API_KEY` | — | Anthropic API Key |
| `ANTHROPIC_MODEL` | — | Anthropic 模型名 |
| `ANTHROPIC_BASE_URL` | `https://api.anthropic.com` | Anthropic API 地址 |
| `OPENAI_API_KEY` | — | OpenAI API Key |
| `OPENAI_MODEL` | — | OpenAI 模型名 |
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` | OpenAI API 地址 |
| `DEEPV_ENABLED` | `false` | 启用 DeepV Provider |
| `DEEPV_SERVER_URL` | — | DeepV 服务器地址 |
| `DEEPV_MODEL` | — | DeepV 模型名 |
| `DEEPV_WORK_DIR` | 当前目录 | DeepV 工作目录（用于获取 Git 信息） |
| `DEEPV_GIT_REMOTE` | `https://gitlab.liebaopay.com/fake/EasyAgent-workspace.git` | 无 remote 时伪造的 Git 地址 |
| `EA_HOST` | `127.0.0.1` | HTTP 监听地址 |
| `EA_PORT` | `8080` | HTTP 监听端口 |
| `EA_SESSION_FILE` | `./data/session.jsonl` | 会话文件路径 |
| `EA_DATA_DIR` | — | 数据目录（Electron 打包模式使用） |
| `EA_ENV_FILE` | `.env` | .env 文件路径 |
| `EA_ENABLE_BASH` | `false` | 启用 Bash 工具 |
| `EA_BASH_TIMEOUT_SECONDS` | `30` | Bash 命令超时 |
| `EA_MAX_OUTPUT_LEN` | `30000` | 工具输出最大字符数 |
| `EA_WORKSPACE` | 当前目录 | 工作目录（工具文件操作的根目录） |
| `EA_EXECUTION_MODE` | `local` | 执行后端：`local` 或 `ssh` |
| `EA_SSH_HOST` | — | SSH 模式目标主机（`user@host`） |
| `EA_SSH_PORT` | `22` | SSH 端口 |
| `EA_SSH_WORKDIR` | — | SSH 模式远程工作目录 |
| `EA_ALLOWED_TOOLS` | — | 工具白名单（逗号分隔，为空表示允许所有） |
| `EA_BLOCKED_TOOLS` | — | 工具黑名单（逗号分隔） |
| `EA_HISTORY_FILE` | — | 交互模式历史记录文件路径 |
| `EA_PROMPT_TEMPLATE` | — | 自定义提示模板文件路径 |

---

## 九、常见任务

### 新增 LLM Provider

1. 在 `sdk/ai/providers/` 下新建 `<provider>.go`
2. 实现 `ai.Provider` 接口（`Stream()` 方法）
3. 在 `internal/app/app.go` 的 `registerProvider()` 中注册
4. 在 `sdk/config/config.go` 中添加对应的环境变量

### 新增工具

1. 在 `sdk/tools/` 下新建 `<tool>.go`
2. 实现 `tools.Tool` 接口（`Name()`、`Description()`、`Parameters()`、`Execute()`）
3. 在 `sdk/tools/` 的注册函数中添加新工具
4. 添加 `_test.go` 测试文件

### 新增前端页面/组件

1. 在 `desktop/src/components/` 下新建组件目录
2. 组件 + 同名 `.module.css`
3. 如需全局状态，在 `desktop/src/stores/` 下新建 Zustand store
4. 如需 API 调用，在 `desktop/src/services/api.ts` 中添加

---

## 十、问题反馈

- **Bug**：提 GitHub Issue，附上复现步骤和日志
- **功能建议**：提 GitHub Issue，描述场景和期望行为
- **疑问**：在 Issue 或内部群中讨论
