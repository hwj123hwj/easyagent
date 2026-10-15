# 飞书接入

飞书桥接通过 WebSocket 长连接收取消息，再调用 EasyAgent 核心的 HTTP / SSE 接口，卡片随生成过程更新。支持本机 serve 进程内桥接，以及独立的 `easyagent-bridge` 托管进程。核心服务运行、桥接运行和飞书已连接需要分别核验。

## 此 Mac / 非托管主机

连接此 Mac 后，在桌面「设置 → 飞书机器人」扫码绑定，并确认保存凭据，再点击启动/重启桥接。轮询扫码状态不会自动保存；服务接口 `POST /settings/feishu/bridge/restart` 使用已保存的本机凭据启动桥接，不需要另开终端。

`GET /settings/feishu` 的 `local_mode:true`、`bridge.running` 和可选 `bridge.error` 用于查看进程内桥接状态。管理接口仍要求显式服务令牌，桌面托管核心自动提供。桥接随 serve 退出，重新启动核心后需再次启动桥接；需要独立常驻时使用下方 systemd 部署。托管主机使用自建应用凭据与配对，不走扫码注册。

## 网页配置（托管部署）

按 [迷你主机部署](MINI_DEPLOY.md) 配好 core / bridge 两个 systemd 用户服务。核心配置需要：

```dotenv
EA_SERVER_API_KEY=your-agent-server-key
EA_FEISHU_ENV_FILE=/home/q/.config/easyagent/feishu.env
FEISHU_OWNER_STATE_FILE=/home/q/.config/easyagent/feishu-owner.json
```

将示例中的用户名与路径换成实际值。`EA_FEISHU_ENV_FILE` 必须对应桥接服务的 EnvironmentFile；桥接使用相同的 owner 状态路径与核心 API 令牌。

在 Web「设置」填写 App ID / App Secret，点击“保存并重启桥接”。Secret 不回显，留空保留现有密钥；首次配置后 App ID 固定。更换机器人需要重新配置使用者绑定，不同应用的 open_id 不能直接复用。

管理 API 即使在本机也要求显式 Bearer 认证。`managed:false` 表示没有 systemd 托管，不代表本机桥接不可用；已有 owner 绑定不会被自动修改。

## 手动运行

`make build` 构建核心与桥接。先启动核心，再为桥接设置：

```dotenv
FEISHU_APP_ID=cli_your-app-id
FEISHU_APP_SECRET=your-app-secret
PI_AGENT_URL=http://127.0.0.1:8080
EA_SERVER_API_KEY=your-agent-server-key
# 可选：显式配置已授权账号，否则使用首次配对。
# FEISHU_OWNER_OPEN_ID=ou_your-open-id
# FEISHU_OWNER_STATE_FILE=/absolute/path/feishu-owner.json
```

将这些值放入专用文件后，用 `EA_ENV_FILE=/absolute/path/feishu.env ./bin/easyagent-bridge` 启动。也可通过交互入口 `/feishu setup` 配置凭据。飞书应用侧需启用机器人能力、相应消息权限及长连接事件订阅，并发布可用版本；配置信息以实际应用控制台为准。

### 终端扫码配置

在 TUI 输入 `/feishu setup` 后，程序暂时让出终端，先选择授权方式：

- `1` 或直接回车：显示黑白二维码，用手机飞书扫码；不会打开浏览器。
- `2`：明确选择在当前主机打开浏览器。打开失败或超时后仍可复制链接到其他设备授权。
- `3`：仅显示链接，适合 SSH、无桌面服务器或终端宽度不足以展示二维码的环境。
- `0` 或 Ctrl+C：取消配置并返回会话，不覆盖已有凭据。

也可直接输入 `/feishu setup --qr`、`/feishu setup --link` 或 `/feishu setup --browser`。授权链接始终可以复制到另一台设备；仅 `--browser` 会调用本机浏览器。初始化、等待扫码和凭据验证期间均可按 Ctrl+C 取消，取消后可重新运行配置或继续聊天。此引导不需要安装终端图片插件；二维码使用固定黑白配色的文本块绘制，须保持每行完整，不折行。

## 首次配对

未预置 owner 时，在网页设置按需查看私聊配对指令，**只私聊目标机器人**发送。配对码单次有效且有截止时间，不要把旧截图或文档中的码作为当前凭据。配对前不执行开发指令；完成后只接受绑定账号的操作，配对状态独立持久化。

如果已经配对，刷新设置页确认状态。不要为解决回复问题删除配对文件或关闭身份校验。

## 排错

| 现象 | 检查方向 |
|---|---|
| bridge active 但机器人无响应 | 检查飞书 WebSocket 连接日志、应用发布状态、消息事件与权限 |
| 提示未授权 / 无法配对 | 是否私聊目标应用、是否为当前有效配对码、core 与 bridge 是否使用同一 owner 文件 |
| 配对成功但对话请求失败 | `PI_AGENT_URL` 是否可达、服务令牌（`EA_SERVER_API_KEY`，兼容 `EA_API_KEY`）是否与核心一致、模型服务是否正常 |
| 卡片只有状态或文字很慢 | 分别检查核心 SSE 首个文本事件、工具耗时和飞书卡片更新；不能只按总耗时判断模型卡顿 |
| 富文本消息无法解析 | 保留消息结构和错误日志用于回归测试；避免在日志中泄漏凭据或私聊内容 |

文本与富文本由桥接解析；无法支持的消息应返回明确说明，不将解析失败占位文本当成用户任务。流式卡片首次内容尽早发送，后续合并短时间内更新，结束时写入最终状态。卡片显示“已完成”不应替代真实回答。

普通入站消息按 App ID 与飞书消息 ID 生成稳定 `request_id`，重复投递和桥接重启后仍复用核心受理凭据，避免重复执行工具；该保证依赖核心的数据目录与请求记录保留，不是跨多个核心进程的互斥。系统生成的后续任务没有入站消息身份，会创建新请求。模型切换可直接使用 `/models` 中的完整 ID（含斜杠）；不存在的模型会立即报错，失败卡片显示错误摘要。

```bash
systemctl --user status easyagent-core.service easyagent-bridge.service
journalctl --user -u easyagent-bridge.service -n 80 --no-pager
curl -fsS http://127.0.0.1:8080/health
```

自启动需 enable 两个用户服务，并为运行账号启用 linger；具体安装与更新步骤见 [部署运维](MINI_DEPLOY.md)。
