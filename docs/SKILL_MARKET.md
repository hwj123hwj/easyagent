# 技能市场

easyagent 内置技能市场客户端，对接 EasyCode 官方 skill store（`https://skills.deepvlab.ai`）。可以浏览/搜索技能、查看详情、下载安装到个人技能目录，也可以卸载之前通过市场安装的技能。**默认开启**，`EA_SKILL_MARKET=0` 可整体关闭。

## 使用方式

### 会话内（Agent 工具）

`skill_market` 工具在市场开启时自动注册，模型可自主调用：

| action | 参数 | 说明 |
|---|---|---|
| `search` | `query`、`category`、`sort`（`featured`/`installs`/`name`）、`page` | 浏览/搜索技能列表 |
| `detail` | `id`（必填） | 查看单个技能的完整元数据 |
| `install` | `id`（必填） | 下载并安装；**按会话权限模式确认** |
| `uninstall` | `name`（必填，目录名） | 卸载市场安装的技能；**按会话权限模式确认** |

安装成功后提示技能在**下一轮或新会话**生效（见下文"生效时机"）。

### REST API

服务端暴露以下端点（同样受 `EA_SKILL_MARKET` 开关与 API 认证约束）：

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/skills/market/search?q=&category=&sort=&page=&section=` | 搜索/浏览（代理官方 store） |
| `GET` | `/skills/market/skills/{id}` | 技能详情 |
| `GET` | `/skills/market/sections` | 主题分组 |
| `GET` | `/skills/market/installed` | 本地已安装的市场技能列表（含 manifest） |
| `POST` | `/skills/market/install` | 启动后台安装任务，body `{"id": <int>}`；返回 `{"job_id", "events_url"}`。同技能重复启动返回 409 |
| `GET` | `/skills/market/jobs/{id}` | 轮询任务状态（无 SSE 场景的回退） |
| `GET` | `/skills/market/jobs/{id}/events` | **SSE 进度流**：`event: progress` / `event: done`，订阅即重放当前快照或终态；15s keepalive 注释帧 |
| `POST` | `/skills/market/jobs/{id}/cancel` | 取消运行中的安装 |
| `DELETE` | `/skills/market/installed/{name}` | 卸载；非市场技能返回 409，未安装返回 404 |

## 安装与生效

- **安装目录**：`EA_SKILL_MARKET_SKILL_DIR`（默认 `~/.agents/skills`，与个人技能目录 `runtime.DefaultSkillDirs` 一致），布局为 `<目录>/<技能名>/SKILL.md`。
- **生效时机**：安装/卸载会触发 `ToolRevision` 变更（与 MCP 热重载同一机制）。已加载的会话在**下一轮 prompt 构建时**自动重建并重新加载技能；新会话立即生效。
- 安装的技能与手写技能、项目技能共存；同名冲突按现有技能优先级规则处理（项目 > 个人）。

## 桌面端

桌面端设置页新增「技能市场」标签：搜索/排序/主题分组浏览、一键安装（实时进度条：解析 → 下载（百分比/不确定动画）→ 校验 → 解压）、已安装徽标与二次确认卸载、分页。

客户端（`desktop/src/client/skill-market.ts`）桌面使用主进程带认证的 IPC 轮询，不把 token 暴露给页面；浏览器通过 fetch + ReadableStream 消费带认证的 SSE，中断时自动回退轮询 `GET /skills/market/jobs/{id}`。

## 安全模型

参考 EasyCode 客户端（`easycodeclient`）的技能市场实现，采用相同的安全原则：

1. **只信服务端元数据**：安装时名称、版本、sha256 全部来自 store 的 detail 响应；agent 或 REST 调用方传入的名称/摘要一律忽略。
2. **完整性校验**：下载后校验 SHA-256（store 未提供时跳过，但校验包大小）；不匹配立即失败且不落盘。
3. **zip-slip 防护**：解压拒绝 `../`、绝对路径等越界条目；文件权限保留 zip 内声明。
4. **不覆盖已有目录**：目标目录已存在（无论是手写技能还是其他内容）直接拒绝安装。
5. **卸载只删自己的**：每个市场安装都写入 `.easyagent-market.json` manifest；卸载时无 manifest 的目录一律拒绝（409），已有目录不会被安装覆盖或失败清理误删。
6. **下载上限**：下载包与展开后总量均最大 200 MiB，最多 10000 个条目；拒绝符号链接等特殊条目，临时目录完成验证后原子发布（macOS/Linux），防止异常响应拖垮内存。
7. **包名校验**：安装名必须匹配 `[a-z0-9][a-z0-9._-]*`，阻断路径注入。

## 配置

| 变量 | 默认 | 说明 |
|---|---|---|
| `EA_SKILL_MARKET` | `1` | 设 `0` 关闭（工具与 REST 端点同时消失） |
| `EA_SKILL_MARKET_BASE_URL` | `https://skills.deepvlab.ai` | store 地址；可指向自建兼容服务 |
| `EA_SKILL_MARKET_SKILL_DIR` | `~/.agents/skills` | 安装根目录 |

YAML 同名驼峰字段（`skill_market_base_url`、`skill_market_skill_dir`）亦可配置；启用开关目前仅支持环境变量。

## 边界说明

- 第一版只对接官方 store REST API；EasyCode 客户端里的 Git 市场模式（`/skill marketplace add <git-url>`）暂未实现。
- store 地址可配置意味着未来可以平滑切换到自建市场服务端，客户端代码无需改动。
- `EA_HOME` 与技能目录优先级见 [CONFIG](CONFIG.md)。
