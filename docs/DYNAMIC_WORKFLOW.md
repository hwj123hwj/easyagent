# 动态工作流

EasyAgent 复用 ZCode 的 TypeScript 动态工作流引擎，通过 Go 适配层驱动真实 Agent 会话。来源固定为 ZCode `ba61ca16e1790878c49566fc2f3cb6e908a4ea77`，源码与许可保存在 `workflow-runtime/vendor/`，构建不依赖本机的 ZCode 目录。

## 使用

在 Web、TUI 或已配对的飞书对话中输入：

```text
/workflow 分别研究技术、成本与风险，再汇总成报告
```

主 Agent 根据任务编写 TypeScript 流程并调用 `create_workflow`。Web 的“工作流”页显示运行、执行节点、Actor 会话、最终产出与脚本；`/workflow` 不带参数时显示帮助（Web 打开该页面）。原有 YAML 流水线继续在“流水线”页使用。

脚本是 async 函数体，支持顶层 await/return。例如：

```ts
interface Plan { topics: string[] }
const planner = agent('planner', { system: '负责分解任务并汇总结果' });
const plan = await planner.ask<Plan>('给出需要研究的主题');
const results = await Promise.all(plan.topics.map(async topic => {
  const researcher = agent(topic);
  return await researcher.ask('研究这个主题：' + topic);
}));
return await planner.ask('汇总为报告：' + results.join('\n'));
```

- 不同 Actor 并行；同一个 Actor 的 ask 按顺序执行并保留会话上下文。
- 可以根据中间结果分支、循环、组合 `Promise.all`。泛型 ask 生成结果 schema，引擎校验返回值并在同一会话中请求修正。
- Actor 使用 EasyAgent 的模型与工具，继承父会话工作目录和模型配置；交互确认沿用父会话回调。Actor 本身不再获得工作流启动工具。
- 脚本不能直接取得 Node、文件或网络对象。宿主只接通 agent/ask、report/phase；外部操作交给 Actor 的已有工具执行。进程隔离不等同于 OS 安全沙箱，现有工具权限仍是信任边界。

## 取消与恢复

对话取消会取消其启动的流程；Web 也可单独取消。每个运行最多 30 分钟、每个引擎最多 4 个并发 Actor，服务最多同时启动 4 个运行。

日志和 Actor 关联保存在 `EA_DATA_DIR/dynamic-workflows/<run-id>/`，会话配置与消息保存在原会话目录。引擎状态变化在返回前写入并同步 WAL。服务重启后的运行显示为“等待恢复”，不会自动重做外部操作。

恢复会重用已完成的 ask 结果、原脚本与 Actor 会话。**尚未完成的 ask 可能已经执行过文件写入或命令，恢复时可能再次执行这些动作**，不能保证副作用恰好执行一次。Web 恢复前要求确认已检查记录；Agent 可通过 `get_workflow` 和 `resume_workflow` 检查与恢复。交互确认会话失去原确认入口时，恢复会拒绝执行，而不是自动批准。

当前没有接入 ZCode 的脚本修订恢复、seed、world/files/git/artifact 端口、可复用命名模板或可视化拓扑编辑器。已完成/脚本失败的运行不可恢复；模型中断、取消、服务中断可用原脚本恢复。工作流运行期间，Actor 会话可查看，但不能从另一个请求对其发起聊天、改模型、压缩或删除。

## 构建与发布

动态工作流额外需要 **Node.js 22+**；普通对话仍可独立使用 Go 二进制。

```bash
cd workflow-runtime
npm ci --ignore-scripts
npm run build
npm test
cd ..
go build -o easyagent ./cmd/easyagent
EA_WORKFLOW_RUNTIME="$PWD/workflow-runtime/output/workflow-runtime.mjs" ./easyagent serve
```

默认从 easyagent 可执行文件旁加载 `workflow-runtime.mjs`；可用 `EA_WORKFLOW_RUNTIME` 指定绝对路径。Release 附带该 bundle 与许可证压缩包；安装时将 bundle 放在二进制旁，并安装 Node.js 22+。源码、npm lockfile 和打包脚本保证依赖版本固定，bundle 不需运行时 npm 安装。

迷你主机更新脚本先构建并验证引擎，再测试 Go、构建核心与飞书桥接。新 bundle 与核心一起切换，健康检查失败时一起回滚。服务的 PATH 必须能找到 Node；`node workflow-runtime.mjs --check` 可检查运行组件。

## 验证

- 真实引擎搭配确定性 Host：不同 Actor 并行、同 Actor 顺序、类型化结果、条件分支、取消、持久化后恢复、已完成 ask 去重、无效脚本拒绝。
- App：继承模型/工作目录、保留工具确认、限制递归启动、持久化 Actor 配置。
- HTTP：认证保护、恢复确认；浏览器：slash 提示、发送、运行列表、节点、Actor 会话与脚本。
- WAL 重放和截断尾记录恢复；Linux 部署测试覆盖运行组件安装及回滚。

浏览器验收使用本地确定性模型，不将模拟结果作为真实模型表现或飞书端到端验证。
