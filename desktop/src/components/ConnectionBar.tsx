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
    workspace = useStore((s) => s.workspace);
  const profile = profiles.find((p) => p.id === selected);
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
      {!settings && <button
        className="icon-btn connection-sidebar-toggle"
        aria-label={workspace.sidebarOpen ? "收起会话列表" : "展开会话列表"}
        aria-expanded={workspace.sidebarOpen}
        aria-controls="session-sidebar"
        title={workspace.sidebarOpen ? "收起会话列表" : "展开会话列表"}
        onClick={() => useStore.getState().toggleSidebar()}
      >
        <Icon name="panel" size={17} />
      </button>}
      <span className="connection-brand">
        <b>ea·</b><span>EasyAgent</span>
      </span>
      <HostPicker />
      <span className="connection-caption" role="status">
        {connectionLabel}
      </span>
      <span className="grow" />
      {!connected && state !== "connecting" && (
        <button
          className="btn"
          onClick={() => void useStore.getState().connectProfile(selected)}
        >
          重新连接
        </button>
      )}
      {!settings && (
        <button
          className={"icon-btn connection-workbench " + (workspace.rightOpen ? "active" : "")}
          aria-label={workspace.rightOpen ? "收起工作台" : "打开工作台"}
          aria-pressed={workspace.rightOpen}
          title={workspace.rightOpen ? "收起工作台" : "打开工作台"}
          onClick={() => useStore.getState().toggleWorkspaceRight()}
        >
          <Icon name="panel-right" size={17} />
        </button>
      )}
      {!settings && <button
        className="btn connection-settings"
        aria-label="设置"
        onClick={() => useStore.getState().openSettings(true)}
      >
        <Icon name="settings" size={15} />
        <span>设置</span>
      </button>}
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
