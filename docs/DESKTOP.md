# 桌面客户端

Electron / React 客户端使用同一 Go 会话服务。个人任务以对话为中心，保留青夜颜色、固定输入区、可展开的工具过程和独立工作区入口。

## 连接与使用

「设置 → 连接与运行」提供「此 Mac」和「迷你主机」配置。选择此 Mac 时，应用启动自己的 loopback 服务与随机 `EA_SERVER_API_KEY`；用户的 Provider / 模型网关密钥继续从 EasyAgent 配置读取。选择远程主机时填写 `http(s)://主机:端口` 和服务令牌，空令牌保留已保存的值。

桌面托管的本地核心使用应用用户目录下独立的 `core-data`，不继承 `EA_DATA_DIR`，避免与已运行的 CLI / Web 核心共用会话及任务凭据。若希望查看已有 8080 服务的历史，应添加连接到该服务的配置；连接远程主机同样共享该主机的历史。

正式桌面应用通过主进程代理 HTTP / WebSocket；远程令牌使用 Electron `safeStorage` 保存，不传给渲染器。不具备安全存储时拒绝保存秘密。连接状态、认证失败、启动错误与重试入口直接显示在界面中。切换主机不会取消旧主机上的任务。

- 输入 `/` 查看共享命令目录，方向键选择，Tab / Enter 补全，Escape 关闭。`/workflow <任务>` 进入已有动态工作流引擎。
- Enter 发送，Shift Enter 换行；服务确认接受前保留草稿。发送结果不确定时重试同一请求标识，避免重复任务。
- 首段输出立即显示，后续增量按渲染帧合并；无逐字播放延迟。工具分组折叠，参数与结果可展开、复制；错误单独说明。
- 向上阅读时保持位置，「回到最新」恢复跟随。长历史使用虚拟列表；模型选择支持搜索。
- 需要批准的工具在输入区附近显示「允许本次」和「拒绝」。断线后恢复同一任务及待审批操作。
- `/mcp` 或设置中的「MCP 工具」管理当前服务主机的能力，详见 [MCP](MCP.md)。

任务归服务端会话所有。关闭窗口或断网仅停止订阅，不自动取消远程任务；停止按钮针对当前 run。服务进程重启后任务标为中断，旧请求不会自动再执行。已有输出与副作用应先检查，再发起新的任务。该恢复机制不能保证硬断电下的外部副作用恰好执行一次。

## 开发预览

需 Node.js 22+、Go 和已配置的模型。只预览界面：

```bash
cd desktop
npm ci --ignore-scripts
npm run dev
```

浏览器预览在设置中连接自己的 Go 服务；跨域时需设置 `EA_ALLOWED_ORIGINS`。预览模式不具备 Electron 安全存储、进程管理或系统 OAuth 回调能力，不应作为正式远程入口。

完整 Electron 开发使用 `npm run electron:dev`，需先构建 `../bin/easyagent` 和动态工作流 bundle（见 [工作流](DYNAMIC_WORKFLOW.md)）。首次安装依赖若跳过 scripts，应运行 `node node_modules/electron/install.js` 安装 Electron 运行时。

## macOS 打包

```bash
# 在仓库根目录执行，按机器架构选择
scripts/build-desktop.sh arm64
# 或 scripts/build-desktop.sh x64
```

脚本使用独立 `desktop/.bundle` 构建 Go 核心、工作流 bundle 与许可证，下载并核验官方便携 Node.js，随后检查 TypeScript、测试并打包。不会替换已有 CLI 或 8080 服务。输出在 `desktop/release/版本/架构`；包版本来自 `desktop/package.json`，本地打包不自动发布 GitHub Release。

发布构建使用 `scripts/build-desktop.sh arm64 v0.2.0-rc.1`（或 `x64`），只接受版本匹配的附注 tag、已合入 main 的干净源码；完成后只读挂载 DMG，验证包内核心、运行时、许可证与来源提交。`v0.2.0-rc.1` 作为测试版需手动下载，桌面更新提示仍只选择正式版。

当前打包面向 macOS，使用完整的 ad hoc 测试签名并在安装包内校验，未配置 Developer ID 签名与公证。桌面更新提示只匹配架构与桌面 DMG 资产，不把 Go CLI 的 Release 当成桌面安装包。正式分发前仍需开发者签名、公证及相应机器的安装验收。
