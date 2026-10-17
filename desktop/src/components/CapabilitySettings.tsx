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
      {!id && <p>选择一个项目会话后查看实际加载的技能。</p>}
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
        </>
      )}
    </section>
  );
}
