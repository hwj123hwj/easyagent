import { ComputerUseSettings } from "./ComputerUseSettings";
import { UsageSettings } from "./UsageSettings";
import { useEffect, useRef, useState } from "react";
import { apiRequest, useStore, type SettingsTab } from "../store";
import { Icon } from "./Icon";
import { MCPConfigEditor } from "./MCPConfigEditor";
import { CapabilitySettings } from "./CapabilitySettings";
import { ModelSettings } from "./ModelSettings";
import { FeishuSettings } from "./FeishuSettings";
import { SkillMarketPanel } from "./SkillMarketPanel";
import { type IconName } from "./Icon";

const settingGroups: Array<{
  title: string;
  items: Array<{
    id: SettingsTab;
    label: string;
    icon: IconName;
    description: string;
  }>;
}> = [
  {
    title: "基础设置",
    items: [
      {
        id: "general",
        label: "常规",
        icon: "settings",
        description: "设置界面语言，查看版本与常用快捷键。",
      },
      {
        id: "appearance",
        label: "外观",
        icon: "sparkle",
        description: "选择适合你的界面配色。",
      },
      {
        id: "models",
        label: "模型连接",
        icon: "cpu",
        description: "连接模型服务，选择默认模型。",
      },
      {
        id: "usage",
        label: "用量统计",
        icon: "sparkle",
        description: "查看近 30 天 Token 消耗与各会话用量排行。",
      },
      {
        id: "skillMarket",
        label: "技能市场",
        icon: "sparkle",
        description: "浏览并安装官方技能市场里的技能。",
      },
      {
        id: "experiments",
        label: "实验",
        icon: "sparkle",
        description: "实验性功能开关：Computer use 等能力。",
      },
    ],
  },
  {
    title: "连接与能力",
    items: [
      {
        id: "connections",
        label: "运行主机",
        icon: "laptop",
        description: "选择任务运行的位置，管理远程 Agent 连接。",
      },
      {
        id: "mcp",
        label: "MCP 工具",
        icon: "wrench",
        description: "为当前主机接入工具服务，管理信任与授权。",
      },
      {
        id: "capabilities",
        label: "技能与工作流",
        icon: "sparkle",
        description: "查看当前项目实际加载的能力与工作流运行记录。",
      },
      {
        id: "feishu",
        label: "飞书机器人",
        icon: "feishu",
        description: "配置应用凭据、查看桥接状态，并完成私聊配对。",
      },
    ],
  },
];
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
    setTab = (tab: SettingsTab) => useStore.getState().openSettings(true, tab);
  const connected = useStore((s) => s.connected);
  const page = settingGroups
    .flatMap((g) => g.items)
    .find((item) => item.id === tab)!;
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.focus();
    setNotice("");
  }, [tab]);
  useEffect(
    () => () => {
      requestAnimationFrame(() => {
        const close = document.querySelector<HTMLButtonElement>(
          '.workspace-right[role="dialog"]:not([hidden]) .rsidebar-mobile-close',
        );
        if (close?.getClientRects().length) close.focus();
        else
          document
            .querySelector<HTMLButtonElement>('.sidebar-foot [aria-label="设置"], .connection-settings')
            ?.focus();
      });
    },
    [],
  );
  const [editing, setEditing] = useState("mini"),
    [name, setName] = useState("迷你主机"),
    [url, setUrl] = useState("http://192.168.5.16:8080"),
    [token, setToken] = useState(""),
    [notice, setNotice] = useState(""),
    [busy, setBusy] = useState(false);
  const [workspace, setWorkspace] = useState(cwd || ""),
    [list, setList] = useState<MCPList | null>(null),
    [mcpName, setMcpName] = useState(""),
    [editorRevision, setEditorRevision] = useState(0),
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
    setEditorRevision((x) => x + 1);
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
    <main className="settings-workspace" aria-label="设置">
      <aside className="settings-navigation">
        <button
          className="settings-return"
          onClick={() => useStore.getState().openSettings(false)}
        >
          <Icon name="arrow-left" size={17} />
          返回工作区
        </button>
        <nav aria-label="设置分类">
          {settingGroups.map((group) => (
            <div className="settings-nav-group" key={group.title}>
              <p>{group.title}</p>
              {group.items.map((item) => (
                <button
                  key={item.id}
                  aria-current={tab === item.id ? "page" : undefined}
                  onClick={() => setTab(item.id)}
                >
                  <Icon name={item.icon} size={18} />
                  <span>{item.label}</span>
                </button>
              ))}
            </div>
          ))}
        </nav>
        <div className="settings-nav-foot">
          <span className="brand-word">ea·</span>
          <span>
            EasyAgent<small>{__APP_VERSION__}</small>
          </span>
        </div>
      </aside>
      <div className="settings-content" key={selected}>
        <header className="settings-page-heading">
          <h1 ref={heading} tabIndex={-1}>
            {page.label}
          </h1>
          <p>{page.description}</p>
          {["connections", "models", "mcp", "feishu"].includes(tab) && (
            <div className="settings-host-context">
              <Icon
                name={
                  profiles.find((p) => p.id === selected)?.kind === "local"
                    ? "laptop"
                    : "globe"
                }
                size={14}
              />
              {profiles.find((p) => p.id === selected)?.name || "当前主机"}
              <span>{connected ? "已连接" : "未连接"}</span>
            </div>
          )}
        </header>
        <div className="settings-scroll">
          {tab === "general" ? (
            <section className="settings-section">
              <div className="settings-row-group">
                <label className="settings-preference-row">
                  <span>
                    <strong>界面语言</strong>
                    <small>选择应用的显示语言。</small>
                  </span>
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
                <div className="settings-preference-row">
                  <span>
                    <strong>桌面版本</strong>
                    <small>EasyAgent {__APP_VERSION__}</small>
                  </span>
                  {window.piAPI && (
                    <button
                      className="btn"
                      disabled={update?.phase === "checking"}
                      onClick={() => void useStore.getState().checkUpdate()}
                    >
                      {update?.phase === "checking" ? "正在检查…" : "检查更新"}
                    </button>
                  )}
                </div>
              </div>
              {update?.phase === "idle" && (
                <p className="settings-note" role="status">
                  当前没有可安装的桌面更新
                </p>
              )}
              <h2>常用快捷键</h2>
              <dl className="settings-shortcuts">
                <div>
                  <dt>发送消息</dt>
                  <dd>
                    <kbd>Enter</kbd>
                  </dd>
                </div>
                <div>
                  <dt>输入换行</dt>
                  <dd>
                    <kbd>Shift</kbd> + <kbd>Enter</kbd>
                  </dd>
                </div>
                <div>
                  <dt>打开命令菜单</dt>
                  <dd>
                    <kbd>/</kbd>
                  </dd>
                </div>
                <div>
                  <dt>文件工作台</dt>
                  <dd>
                    <kbd>⌘ / Ctrl</kbd> + <kbd>P</kbd>
                  </dd>
                </div>
                <div>
                  <dt>代码审查</dt>
                  <dd>
                    <kbd>⌘ / Ctrl</kbd> + <kbd>Shift</kbd> + <kbd>G</kbd>
                  </dd>
                </div>
              </dl>
            </section>
          ) : tab === "appearance" ? (
            <section className="settings-section">
              <h2>配色主题</h2>
              <p>立即应用到当前界面，选择会保留在此设备。</p>
              <div
                className="settings-themes"
                role="group"
                aria-label="配色主题"
              >
                {(
                  [
                    {
                      id: "dark",
                      label: "青夜",
                      detail: "深青背景 · 柔和浅青",
                    },
                    {
                      id: "light",
                      label: "浅色",
                      detail: "明亮表面 · 清晰文字",
                    },
                    {
                      id: "system",
                      label: "跟随系统",
                      detail: "随系统外观自动切换",
                    },
                  ] as const
                ).map((option) => (
                  <button
                    key={option.id}
                    className={"settings-theme-choice " + option.id}
                    aria-pressed={theme === option.id}
                    onClick={() => useStore.getState().setTheme(option.id)}
                  >
                    <span className="settings-theme-preview" aria-hidden="true">
                      <i />
                      <b>
                        <em />
                        <em />
                      </b>
                    </span>
                    <span>
                      <strong>{option.label}</strong>
                      {theme === option.id && (
                        <Icon name="circle-check" size={17} />
                      )}
                    </span>
                    <small>{option.detail}</small>
                  </button>
                ))}
              </div>
            </section>
          ) : tab === "usage" ? (
            <UsageSettings />
          ) : tab === "skillMarket" ? (
            <SkillMarketPanel />
          ) : tab === "experiments" ? (
            <ComputerUseSettings />
          ) : tab === "capabilities" ? (
            <CapabilitySettings />
          ) : tab === "feishu" ? (
            <FeishuSettings />
          ) : tab === "connections" ? (
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
                    onClick={() =>
                      void useStore.getState().connectProfile(p.id)
                    }
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
                      .saveProfile({
                        id,
                        name,
                        url,
                        token: token || undefined,
                      });
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
            </section>
          ) : tab === "models" ? (
            <ModelSettings />
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
                        下方填写名称与标准 MCP
                        配置。可接入本机命令，也可连接支持 Streamable HTTP
                        的远程服务。
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
                            setEditorRevision((x) => x + 1);
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
                              window.confirm(
                                `删除 MCP 配置「${server.name}」？`,
                              )
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
              <MCPConfigEditor
                key={scope + editorRevision}
                value={configuration}
                onChange={setConfiguration}
              />
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
                    setEditorRevision((x) => x + 1);
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
      </div>
    </main>
  );
}
