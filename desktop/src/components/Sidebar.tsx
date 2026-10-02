import { useEffect, useMemo, useState } from "react";
import { Capacitor } from "@capacitor/core";
import { useStore, type SessionView } from "../store";
import { Icon } from "./Icon";
import { useT, type TFunc } from "../i18n/useT";
import { isElectron } from "../platform";
import { checkMobileUpdate, getAppVersion } from "../mobile-updater";
import appIcon from "../assets/app-icon.png";

function projectName(cwd: string): string {
  const parts = cwd.replace(/[\\/]+$/, "").split(/[\\/]/);
  return parts[parts.length - 1] || cwd;
}

interface ProjectGroup {
  cwd: string;
  name: string;
  views: SessionView[];
}

export function Sidebar() {
  const order = useStore((s) => s.order);
  const sessions = useStore((s) => s.sessions);
  const active = useStore((s) => s.activeSessionId);
  const setActive = useStore((s) => s.setActive);
  const createSession = useStore((s) => s.createSession);
  const deleteSession = useStore((s) => s.deleteSession);
  const refreshSessions = useStore((s) => s.refreshSessions);
  const pickFolder = useStore((s) => s.pickFolder);
  const t = useT();

  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [filter, setFilter] = useState("");
  const [showNewMenu, setShowNewMenu] = useState(false);

  const toggleCollapse = (key: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  // Dismiss the new-session mode menu on outside click / Escape.
  useEffect(() => {
    if (!showNewMenu) return;
    const onDown = (e: MouseEvent) => {
      const target = e.target as HTMLElement;
      if (!target.closest(".new-session-wrap")) setShowNewMenu(false);
    };
    const onKey = (e: KeyboardEvent) =>
      e.key === "Escape" && setShowNewMenu(false);
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [showNewMenu]);

  // Refresh session list periodically (every 10s) to catch backend-side changes
  useEffect(() => {
    const timer = setInterval(() => void refreshSessions(), 10000);
    return () => clearInterval(timer);
  }, [refreshSessions]);

  const { chats, projects } = useMemo(() => {
    const views = order
      .map((id) => sessions[id])
      .filter(Boolean) as SessionView[];
    const filtered = filter
      ? views.filter((v) =>
          v.meta.title.toLowerCase().includes(filter.toLowerCase()),
        )
      : views;
    const chatViews: SessionView[] = [];
    const projectMap = new Map<string, ProjectGroup>();
    for (const v of filtered) {
      if (!v.meta.cwd) {
        chatViews.push(v);
        continue;
      }
      const cwd = v.meta.cwd;
      let g = projectMap.get(cwd);
      if (!g) {
        g = { cwd, name: projectName(cwd), views: [] };
        projectMap.set(cwd, g);
      }
      g.views.push(v);
    }
    return { chats: chatViews, projects: [...projectMap.values()] };
  }, [order, sessions, filter]);

  const onClick = (id: string) => {
    setActive(id);
  };

  const reportError = (error: unknown) =>
    useStore.setState({
      connectionError: error instanceof Error ? error.message : "操作未完成",
    });

  const handleNew = async (application?: string) => {
    setShowNewMenu(false);
    // If current active session belongs to a project, create new session in same project
    const currentCwd = active ? sessions[active]?.meta.cwd : undefined;
    const opts: { cwd?: string; application?: string } = {};
    if (currentCwd) opts.cwd = currentCwd;
    if (application) opts.application = application;
    await createSession(Object.keys(opts).length > 0 ? opts : undefined).catch(
      reportError,
    );
  };

  const handleNewInProject = async (cwd: string) => {
    await createSession({ cwd }).catch(reportError);
  };

  const handleNewProject = async () => {
    try {
      const folder = await pickFolder();
      if (folder) await createSession({ cwd: folder });
    } catch (error) {
      reportError(error);
    }
  };

  const handleDelete = async (id: string) => {
    await deleteSession(id).catch(reportError);
  };

  const renderCard = (v: SessionView, nested: boolean) => (
    <div
      key={v.meta.id}
      className={`session-item ${nested ? "nested" : ""} ${active === v.meta.id ? "active" : ""}`}
    >
      <button
        className="session-open"
        onClick={() => onClick(v.meta.id)}
        aria-current={active === v.meta.id ? "page" : undefined}
      >
        <div className="session-row">
          <span className={`status-dot ${v.meta.status}`} />
          <>
            <span className="session-title" title={v.meta.title}>
              {v.meta.title}
            </span>
            {v.meta.application && (
              <span className={`session-badge ${v.meta.application}`}>
                {v.meta.application}
              </span>
            )}
          </>
        </div>
        <div className="session-row">
          <span className="session-sub">{relTime(t, v.meta.updatedAt)}</span>
        </div>
      </button>
      <div className="session-actions">
        <button
          className="icon-btn"
          title={t("common.delete")}
          aria-label={t("common.delete") + " " + v.meta.title}
          onClick={(e) => {
            e.stopPropagation();
            void handleDelete(v.meta.id);
          }}
        >
          <Icon name="x" size={14} />
        </button>
      </div>
    </div>
  );

  const isEmpty = chats.length === 0 && projects.length === 0;

  return (
    <aside className="sidebar">
      <div className="sidebar-head">
        <div className="brand">
          <img className="brand-mark" src={appIcon} alt="EasyAgent" />
          EasyAgent
        </div>
        <div className="sidebar-head-actions">
          <div className="new-session-wrap">
            <button className="btn-new" onClick={() => void handleNew()}>
              <Icon name="plus" size={15} />
              {t("sidebar.newSession")}
            </button>
            <button
              className="btn-new-dropdown"
              onClick={() => setShowNewMenu(!showNewMenu)}
              title={t("mode.chooseMode")}
            >
              <Icon name="chevron-down" size={12} />
            </button>
            {showNewMenu && (
              <div className="new-session-menu">
                <button
                  className="new-session-option"
                  onClick={() => void handleNew()}
                >
                  <Icon name="cpu" size={14} />
                  {t("mode.coding")}
                </button>
                <button
                  className="new-session-option"
                  onClick={() => void handleNew("music")}
                >
                  <Icon name="sparkle" size={14} />
                  {t("mode.music")}
                </button>
                <button
                  className="new-session-option"
                  onClick={() => void handleNew("kb")}
                >
                  <Icon name="search" size={14} />
                  {t("mode.kb")}
                </button>
              </div>
            )}
          </div>
          <button
            className="btn-new-project"
            onClick={() => void handleNewProject()}
            title={t("sidebar.newProject") || "New Project"}
          >
            <Icon name="folder-open" size={15} />
          </button>
        </div>
      </div>

      <div className="sidebar-search">
        <Icon name="search" size={14} />
        <input
          placeholder={t("sidebar.searchPlaceholder")}
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
      </div>

      <div className="session-list">
        {isEmpty && (
          <div className="group-label">{t("sidebar.noSessions")}</div>
        )}

        {chats.length > 0 && (
          <div className="sidebar-section">
            <div className="group-label">{t("sidebar.chats") || "Chats"}</div>
            <div className="project-group">
              <button
                className="project-header"
                onClick={() => toggleCollapse("__chats__")}
              >
                <Icon
                  name={
                    collapsed.has("__chats__")
                      ? "chevron-right"
                      : "chevron-down"
                  }
                  size={14}
                />
                <Icon name="folder" size={14} />
                <span className="project-name">
                  {t("sidebar.chatsFolder") || "Chats"}
                </span>
                <span className="project-count">{chats.length}</span>
              </button>
              {!collapsed.has("__chats__") &&
                chats.map((v) => renderCard(v, true))}
            </div>
          </div>
        )}

        {projects.length > 0 && (
          <div className="sidebar-section">
            <div className="group-label">
              {t("sidebar.projects") || "Projects"}
            </div>
            {projects.map((g) => (
              <div key={g.cwd} className="project-group">
                <div className="project-header-row">
                  <button
                    className="project-header"
                    title={g.cwd}
                    onClick={() => toggleCollapse(g.cwd)}
                  >
                    <Icon
                      name={
                        collapsed.has(g.cwd) ? "chevron-right" : "chevron-down"
                      }
                      size={14}
                    />
                    <Icon name="folder" size={14} />
                    <span className="project-name">{g.name}</span>
                    <span className="project-count">{g.views.length}</span>
                  </button>
                  <button
                    className="project-new-btn"
                    title={
                      t("sidebar.newInProject") || "New conversation in project"
                    }
                    onClick={(e) => {
                      e.stopPropagation();
                      void handleNewInProject(g.cwd);
                    }}
                  >
                    <Icon name="plus" size={12} />
                  </button>
                </div>
                {!collapsed.has(g.cwd) &&
                  g.views.map((v) => renderCard(v, true))}
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="sidebar-foot">
        <span className="avatar">
          <Icon name="cpu" size={14} />
        </span>
        <span
          style={{
            flex: 1,
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          EasyAgent
        </span>
        {/* Mobile: check-for-update button */}
        {Capacitor.isNativePlatform() && (
          <button
            className="icon-btn sidebar-check-update"
            title="检查更新"
            onClick={async () => {
              const btn = document.querySelector(
                ".sidebar-check-update",
              ) as HTMLButtonElement;
              if (btn) {
                btn.textContent = "⋯";
                btn.disabled = true;
              }
              try {
                const ver = await getAppVersion();
                const info = await checkMobileUpdate();
                if (info) {
                  window.dispatchEvent(
                    new CustomEvent("pi-go-show-update", { detail: info }),
                  );
                } else {
                  alert(
                    `当前版本 v${ver}\nGitHub 最新版检查完毕，可能已是最新或网络受限。\n如需手动更新，请用浏览器访问:\ngithub.com/hwj123hwj/easyagent/releases`,
                  );
                }
              } catch (e) {
                alert(
                  "检查更新失败: " +
                    (e instanceof Error ? e.message : "未知错误") +
                    "\n请用浏览器访问 github.com/hwj123hwj/easyagent/releases",
                );
              }
              if (btn) {
                btn.textContent = "↻";
                btn.disabled = false;
              }
            }}
          >
            <Icon name="refresh" size={15} />
          </button>
        )}
        {isElectron && (
          <button
            className="icon-btn"
            title={t("sidebar.newProject") || "New Project"}
            onClick={() => void handleNewProject()}
          >
            <Icon name="folder-open" size={15} />
          </button>
        )}
      </div>
    </aside>
  );
}

function relTime(t: TFunc, ts: number): string {
  if (!ts) return "";
  const diff = Date.now() - ts;
  const m = Math.floor(diff / 60000);
  if (m < 1) return t("time.justNow");
  if (m < 60) return t("time.minutesAgo", { m });
  const h = Math.floor(m / 60);
  if (h < 24) return t("time.hoursAgo", { h });
  return t("time.daysAgo", { d: Math.floor(h / 24) });
}
