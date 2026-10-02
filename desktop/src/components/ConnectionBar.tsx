import { useStore } from "../store";
import { Icon } from "./Icon";
export function ConnectionBar() {
  const profiles = useStore((s) => s.profiles),
    selected = useStore((s) => s.selectedProfile),
    connected = useStore((s) => s.connected),
    state = useStore((s) => s.connectionState),
    error = useStore((s) => s.connectionError),
    settings = useStore((s) => s.settingsOpen);
  const profile = profiles.find((p) => p.id === selected);
  return (
    <header className="connection-bar">
      <span className="connection-brand">
        <b>ea·</b> 个人工作区
      </span>
      <label className="connection-picker">
        <span
          className={"connection-dot " + (connected ? "online" : "")}
          aria-hidden="true"
        />
        <span className="sr-only">运行主机</span>
        <select
          aria-label="选择运行主机"
          value={selected}
          disabled={state === "connecting"}
          onChange={(e) =>
            void useStore.getState().connectProfile(e.target.value)
          }
        >
          {profiles.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
      </label>
      <span className="connection-caption" role="status">
        {connected
          ? profile?.kind === "remote"
            ? "远程 Agent 已连接"
            : "本地 Agent 已连接"
          : state === "connecting"
            ? "正在连接服务"
            : state === "error"
              ? "连接失败"
              : "连接已断开，正在重连"}
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
      <button
        className={"btn connection-settings " + (settings ? "active" : "")}
        aria-pressed={settings}
        onClick={() => useStore.getState().openSettings(!settings)}
      >
        <Icon name="settings" size={15} />
        设置
      </button>
      {error && (
        <div className="connection-notice" role="alert">
          {error}
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
