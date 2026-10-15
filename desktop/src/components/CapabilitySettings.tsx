import { useEffect, useState } from "react";
import { apiRequest, useStore } from "../store";
interface Catalog {
  workspace: string;
  skills: {
    name: string;
    description: string;
    source: string;
    scope: string;
    model_invocation: boolean;
  }[];
  tools: string[];
  workflow_entry: string;
  workflows: {
    id: string;
    name: string;
    status: string;
    error?: string;
    result?: unknown;
  }[];
}
export function CapabilitySettings() {
  const id = useStore((s) => s.activeSessionId),
    profile = useStore((s) => s.selectedProfile),
    [data, setData] = useState<Catalog>(),
    [error, setError] = useState(""),
    [revision, setRevision] = useState(0);
  useEffect(() => {
    let alive = true;
    setData(undefined);
    setError("");
    if (id)
      void apiRequest<Catalog>(
        "GET",
        `/sessions/${encodeURIComponent(id)}/capabilities`,
      )
        .then((v) => {
          if (alive) setData(v);
        })
        .catch((e) => {
          if (alive) setError(e.message);
        });
    return () => {
      alive = false;
    };
  }, [id, profile, revision]);
  return (
    <section className="settings-section">
      <div className="settings-section-heading">
        <h2>当前会话的能力</h2>
        <button
          className="btn"
          disabled={!id}
          onClick={() => setRevision((x) => x + 1)}
        >
          刷新
        </button>
      </div>
      {!id && <p>选择一个项目会话后查看实际加载的技能与工作流。</p>}
      {error && <p className="inline-error">{error}</p>}
      {data && (
        <>
          <p>项目：{data.workspace}</p>
          <p className="settings-note">
            这里列出构建当前会话提示时使用的技能。默认读取项目与个人
            .agents/skills（含网关同步链接），兼容
            .claude/skills。同名技能以项目为先；文件变更后新建会话加载。
          </p>
          <h2>已加载技能 · {data.skills.length}</h2>
          {!data.skills.length && <p>当前没有加载技能。</p>}
          {data.skills.map((skill) => (
            <details className="capability-entry" key={skill.name}>
              <summary>
                <strong>{skill.name}</strong>
                <small>
                  {skill.scope === "project" ? "项目" : "个人"} ·{" "}
                  {skill.model_invocation ? "可由模型调用" : "仅显式调用"}
                </small>
              </summary>
              <p>{skill.description}</p>
              <code>{skill.source}</code>
            </details>
          ))}
          <h2>工作流</h2>
          <p>
            入口 <code>{data.workflow_entry}</code>，使用 create_workflow /
            get_workflow / resume_workflow
            工具创建、查看与恢复。可在对话里描述角色、步骤和验收条件。
          </p>
          <p className="settings-note">
            可用工具：
            {data.tools
              .filter((name) => name.includes("workflow"))
              .join(" · ") || "当前工具策略未开放工作流"}
          </p>
          {data.workflows.length ? (
            data.workflows
              .slice()
              .reverse()
              .slice(0, 20)
              .map((run) => (
                <details className="capability-entry" key={run.id}>
                  <summary>
                    <strong>{run.name}</strong>
                    <small>{run.status}</small>
                  </summary>
                  <code>{run.id}</code>
                  {run.result !== undefined && (
                    <pre>
                      {typeof run.result === "string"
                        ? run.result
                        : JSON.stringify(run.result, null, 2)}
                    </pre>
                  )}
                  {run.error && <p className="inline-error">{run.error}</p>}
                  <button
                    className="btn"
                    onClick={() => {
                      useStore
                        .getState()
                        .setDraft(
                          id!,
                          "请用 get_workflow 查看工作流 " +
                            run.id +
                            " 的状态和结果，不要新建流程。",
                        );
                      useStore.getState().openSettings(false);
                    }}
                  >
                    在对话中查看
                  </button>
                </details>
              ))
          ) : (
            <p>当前会话暂无工作流运行记录。</p>
          )}
        </>
      )}
    </section>
  );
}
