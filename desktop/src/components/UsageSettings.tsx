import { useEffect, useState } from "react";
import { apiRequest, useStore } from "../store";

interface UsageBucket {
  session_id?: string;
  run_count: number;
  input_tokens: number;
  output_tokens: number;
  cached_input_tokens?: number;
  last_used_at?: string;
}

interface UsageSummary {
  since: string;
  until: string;
  total: UsageBucket;
  sessions: UsageBucket[];
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(2) + "M";
  if (n >= 1_000) return (n / 1_000).toFixed(1) + "K";
  return String(n);
}

export function UsageSettings() {
  const [data, setData] = useState<UsageSummary | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const sessions = useStore((s) => s.sessions);
  const setActive = useStore((s) => s.setActive);
  const openSettings = useStore((s) => s.openSettings);

  const load = async () => {
    setLoading(true);
    setError("");
    try {
      const res = await apiRequest<UsageSummary>("GET", "/usage/summary");
      setData(res);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const total = data?.total;
  const grandTokens = (total?.input_tokens || 0) + (total?.output_tokens || 0);

  return (
    <div className="usage-settings" style={{ padding: "8px 0" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 16 }}>
        <div>
          <h3 style={{ margin: 0, fontSize: 15, fontWeight: 600 }}>近 30 天用量概览</h3>
          <p style={{ margin: "4px 0 0", fontSize: 12, opacity: 0.7 }}>
            基于本地持久化的执行收据统计（含多轮工具调用的累计消耗）
          </p>
        </div>
        <button
          className="btn"
          onClick={() => void load()}
          disabled={loading}
          style={{ padding: "4px 10px", fontSize: 12 }}
        >
          {loading ? "刷新中…" : "刷新"}
        </button>
      </div>

      {error && (
        <div style={{ color: "var(--danger, #ef4444)", fontSize: 13, marginBottom: 12 }}>
          加载失败: {error}
        </div>
      )}

      {total && (
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(4, 1fr)",
            gap: 12,
            marginBottom: 20,
          }}
        >
          <div style={{ background: "var(--bg-elev)", padding: 12, borderRadius: 8, border: "1px solid var(--border)" }}>
            <div style={{ fontSize: 11, opacity: 0.7 }}>总 Tokens</div>
            <div style={{ fontSize: 20, fontWeight: 700, marginTop: 4 }}>{formatTokens(grandTokens)}</div>
          </div>
          <div style={{ background: "var(--bg-elev)", padding: 12, borderRadius: 8, border: "1px solid var(--border)" }}>
            <div style={{ fontSize: 11, opacity: 0.7 }}>输入 Tokens</div>
            <div style={{ fontSize: 20, fontWeight: 700, marginTop: 4 }}>{formatTokens(total.input_tokens)}</div>
          </div>
          <div style={{ background: "var(--bg-elev)", padding: 12, borderRadius: 8, border: "1px solid var(--border)" }}>
            <div style={{ fontSize: 11, opacity: 0.7 }}>输出 Tokens</div>
            <div style={{ fontSize: 20, fontWeight: 700, marginTop: 4 }}>{formatTokens(total.output_tokens)}</div>
          </div>
          <div style={{ background: "var(--bg-elev)", padding: 12, borderRadius: 8, border: "1px solid var(--border)" }}>
            <div style={{ fontSize: 11, opacity: 0.7 }}>总任务数 (Runs)</div>
            <div style={{ fontSize: 20, fontWeight: 700, marginTop: 4 }}>{total.run_count}</div>
          </div>
        </div>
      )}

      {data?.sessions && data.sessions.length > 0 ? (
        <div>
          <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 8 }}>会话消耗排行</div>
          <div
            style={{
              border: "1px solid var(--border)",
              borderRadius: 8,
              overflow: "hidden",
              background: "var(--bg-elev)",
            }}
          >
            <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 12 }}>
              <thead>
                <tr style={{ background: "var(--bg-hover)", borderBottom: "1px solid var(--border)", textAlign: "left" }}>
                  <th style={{ padding: "8px 12px" }}>会话</th>
                  <th style={{ padding: "8px 12px", width: 80, textAlign: "right" }}>Runs</th>
                  <th style={{ padding: "8px 12px", width: 100, textAlign: "right" }}>输入</th>
                  <th style={{ padding: "8px 12px", width: 100, textAlign: "right" }}>输出</th>
                  <th style={{ padding: "8px 12px", width: 100, textAlign: "right" }}>合计</th>
                </tr>
              </thead>
              <tbody>
                {data.sessions.map((s) => {
                  const title = (s.session_id && sessions[s.session_id]?.meta?.title) || s.session_id || "未知会话";
                  const subtotal = s.input_tokens + s.output_tokens;
                  return (
                    <tr
                      key={s.session_id || "unknown"}
                      style={{ borderBottom: "1px solid var(--border)", cursor: s.session_id ? "pointer" : "default" }}
                      onClick={() => {
                        if (s.session_id) {
                          void setActive(s.session_id);
                          openSettings(false);
                        }
                      }}
                      title={s.session_id ? "点击跳转到该会话" : undefined}
                    >
                      <td style={{ padding: "8px 12px", maxWidth: 260, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                        {title}
                      </td>
                      <td style={{ padding: "8px 12px", textAlign: "right" }}>{s.run_count}</td>
                      <td style={{ padding: "8px 12px", textAlign: "right" }}>{formatTokens(s.input_tokens)}</td>
                      <td style={{ padding: "8px 12px", textAlign: "right" }}>{formatTokens(s.output_tokens)}</td>
                      <td style={{ padding: "8px 12px", textAlign: "right", fontWeight: 600 }}>{formatTokens(subtotal)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </div>
      ) : (
        !loading && <div style={{ fontSize: 13, opacity: 0.6, padding: "24px 0", textAlign: "center" }}>近 30 天暂无执行记录</div>
      )}
    </div>
  );
}
