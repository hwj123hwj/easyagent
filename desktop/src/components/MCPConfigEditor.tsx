import { useState } from "react";
interface Fields {
  transport: string;
  command: string;
  args: string;
  variables: string;
  url: string;
  timeout: string;
  trust: boolean;
}
const initial: Fields = {
  transport: "stdio",
  command: "",
  args: "",
  variables: "",
  url: "",
  timeout: "",
  trust: false,
};
export function MCPConfigEditor({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  const [advanced, setAdvanced] = useState(false),
    [fields, setFields] = useState(initial),
    [error, setError] = useState(""),
    [base, setBase] = useState<Record<string, unknown>>({});
  function update(patch: Partial<Fields>) {
    const next = { ...fields, ...patch };
    setFields(next);
    setError("");
    try {
      const config: Record<string, unknown> = {
        ...base,
        type: next.transport,
        trust: next.trust,
      };
      for (const key of [
        "command",
        "args",
        "env",
        "url",
        "httpUrl",
        "headers",
        "timeout",
      ])
        delete config[key];
      if (next.transport === "stdio") {
        if (!next.command.trim()) {
          onChange("");
          return;
        }
        config.command = next.command.trim();
        config.args = next.args.split("\n").filter(Boolean);
      } else {
        const parsed = new URL(next.url);
        if (
          !["http:", "https:"].includes(parsed.protocol) ||
          parsed.username ||
          parsed.password ||
          parsed.hash
        )
          throw new Error("请输入有效 HTTP(S) 地址，不要在地址中填写密码");
        config.url = parsed.href;
      }
      const pairs: Record<string, string> = {};
      for (const line of next.variables
        .split("\n")
        .filter((line) => line.trim())) {
        const index = line.indexOf("=");
        if (index < 1) throw new Error("每行使用 KEY=VALUE 格式");
        const key = line.slice(0, index).trim();
        if (/[\x00\r\n=]/.test(key)) throw new Error("变量名无效");
        pairs[key] = line.slice(index + 1);
      }
      if (Object.keys(pairs).length)
        config[next.transport === "stdio" ? "env" : "headers"] = pairs;
      if (next.timeout) {
        const timeout = Number(next.timeout);
        if (!Number.isInteger(timeout) || timeout < 0)
          throw new Error("超时须为非负整数毫秒");
        config.timeout = timeout;
      }
      onChange(JSON.stringify(config, null, 2));
    } catch (e) {
      setError((e as Error).message);
      onChange("");
    }
  }
  function toggle() {
    if (advanced) {
      try {
        const config = JSON.parse(value || "{}");
        if (
          !config ||
          typeof config !== "object" ||
          Array.isArray(config) ||
          (config.type && !["stdio", "http"].includes(config.type))
        )
          throw new Error();
        if (
          [config.command, config.url, config.httpUrl].some(
            (v) => v !== undefined && typeof v !== "string",
          ) ||
          (config.args !== undefined &&
            (!Array.isArray(config.args) ||
              config.args.some((v: unknown) => typeof v !== "string")))
        )
          throw new Error();
        setBase(config);
        setFields({
          transport: config.command ? "stdio" : "http",
          command: config.command || "",
          args: (config.args || []).join("\n"),
          variables: Object.entries(config.env || config.headers || {})
            .map(([k, v]) => k + "=" + v)
            .join("\n"),
          url: config.url || config.httpUrl || "",
          timeout: config.timeout ? String(config.timeout) : "",
          trust: !!config.trust,
        });
        setError("");
      } catch {
        setError("请检查 JSON 对象及连接类型；其他字段继续保留");
        return;
      }
    }
    setAdvanced(!advanced);
  }
  return (
    <div className="mcp-config-editor">
      <div className="settings-preference-row">
        <span>
          <strong>{advanced ? "高级 JSON" : "工具连接配置"}</strong>
          <small>配置与密钥属于当前运行主机。</small>
        </span>
        <button className="btn" onClick={toggle}>
          {advanced ? "返回表单" : "高级 JSON"}
        </button>
      </div>
      {advanced ? (
        <label>
          标准 MCP 配置
          <textarea
            className="config-input"
            value={value}
            onChange={(e) => onChange(e.target.value)}
            spellCheck={false}
          />
        </label>
      ) : (
        <>
          <label>
            连接方式
            <select
              value={fields.transport}
              onChange={(e) => update({ transport: e.target.value })}
            >
              <option value="stdio">本机命令 · stdio</option>
              <option value="http">远程服务 · Streamable HTTP</option>
            </select>
          </label>
          {fields.transport === "stdio" ? (
            <>
              <label>
                启动命令
                <input
                  value={fields.command}
                  onChange={(e) => update({ command: e.target.value })}
                  placeholder="npx"
                />
              </label>
              <label>
                命令参数 · 每行一个
                <textarea
                  value={fields.args}
                  onChange={(e) => update({ args: e.target.value })}
                  placeholder={
                    "-y\n@modelcontextprotocol/server-filesystem\n/your/project"
                  }
                  spellCheck={false}
                />
              </label>
            </>
          ) : (
            <label>
              服务地址
              <input
                value={fields.url}
                onChange={(e) => update({ url: e.target.value })}
                placeholder="https://example.com/mcp"
              />
            </label>
          )}
          <details>
            <summary>
              {fields.transport === "stdio" ? "环境变量" : "请求头"}与超时
            </summary>
            <label>
              {fields.transport === "stdio" ? "环境变量" : "请求头"} · 每行
              KEY=VALUE
              <textarea
                value={fields.variables}
                onChange={(e) => update({ variables: e.target.value })}
                spellCheck={false}
                autoComplete="off"
              />
            </label>
            <label>
              超时 · 毫秒
              <input
                type="number"
                min="0"
                value={fields.timeout}
                onChange={(e) => update({ timeout: e.target.value })}
              />
            </label>
          </details>
          <label className="mcp-tool-toggle">
            <input
              type="checkbox"
              checked={fields.trust}
              onChange={(e) => update({ trust: e.target.checked })}
            />
            信任此工具服务
          </label>
          <p className="settings-note">
            stdio 会在所选主机启动命令；仅信任你了解的工具来源。
          </p>
        </>
      )}
      {error && (
        <p role="alert" className="inline-error">
          {error}
        </p>
      )}
    </div>
  );
}
