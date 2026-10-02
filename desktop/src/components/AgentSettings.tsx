import { useEffect, useRef, useState } from "react";
import { apiRequest, useStore } from "../store";
import { Icon } from "./Icon";
interface MCPServer {
  name: string;
  source: string;
  scope: string;
  transport: string;
  state: string;
  error?: string;
  tool_count: number;
  trusted: boolean;
  enabled: boolean;
  tools: Array<{ name: string; description: string; enabled: boolean }>;
}
interface MCPList {
  workspace: string;
  project_trusted: boolean;
  servers: MCPServer[];
  issues: Array<unknown>;
}
export function AgentSettings() {
  const update = useStore((s) => s.update);
  const theme = useStore((s) => s.theme),
    lang = useStore((s) => s.lang);
  const profiles = useStore((s) => s.profiles),
    selected = useStore((s) => s.selectedProfile),
    active = useStore((s) => s.activeSessionId),
    cwd = useStore((s) => (active ? s.sessions[active]?.meta.cwd : ""));
  const tab = useStore((s) => s.settingsTab),
    setTab = (tab: "connections" | "mcp") =>
      useStore.getState().openSettings(true, tab);
  const connected = useStore((s) => s.connected);
  const [editing, setEditing] = useState("mini"),
    [name, setName] = useState("迷你主机"),
    [url, setUrl] = useState("http://192.168.5.16:8080"),
    [token, setToken] = useState(""),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(false);
  const [workspace, setWorkspace] = useState(cwd || ""),
    [list, setList] = useState<MCPList | null>(null),
    [mcpName, setMcpName] = useState(""),
    [scope, setScope] = useState("user"),
    [configuration, setConfiguration] = useState(""),
    [loading, setLoading] = useState(false);
  const context = useRef(""),
    loadVersion = useRef(0);
  context.current = `${selected}\0${workspace}`;
  const params = `?workspace=${encodeURIComponent(workspace)}&scope=${scope}`;
  useEffect(() => {
    if (
      editing === "mini" &&
      !profiles.some((p) => p.id === "mini") &&
      profiles.some((p) => p.kind === "remote")
    )
      setEditing(profiles.find((p) => p.kind === "remote")!.id);
  }, [profiles, editing]);
  useEffect(() => {
    const p = profiles.find((p) => p.id === editing);
    if (p) {
      setName(p.name);
      setUrl(p.url);
      setToken("");
    }
  }, [editing, profiles]);
  async function load() {
    const issuedContext = context.current,
      version = ++loadVersion.current;
    setLoading(true);
    try {
      const result = await apiRequest<MCPList>(
        "GET",
        "/mcp?workspace=" + encodeURIComponent(workspace),
      );
      if (issuedContext !== context.current || version !== loadVersion.current)
        return;
      setList(result);
      setNotice("");
    } catch (error) {
      if (issuedContext === context.current && version === loadVersion.current)
        setNotice((error as Error).message);
    } finally {
      if (issuedContext === context.current && version === loadVersion.current)
        setLoading(false);
    }
  }
  useEffect(() => {
    setWorkspace(cwd || "");
    setList(null);
    setNotice("");
    setConfiguration("");
    setMcpName("");
  }, [selected]);
  useEffect(() => {
    setList(null);
    setLoading(false);
  }, [selected, workspace]);
  useEffect(() => {
    if (tab === "mcp" && connected) void load();
  }, [tab, selected, connected]);
  useEffect(() => {
    if (tab !== "mcp" || !list?.servers.some((s) => s.state === "connecting"))
      return;
    const timer = setTimeout(() => void load(), 1500);
    return () => clearTimeout(timer);
  }, [tab, list]);
  async function action(server: MCPServer, operation: string) {
    setBusy(true);
    setNotice("");
    try {
      if (operation === "login") {
        if (!window.piAPI)
          throw new Error("网页预览请使用桌面客户端完成 OAuth 授权");
        await window.piAPI.loginMCP(server.name, workspace);
      } else
        await apiRequest(
          operation === "delete" ? "DELETE" : "POST",
          `/mcp/servers/${encodeURIComponent(server.name)}${operation === "delete" ? "" : "/" + operation}?workspace=${encodeURIComponent(workspace)}&scope=${server.scope}`,
        );
      await load();
      setNotice(operation === "login" ? "授权已完成" : "配置已更新");
    } catch (error) {
      setNotice((error as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="main settings-main">
      <div className="settings-heading">
        <div>
          <h1>工作区设置</h1>
          <p>连接你的 Agent，让能力在正确的主机上运行。</p>
        </div>
        <button
          className="btn"
          onClick={() => useStore.getState().openSettings(false)}
        >
          返回对话
        </button>
      </div>
      <nav className="settings-tabs" aria-label="设置分类">
        <button
          aria-current={tab === "connections" ? "page" : undefined}
          onClick={() => setTab("connections")}
        >
          连接与运行
        </button>
        <button
          aria-current={tab === "mcp" ? "page" : undefined}
          onClick={() => setTab("mcp")}
        >
          MCP 工具
        </button>
      </nav>
      <div className="settings-scroll">
        {tab === "connections" ? (
          <section className="settings-section">
            <h2>运行主机</h2>
            <p>
              此 Mac 运行本地任务；迷你主机保留日常会话。文件路径、工具和 MCP
              均属于所选服务主机。
            </p>
            <div className="profile-list">
              {profiles.map((p) => (
                <button
                  key={p.id}
                  className={
                    "profile-row " + (p.id === selected ? "selected" : "")
                  }
                  disabled={busy}
                  onClick={() => void useStore.getState().connectProfile(p.id)}
                >
                  <span>
                    <strong>{p.name}</strong>
                    <small>
                      {p.kind === "local" ? "应用管理的本地 Agent" : p.url}
                    </small>
                  </span>
                  <span>
                    {p.id === selected ? "当前连接" : "连接"}
                    <Icon name="arrow-right" size={14} />
                  </span>
                </button>
              ))}
            </div>
            <h2>远程连接配置</h2>
            <label>
              选择配置
              <select
                value={editing}
                onChange={(e) => setEditing(e.target.value)}
              >
                {profiles
                  .filter((p) => p.kind === "remote")
                  .map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                <option value="custom">新增连接</option>
              </select>
            </label>
            <div className="settings-fields">
              <label>
                名称
                <input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  autoComplete="off"
                />
              </label>
              <label>
                服务地址
                <input
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  placeholder="http://192.168.5.16:8080"
                  spellCheck={false}
                />
              </label>
              <label>
                API 令牌
                <input
                  type="password"
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  autoComplete="new-password"
                  placeholder={
                    profiles.find((p) => p.id === editing)?.hasToken
                      ? "已保存，留空保留现有令牌"
                      : "填写 EA_SERVER_API_KEY（兼容 EA_API_KEY）"
                  }
                />
              </label>
            </div>
            <p className="settings-note">
              {window.piAPI
                ? "令牌由系统安全存储加密，只保存在桌面主进程中。"
                : "预览模式的令牌仅保留在当前页面内存，刷新后需重新输入。"}
            </p>
            <button
              className="btn primary"
              disabled={busy || !url.trim()}
              onClick={async () => {
                setBusy(true);
                setNotice("");
                try {
                  const id =
                    editing === "custom" ? "remote-" + Date.now() : editing;
                  await useStore
                    .getState()
                    .saveProfile({ id, name, url, token: token || undefined });
                  setToken("");
                  await useStore
                    .getState()
                    .connectProfile(window.piAPI ? id : "browser");
                  setEditing(id);
                  setNotice("连接配置已保存");
                } catch (error) {
                  setNotice((error as Error).message);
                } finally {
                  setBusy(false);
                }
              }}
            >
              {busy ? "正在保存…" : "保存并连接"}
            </button>
            <h2>桌面版本</h2>
            <p>EasyAgent {__APP_VERSION__}</p>
            {window.piAPI && (
              <button
                className="btn"
                onClick={() => void useStore.getState().checkUpdate()}
              >
                检查桌面更新
              </button>
            )}
            {update?.phase === "idle" && (
              <p role="status">当前没有可安装的桌面更新</p>
            )}
            <h2>界面</h2>
            <div className="settings-fields">
              <label>
                主题
                <select
                  value={theme}
                  onChange={(e) =>
                    useStore
                      .getState()
                      .setTheme(e.target.value as "dark" | "light" | "system")
                  }
                >
                  <option value="dark">青夜</option>
                  <option value="light">浅色</option>
                  <option value="system">跟随系统</option>
                </select>
              </label>
              <label>
                语言
                <select
                  value={lang}
                  onChange={(e) =>
                    useStore.getState().setLang(e.target.value as "zh" | "en")
                  }
                >
                  <option value="zh">中文</option>
                  <option value="en">English</option>
                </select>
              </label>
            </div>
          </section>
        ) : (
          <section className="settings-section">
            <div className="settings-section-title">
              <div>
                <h2>MCP 工具连接</h2>
                <p>
                  连接配置在当前服务主机保存。项目配置需显式信任后才可执行。
                </p>
              </div>
              <button
                className="btn"
                disabled={loading || busy}
                onClick={() => void load()}
              >
                <Icon name="refresh" size={14} />
                刷新
              </button>
            </div>
            <label>
              项目工作区
              <div className="field-action">
                <input
                  value={workspace}
                  onChange={(e) => setWorkspace(e.target.value)}
                  placeholder="服务主机上的绝对路径"
                />
                <button
                  className="btn"
                  disabled={busy}
                  onClick={() => void load()}
                >
                  读取
                </button>
              </div>
            </label>
            {list && (
              <>
                <div className="project-trust">
                  <span>
                    项目配置 · {list.project_trusted ? "已信任" : "未信任"}
                  </span>
                  <button
                    className="btn"
                    disabled={busy || !workspace}
                    onClick={async () => {
                      setBusy(true);
                      try {
                        await apiRequest("PUT", "/mcp/project-trust", {
                          workspace,
                          trusted: !list.project_trusted,
                        });
                        await load();
                      } catch (error) {
                        setNotice((error as Error).message);
                      } finally {
                        setBusy(false);
                      }
                    }}
                  >
                    {list.project_trusted ? "撤销信任" : "信任此项目"}
                  </button>
                </div>
                {list.issues?.length > 0 && (
                  <div className="inline-error">
                    {list.issues.map((issue, i) => (
                      <p key={i}>
                        {typeof issue === "string"
                          ? issue
                          : JSON.stringify(issue)}
                      </p>
                    ))}
                  </div>
                )}
                {!list.servers.length && (
                  <div className="settings-empty">
                    <h3>接入你的第一个工具服务</h3>
                    <p>
                      下方填写名称与标准 MCP 配置。可接入本机命令，也可连接支持
                      Streamable HTTP 的远程服务。
                    </p>
                  </div>
                )}
                {list.servers.map((server) => (
                  <details
                    className="mcp-server"
                    key={server.scope + server.name}
                  >
                    <summary>
                      <strong>{server.name}</strong>
                      <span>
                        {server.scope === "project" ? "项目" : "个人"} ·{" "}
                        {server.transport}
                      </span>
                      <span className={"mcp-state " + server.state}>
                        {server.state} · {server.tool_count} 个工具
                      </span>
                    </summary>
                    {server.error && (
                      <p className="inline-error">{server.error}</p>
                    )}
                    <div className="mcp-actions">
                      <button
                        className="btn"
                        disabled={busy}
                        onClick={() => {
                          setMcpName(server.name);
                          setScope(server.scope);
                          setConfiguration("");
                          requestAnimationFrame(() =>
                            document
                              .querySelector<HTMLTextAreaElement>(
                                ".config-input",
                              )
                              ?.focus(),
                          );
                        }}
                      >
                        修改配置
                      </button>
                      {[
                        "reconnect",
                        "refresh",
                        server.enabled ? "disable" : "enable",
                        server.trusted ? "untrust" : "trust",
                        ...(server.transport === "stdio"
                          ? []
                          : ["login", "logout"]),
                      ].map((op) => (
                        <button
                          className="btn"
                          key={op}
                          disabled={busy}
                          onClick={() => void action(server, op)}
                        >
                          {
                            {
                              reconnect: "重新连接",
                              refresh: "刷新工具",
                              disable: "停用",
                              enable: "启用",
                              untrust: "撤销信任",
                              trust: "信任服务",
                              login: "授权登录",
                              logout: "退出授权",
                            }[op]
                          }
                        </button>
                      ))}
                      <button
                        className="btn danger"
                        disabled={busy}
                        onClick={() => {
                          if (
                            window.confirm(`删除 MCP 配置「${server.name}」？`)
                          )
                            void action(server, "delete");
                        }}
                      >
                        删除
                      </button>
                    </div>
                    <div className="mcp-tools">
                      {server.tools?.map((tool) => (
                        <div key={tool.name}>
                          <strong>{tool.name}</strong>
                          <label className="mcp-tool-toggle">
                            <input
                              type="checkbox"
                              checked={tool.enabled !== false}
                              disabled={busy}
                              onChange={async (e) => {
                                const enabled = e.target.checked;
                                setBusy(true);
                                try {
                                  await apiRequest(
                                    "POST",
                                    `/mcp/servers/${encodeURIComponent(server.name)}/tools/${encodeURIComponent(tool.name)}?workspace=${encodeURIComponent(workspace)}&scope=${server.scope}`,
                                    { enabled },
                                  );
                                  await load();
                                } catch (error) {
                                  setNotice((error as Error).message);
                                } finally {
                                  setBusy(false);
                                }
                              }}
                            />
                            {tool.enabled !== false ? "已启用" : "已停用"}
                          </label>
                          <p>{tool.description}</p>
                        </div>
                      ))}
                    </div>
                  </details>
                ))}
              </>
            )}
            <h2>添加或更新配置</h2>
            <div className="settings-fields">
              <label>
                服务名称
                <input
                  value={mcpName}
                  onChange={(e) => setMcpName(e.target.value)}
                  placeholder="filesystem"
                />
              </label>
              <label>
                配置范围
                <select
                  value={scope}
                  onChange={(e) => setScope(e.target.value)}
                >
                  <option value="user">个人</option>
                  <option value="project">当前项目</option>
                </select>
              </label>
            </div>
            <label>
              标准 MCP 配置
              <textarea
                className="config-input"
                value={configuration}
                onChange={(e) => setConfiguration(e.target.value)}
                spellCheck={false}
                placeholder={`{
  "command": "npx",
  "args": ["-y", "@modelcontextprotocol/server-filesystem", "/your/project"],
  "trust": true
}`}
              />
            </label>
            <p className="settings-note">
              保存不会自动信任来源不明的项目。现有字段未填写时保留；密钥不会回显，需要更改时填写新值。
            </p>
            <button
              className="btn primary"
              disabled={
                busy ||
                !mcpName.trim() ||
                !configuration.trim() ||
                (scope === "project" && !workspace)
              }
              onClick={async () => {
                setBusy(true);
                try {
                  const config = JSON.parse(configuration);
                  if (
                    !config ||
                    typeof config !== "object" ||
                    Array.isArray(config)
                  )
                    throw new Error("配置须为 JSON 对象");
                  await apiRequest(
                    "PUT",
                    `/mcp/servers/${encodeURIComponent(mcpName.trim())}${params}`,
                    config,
                  );
                  setConfiguration("");
                  await load();
                  setNotice("MCP 配置已保存");
                } catch (error) {
                  setNotice((error as Error).message);
                } finally {
                  setBusy(false);
                }
              }}
            >
              保存配置
            </button>
          </section>
        )}
        {notice && (
          <p className="settings-notice" role="status">
            {notice}
          </p>
        )}
      </div>
    </main>
  );
}
