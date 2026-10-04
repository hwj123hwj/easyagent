import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { useStore, type SessionView } from "../../store";
import { copyText } from "../../client/clipboard";
import { Icon } from "../Icon";

export function BottomTerminal({ view, height, open }: { view: SessionView; height: number; open: boolean }) {
  const host = useRef<HTMLDivElement>(null);
  const profile = useStore((s) => s.selectedProfile);
  const [state, setState] = useState("正在连接");
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [started, setStarted] = useState(open);
  useEffect(() => { if (open) setStarted(true); }, [open]);

  useEffect(() => {
    const api = window.piAPI;
    if (!host.current || !api || !started) return;
    let disposed = false, ready = false;
    const id = crypto.randomUUID();
    const styles = getComputedStyle(host.current);
    const terminal = new Terminal({
      cursorBlink: true, fontSize: 13, fontFamily: styles.getPropertyValue("--mono").trim() || "monospace",
      scrollback: 5000, allowProposedApi: false,
      theme: { background: styles.getPropertyValue("--bg-sunken").trim(), foreground: styles.getPropertyValue("--text").trim() || "#dce7e3" },
    });
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(host.current);
    terminal.textarea?.setAttribute("aria-label", "终端命令输入");
    fit.fit();
    setState("正在连接"); setError("");
    const send = (message: object) => { if (ready && !disposed) void api.terminalSend(id, message).catch(() => {}); };
    const remove = api.onTerminalEvent((event) => {
      if (event.id !== id || disposed) return;
      if (event.type === "ready") { ready = true; setState("已连接"); fit.fit(); send({ type: "resize", cols: terminal.cols, rows: terminal.rows }); if (host.current?.getClientRects().length) terminal.focus(); }
      if (event.type === "output" && event.data) {
        terminal.write(Uint8Array.from(atob(event.data), (c) => c.charCodeAt(0)), () => send({ type: "ack" }));
      }
      if (event.type === "error") { setError(event.message || "终端连接失败"); setState("连接失败"); }
      if (event.type === "closed") { ready = false; setState("已断开"); }
    });
    const input = terminal.onData((data) => send({ type: "input", data }));
    const resize = terminal.onResize(({ cols, rows }) => send({ type: "resize", cols, rows }));
    terminal.attachCustomKeyEventHandler((event) => {
      if ((event.metaKey || event.ctrlKey && event.shiftKey) && event.key.toLowerCase() === "c" && terminal.hasSelection()) {
        if (event.type === "keydown") void copyText(terminal.getSelection()).catch((e) => setError(e.message));
        return false;
      }
      return true;
    });
    const observer = new ResizeObserver(() => { if (host.current?.clientHeight && host.current?.clientWidth) fit.fit(); });
    observer.observe(host.current);
    void api.terminalOpen(id, view.meta.id, profile, terminal.cols, terminal.rows).catch((e) => {
      if (!disposed) { setError(e.message); setState("连接失败"); }
    });
    return () => {
      disposed = true; observer.disconnect(); input.dispose(); resize.dispose(); remove(); terminal.dispose();
      void api.terminalClose(id).catch(() => {});
    };
  }, [view.meta.id, view.meta.cwd, profile, attempt, started]);

  return (
    <section className="bottom-terminal" style={{ height }} hidden={!open} aria-label="交互式终端" onKeyDown={(event) => event.stopPropagation()}>
      <div className="bottom-terminal-head">
        <Icon name="terminal" size={14} /><span>终端</span>
        <span className="terminal-cwd" title={view.meta.cwd}>{view.meta.cwd}</span>
        <span className="grow" /><span role="status">{state}</span>
        {state !== "已连接" && <button className="icon-btn" aria-label="重新连接终端" title="重新连接终端" onClick={() => setAttempt((n) => n + 1)}><Icon name="refresh" size={14} /></button>}
        <button className="icon-btn" aria-label="收起终端" title="收起终端（保持 shell 运行）" onClick={() => useStore.getState().toggleWorkspaceBottom()}><Icon name="x" size={14} /></button>
      </div>
      {error && <div className="terminal-error" role="alert">{error}</div>}
      <div className="interactive-terminal" ref={host} />
    </section>
  );
}
