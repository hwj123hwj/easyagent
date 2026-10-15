# YAML 流水线

EasyAgent 的多步骤 Agent 编排：用 YAML 声明一个步骤 DAG（顺序依赖、fan-out、重试、人工确认门），服务端按依赖调度执行，每一步驱动一次独立 Agent 会话。引擎位于 `sdk/workflow`（零领域知识，可被外部 Go 模块复用），HTTP 入口在 serve 模式的 `/workflows`。

与 `/workflow` 使用的动态 Actor 引擎不同，动态流程见 [动态工作流](DYNAMIC_WORKFLOW.md)。

## 快速开始

```bash
# 启动 serve 模式后提交一个工作流
curl -s -H "Authorization: Bearer $EA_API_KEY" -X POST http://localhost:8080/workflows \
  -H 'Content-Type: text/yaml' \
  --data-binary @- <<'EOF'
name: research-pipeline
max_concurrency: 4
vars:
  topic: Go 泛型
  sources: ["类型约束", "性能", "工程实践"]
steps:
  - id: outline
    prompt: "为「{{vars.topic}}」写一份研究大纲"
  - id: research
    foreach: sources            # 或模板： "{{vars.sources}}"
    concurrency: 3
    retries: 1
    prompt: "针对 {{item}} 展开研究，给出要点"
    depends_on: [outline]
  - id: merge
    prompt: "把以下研究要点整理成报告：\n{{steps.research.output}}"
    depends_on: [research]
  - id: publish
    prompt: "将报告发布到知识库：\n{{steps.merge.output}}"
    confirm: true               # 人工确认门：需 POST approve 放行
    depends_on: [merge]
EOF
# → 202 {"run_id":"wf-1790344371-2333d68c","status":"running"}

# 将返回的 run_id 填入 RUN_ID
RUN_ID=your-run-id

# 查询进度（meta + 全部事件）
curl -s -H "Authorization: Bearer $EA_API_KEY" "http://localhost:8080/workflows/$RUN_ID"

# 确认门放行 / 拒绝
curl -s -H "Authorization: Bearer $EA_API_KEY" -X POST "http://localhost:8080/workflows/$RUN_ID/approve" -d '{}'
curl -s -H "Authorization: Bearer $EA_API_KEY" -X POST "http://localhost:8080/workflows/$RUN_ID/reject"  -d '{"step":"publish"}'

# 取消
curl -s -H "Authorization: Bearer $EA_API_KEY" -X POST "http://localhost:8080/workflows/$RUN_ID/cancel"
```

## YAML 规范

### 顶层字段

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `name` | string | ✅ | 工作流名，`[a-z0-9_-]`；缓存按名字隔离 |
| `description` | string | | 说明 |
| `timeout` | duration | | 运行整体超时，如 `30m` |
| `max_concurrency` | int | | 全局并发会话上限（默认 4，上限 64） |
| `vars` | map | | 用户变量，模板 `{{vars.x}}` 引用；POST 的 `vars` 可覆盖 |
| `steps` | list | ✅ | 步骤列表 |

### 步骤字段

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | string | ✅ | 步骤 ID，`[a-z0-9_-]`，全局唯一 |
| `prompt` | string | ✅ | 模板（见下） |
| `depends_on` | list | | 依赖的步骤 ID；**省略时默认依赖上一个步骤**（顺序链） |
| `foreach` | string 或 list | | fan-out：**内联列表**（如 `["a", "b"]`）、变量名（要求是数组）或模板（渲染成 JSON 数组或按行切分） |
| `concurrency` | int | | foreach 项并发上限（默认 `max_concurrency`） |
| `model` | string | | 本步骤模型覆盖 |
| `retries` | int | | 失败重试次数（0–10），退避为 n×基数 |
| `confirm` | bool | | 人工确认门：执行前暂停等 approve/reject（不支持 foreach 步骤） |

### 模板语法

| 写法 | 含义 |
|---|---|
| `{{vars.topic}}` | 用户变量 |
| `{{steps.a.output}}` | 已完成步骤的全部产出（foreach 为各项 join） |
| `{{steps.a.outputs}}` | foreach 各项列表，可配合 `{{range .steps.a.outputs}}…{{end}}` |
| `{{item}}` / `{{index}}` | foreach 当前项与序号 |

引用不存在的键会直接报错（`missingkey=error`）；引用尚未完成的步骤同样报错，所以**跨步骤引用必须写进 `depends_on`**。

### 失败语义

- 步骤重试耗尽 → 该步骤 `failed`，**下游步骤全部 `skipped`**，运行终态 `failed`。
- foreach 某一项最终失败 → 取消其余未启动的项（fail-fast），步骤 `failed`。
- 确认门被拒 → 步骤 `rejected`，下游跳过，运行终态 `rejected`。
- 取消/超时 → 运行终态 `cancelled`。

## 步骤缓存

每个步骤执行单元（普通步骤一个、foreach 每项一个）的签名 =
`hash(工作流名, 步骤ID, 项, 渲染后完整 prompt, 模型)`。

签名命中即复用上次产出，**不调用 LLM、不计费**。因此：

- 改了 `vars` → 只有受影响的步骤重新执行；
- 上游产出变化 → 下游 prompt 变化 → 下游重算，上游若未变仍然命中；
- 确认门步骤每次运行仍会等待审批，批准后才查缓存。

缓存放 `DataDir/workflows/cache/<name>.json`，删除即失效。这就是"修订重跑"的实现：改完 YAML 再提交一次，已完成的步骤免费。

## 数据与可观测性

```
DataDir/workflows/
├── cache/<name>.json            # 步骤缓存
└── <run-id>/
    ├── meta.json                # 聚合状态（GET /workflows 列表与详情的数据源）
    └── journal.jsonl            # 全部事件：run_start/step_start/step_end/
                                 #   item_end/step_error/gate_wait/gate_result/run_end
```

`GET /workflows/{id}` 返回 `meta`（含各步骤状态与最终产出）+ `events`（journal 原文）。每一步都是真实 AgentSession，可在 `/sessions` 里回看完整执行过程。

运行状态：`running` / `waiting_approval` / `completed` / `failed` / `rejected` / `cancelled`。

## 已知限制（v1）

- 每步独立会话（上下文隔离），prompt 需通过模板自包含；无共享会话模式。
- 进程重启会丢失等待审批中的运行 goroutine：磁盘 meta 停在 `waiting_approval`，用 `POST /workflows/{id}/cancel` 清理即可。
- `confirm` + `foreach` 组合不支持（校验会拒绝）。

## SDK 用法

```go
import "github.com/hwj123hwj/easyagent/sdk/workflow"

eng := &workflow.Engine{
    Factory: myRunnerFactory,          // 实现 workflow.RunnerFactory
    Dir:     "./data/workflows",
}
status, err := eng.Run(ctx, spec, vars, runID)          // 同步执行
reg := workflow.NewRegistry(eng)
runID, err := reg.Start(spec, vars)                     // 异步 + 审批/取消
```

`workflow.Engine.Gates` 为 nil 时确认门自动放行（便于测试与嵌入）。
