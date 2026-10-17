# EasyAgent 原生桌面客户端

MyGo v0.5.5 的原生界面，直接复用现有 EasyAgent Go 核心的 REST/WebSocket 协议。UI 不使用 React、HTML、JavaScript 或 WebView；电脑控制继续使用原有系统助手并随包提供。

正式桌面客户端，Bundle ID 为 `com.hwj.easyagent`。安装产物随正式 Release 发布（`easyagent-native-darwin-arm64.dmg`）；本机构建产物只进入 `build.noindex/`，不自动替换 `/Applications/EasyAgent.app`。

```sh
cd desktop-native
go run ./tools/build
# 使用已准备的资源开发：
EASYAGENT_NATIVE_CORE="$PWD/resources/darwin-arm64/easyagent" go run .
```

构建生成 `build.noindex/darwin-arm64/EasyAgent.app` 和安装镜像。构建核心取当前工作树源码；不安装、停止或覆盖已有服务。

## 维护范围

Go / MyGo 是唯一维护中的桌面客户端，不依赖 Electron、React、Node 或 WebView。macOS 电脑控制仍需要随包提供的 Swift 系统助手；Go 服务内嵌的 Web 界面独立保留。

旧 `desktop/`（含 Capacitor Android 外壳）已退役，完整历史可从 `archive/workflow` 查看；退役前精确源码见提交 `e4b4088`。旧客户端的用户数据、已安装应用和签名身份不会自动删除或迁移。原生客户端使用 MyGo 原生签名更新器，从正式 Release 检查和安装更新。

## 实际功能

- 本机核心进程的启动、鉴权、重连、退出清理；远程主机保存、切换与重连。
- 会话新建、历史恢复、搜索、重命名、置顶、归档、删除、用户消息编辑分支和回复分支。
- 发送确认后清空草稿；流式文本、思考、工具记录、任务审批、用户问答、停止、断线恢复和失败重试。后台任务完成不会切走当前会话。
- 消息队列、暂停/继续、编辑、立即发送、移除，附件上传（每个 1 字节至 4 MiB），原生录音和后端 ASR 转写。
- 真实模型目录、会话模型切换、本地模型检测、配置保存与重启。后台仍有任务时拒绝重启。
- 十类设置：常规、外观、模型连接、用量统计、技能市场、实验、运行主机、MCP 工具、技能、飞书机器人。
- MCP 项目信任、配置表单/JSON、连接、工具开关、OAuth 回调、授权和删除；技能市场搜索、详情、安装进度、取消与卸载。
- 文件浏览、搜索、多标签编辑、Markdown/图片预览、保存、导出；文件按主机和工作区隔离，保存前检查外部修改。
- 运行文件差异和受版本校验保护的撤销；原生终端通过服务的鉴权 WebSocket 连接，可查看任务、计划、上下文、记忆和知识库，并执行对应管理操作。
- 音乐工具结果的原生播放、暂停、进度、音量和跨会话播放。

快捷键：Enter 发送，Shift+Enter 换行，⌘N 新会话，⌘P 工作台，⌘⇧G 审查，⌘, 设置，⌘F 搜索当前会话。命令菜单支持上下选择、Tab/Enter 补全和 Esc 关闭。

## 数据与配置

偏好、主机元数据、模型元数据和草稿保存在此应用自己的 UserData 目录。模型密钥和远程主机令牌保存在 macOS 钥匙串，不写入 JSON。

本机核心使用独立的 `core-data` 和 `EA_HOME=core-home`；MCP、飞书凭据、会话、执行收据与运行覆盖均在此应用目录下。继承模式只读取普通 CLI 的模型/工具默认配置与环境变量。飞书凭据沿用核心格式并使用私有文件权限，手动更新保留已有授权和机器人字段，换 App ID 需要重新绑定。

远程主机上的保存、安装、工具执行、文件写入与删除会实际生效。选择的项目目录和共享个人技能目录也是真实目录。可先选择临时项目检查操作，再用于日常工作。

原生界面尚不能宣称与 Electron 全面逐像素一致。Mermaid 使用独立兼容渲染器，PDF 原生预览当前仅在 macOS 实机验证；不同平台的字体、文件选择器及系统授权界面由系统决定。

“检查更新”打开原生更新窗口，可查看版本、下载进度、取消、跳过版本或稍后安装。默认每天检查更新，自动下载可在常规设置开启。仅接受 Ed25519 签名的原生压缩包；任务运行、配置保存或文件未保存时阻止重启。首个包含更新清单的正式版本发布前，线上检查会返回未找到清单。桌面/文稿/下载目录访问、飞书登录、MCP OAuth、麦克风及电脑控制需要用户完成对应授权；服务端未配置 ASR 或关闭某项能力时，界面显示实际错误。

## 验证

```sh
go run ./tools/prepare
EASYAGENT_NATIVE_INTEGRATION=1 go test -race -count=1 ./...
go vet ./...
go run github.com/egoist/mygo/cmd/mygo vet
# 可选：系统钥匙串合成凭据往返（只创建并删除测试条目）
EASYAGENT_NATIVE_KEYCHAIN_TEST=1 go test -run TestNativeSystemKeychainRoundTrip
EASYAGENT_PREVIEW_SNAPSHOTS=build.noindex/review go test -run 'TestNativeLiveSettingsLayouts|TestPreviewSnapshots|TestSettingsSnapshots'
```

集成测试启动真实核心进程，使用临时数据目录、临时工作区和本机模拟模型接口，覆盖会话/流式输出、真实工具审批、取消、文件、附件、模型保存和重启、会话恢复、终端和关键隔离边界，不请求真实模型。

原生 GUI 验收结束后先退出应用，再撤销测试注册：

```sh
python3 ../scripts/unregister-desktop-apps.py "build.noindex/darwin-arm64/EasyAgent.app"
```
