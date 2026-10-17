# Go 原生桌面客户端

EasyAgent 的唯一维护中桌面客户端是 `desktop-native/`，使用 Go / MyGo 原生界面，Bundle ID 为 `com.hwj.easyagent`。界面不使用 Electron、React、HTML 或 WebView；macOS 电脑控制通过随包提供的 Swift 系统助手实现。核心服务、TUI 和桌面应用均由 Go 构建；服务内嵌 Web 界面继续独立维护。

## 安装与启动

Apple Silicon Mac 从 Release 下载 `easyagent-native-darwin-arm64.dmg`，正式安装入口为 `/Applications/EasyAgent.app`。当前使用 ad hoc 签名，尚未进行 Developer ID 签名与 Apple 公证。原生客户端已接入签名更新：设置中可检查更新，并选择自动检查或后台下载。存在活动任务、待审批操作或未保存文件时会阻止退出与更新重启。更新频道需要正式 Release 发布签名清单及归档后才可用；发布构建要求见 RELEASING.md。

本机构建：

```sh
make desktop
open desktop-native/build.noindex/darwin-arm64/EasyAgent.app
```

构建脚本会验证实际 bundle 的核心查找、启动、`/health` 响应和退出回收。产物保存在 `.noindex` 目录，不自动替换现有应用或运行服务。原生 GUI 验收结束后先退出应用，再执行：

```sh
python3 scripts/unregister-desktop-apps.py desktop-native/build.noindex/darwin-arm64/EasyAgent.app
```

## 配置和数据

“运行主机”支持此 Mac 与远程 Go 服务。此 Mac 启动独立 loopback 核心，使用应用自己的 `core-data` 和 `core-home`；模型配置可沿用 CLI 默认值，也可在“模型连接”中单独设置。远程主机配置需要服务地址和 API 令牌。密钥由 macOS 钥匙串保存，普通 JSON 仅存元数据。

连接现有服务才能查看该服务上的会话。切换主机不会取消远程任务；退出应用会关闭它托管的本地核心。存在运行任务时，修改本地模型配置会拒绝重启，须先停止任务。

会话、工具审批、队列、文件工作台、终端、技能市场、MCP、飞书及音频能力见 [原生客户端说明](../desktop-native/README.md)。电脑控制需要 macOS 14+，辅助功能和屏幕录制授权仍由用户完成，见 [配置说明](CONFIG.md#实验性电脑控制)。

## 旧客户端归档

Electron / React 的 `desktop/`（含共用前端的 Capacitor Android 外壳）已退役，不再构建、测试或发布。从 v0.16.0 起 Release 不再要求 Electron DMG；已发布版本与资产不变。

历史源码与文档可从 [archive/workflow](https://github.com/hwj123hwj/easyagent/tree/archive/workflow/desktop) 查看，退役前的精确快照为 [e4b4088](https://github.com/hwj123hwj/easyagent/tree/e4b4088/desktop)。Electron 的自动更新、固定证书重签名及 safeStorage 数据不适用于原生客户端；本次退役不会删除旧用户数据、签名钥匙串或覆盖已安装应用。
