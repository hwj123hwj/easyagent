import { useStore } from "../store";
import { Icon } from "./Icon";
import { HostPicker } from "./HostPicker";

export function ConnectionBar() {
  const profiles = useStore((s) => s.profiles),
    selected = useStore((s) => s.selectedProfile),
    connected = useStore((s) => s.connected),
    state = useStore((s) => s.connectionState),
    error = useStore((s) => s.connectionError),
    settings = useStore((s) => s.settingsOpen),
    workspace = useStore((s) => s.workspace),
    activeId = useStore((s) => s.activeSessionId),
    sessions = useStore((s) => s.sessions);

  const profile = profiles.find((p) => p.id === selected);
  const activeSession = activeId ? sessions[activeId] : undefined;
  const currentTitle = activeSession?.meta?.title;

  const connectionLabel = connected
    ? profile?.kind === "remote"
      ? "远程已连接"
      : "本地已连接"
    : state === "connecting"
      ? "正在连接"
      : state === "error"
        ? "连接失败"
        : "正在重连";

  return (
    <header className="connection-bar">
      {/* ── Left Area: Sidebar Toggle & Brand ── */}
      <div className="connection-bar-left">
        {!settings && (
          <button
            className="icon-btn connection-sidebar-toggle"
            aria-label={workspace.sidebarOpen ? "收起会话列表" : "展开会话列表"}
            aria-expanded={workspace.sidebarOpen}
            aria-controls="session-sidebar"
            title={workspace.sidebarOpen ? "收起会话列表" : "展开会话列表"}
            onClick={() => useStore.getState().toggleSidebar()}
          >
            <Icon name="panel" size={17} />
          </button>
        )}
        <span className="connection-brand" title="EasyAgent">
          <b>ea·</b>
        </span>
      </div>

      {/* ── Center Area: Active Session Context ── */}
      <div className="connection-bar-center">
        {currentTitle && (
          <span className="connection-session-title" title={currentTitle}>
            {activeSession?.meta?.forked_from && (
              <span className="connection-session-fork" title="分叉会话">
                <Icon name="git-branch" size={12} />
              </span>
            )}
            {currentTitle}
          </span>
        )}
      </div>

      {/* ── Right Area: Host Status & Controls ── */}
      <div className="connection-bar-right">
        {!connected && state !== "connecting" && (
          <button
            className="btn btn-sm connection-reconnect"
            onClick={() => void useStore.getState().connectProfile(selected)}
          >
            重新连接
          </button>
        )}
        <div className="connection-host-capsule">
          <HostPicker />
          <span className="connection-caption" role="status">
            {connectionLabel}
          </span>
        </div>
        {!settings && window.piAPI && (
          <button
            className={
              "icon-btn connection-terminal " +
              (workspace.bottomOpen ? "active" : "")
            }
            aria-label={workspace.bottomOpen ? "收起终端" : "打开终端"}
            aria-pressed={workspace.bottomOpen}
            title={workspace.bottomOpen ? "收起终端" : "打开终端"}
            onClick={() => useStore.getState().toggleWorkspaceBottom()}
          >
            <Icon name="terminal" size={16} />
          </button>
        )}
        {!settings && (
          <button
            className={
              "icon-btn connection-workbench " +
              (workspace.rightOpen ? "active" : "")
            }
            aria-label={workspace.rightOpen ? "收起工作台" : "打开工作台"}
            aria-pressed={workspace.rightOpen}
            title={workspace.rightOpen ? "收起工作台" : "打开工作台"}
            onClick={() => useStore.getState().toggleWorkspaceRight()}
          >
            <Icon name="panel-right" size={16} />
          </button>
        )}
        {!settings && !workspace.sidebarOpen && (
          <button
            className="icon-btn connection-settings"
            aria-label="设置"
            title="设置"
            onClick={() => useStore.getState().openSettings(true)}
          >
            <Icon name="settings" size={15} />
          </button>
        )}
      </div>

      {error && (
        <div className="connection-notice" role="alert">
          <span>{error}</span>
          <button
            aria-label="关闭提示"
            onClick={() => useStore.setState({ connectionError: undefined })}
          >
            <Icon name="x" size={13} />
          </button>
        </div>
      )}
    </header>
  );
}
