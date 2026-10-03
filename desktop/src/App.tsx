import { useEffect, useRef, useState } from "react";
import { useStore, type RightView } from "./store";
import { Sidebar } from "./components/Sidebar";
import { SessionView } from "./components/SessionView";
import { UpdateBanner } from "./components/UpdateBanner";
import { ErrorBoundary } from "./components/ErrorBoundary";
import { RightSidebar } from "./components/workspace/RightSidebar";
import { Resizer } from "./components/workspace/Resizer";
import { Icon } from "./components/Icon";
import { useT } from "./i18n/useT";
import { applyTheme } from "./theme";
import { GlobalMusicBar } from "./components/GlobalMusicBar";
import { ServerConnect } from "./components/ServerConnect";
import { MobileUpdateDialog } from "./components/MobileUpdateDialog";
import { isElectron } from "./platform";
import { ConnectionBar } from "./components/ConnectionBar";
import { AgentSettings } from "./components/AgentSettings";
import { WorkspacePathDialog } from "./components/WorkspacePathDialog";

export function App() {
  const init = useStore((s) => s.init);
  const ready = useStore((s) => s.ready);
  const workspace = useStore((s) => s.workspace);
  const setWorkspaceSize = useStore((s) => s.setWorkspaceSize);
  const musicActive = useStore((s) => s.music.current != null);
  const lang = useStore((s) => s.lang);
  const theme = useStore((s) => s.theme);
  const settingsOpen = useStore((s) => s.settingsOpen);
  const toggleSidebar = useStore((s) => s.toggleSidebar);
  const toggleRight = useStore((s) => s.toggleWorkspaceRight);
  const t = useT();
  const [viewportWidth, setViewportWidth] = useState(window.innerWidth);
  const rightPanel = useRef<HTMLDivElement>(null);

  // Stored sizes are preferences; the visible split must still leave room to read.
  const mobile = viewportWidth < 769;
  const sidebarWidth = mobile
    ? workspace.sidebarWidth
    : Math.min(workspace.sidebarWidth, Math.max(200, Math.min(280, viewportWidth * 0.22)));
  const sessionWidth = viewportWidth - (!mobile && workspace.sidebarOpen ? sidebarWidth + 5 : 0);
  const floatingWorkbench = !mobile && workspace.rightOpen && !!workspace.rightView && !settingsOpen && sessionWidth < 845;
  const rightWidth = floatingWorkbench
    ? Math.min(workspace.rightWidth, 520, sessionWidth - 24)
    : Math.min(workspace.rightWidth, Math.max(360, sessionWidth - 485));

  // Auto-reveal collapsed sidebar on hover (VSCode-style) — desktop only
  const [revealed, setRevealed] = useState(false);
  const sidebarOpen = workspace.sidebarOpen && !settingsOpen;
  useEffect(() => {
    setRevealed(false);
  }, [sidebarOpen]);

  useEffect(() => {
    void init();
  }, [init]);

  // Mark body as mobile for CSS targeting
  useEffect(() => {
    const update = () => {
      setViewportWidth(window.innerWidth);
      document.body.classList.toggle("mobile", innerWidth < 769);
    };
    update();
    window.addEventListener("resize", update);
    return () => window.removeEventListener("resize", update);
  }, []);

  useEffect(() => {
    if (!floatingWorkbench || !ready) return;
    const previousFocus = document.activeElement as HTMLElement | null;
    const panel = rightPanel.current;
    panel?.querySelector<HTMLButtonElement>(".rsidebar-mobile-close")?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !event.defaultPrevented) {
        event.preventDefault();
        toggleRight();
      }
      if (event.key !== "Tab" || event.defaultPrevented || !panel) return;
      const controls = [...panel.querySelectorAll<HTMLElement>(
        'button:not(:disabled), a[href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex="0"]',
      )].filter((control) => control.getClientRects().length > 0);
      const first = controls[0], last = controls[controls.length - 1];
      const active = document.activeElement as HTMLElement | null;
      if (!first) {
        event.preventDefault();
        panel.focus();
      } else if (!active || !controls.includes(active)) {
        // Switching panes can hide/remove the focused control and leave focus on body.
        event.preventDefault();
        (event.shiftKey ? last : first).focus();
      } else if (event.shiftKey && active === first) {
        event.preventDefault();
        last?.focus();
      } else if (!event.shiftKey && active === last) {
        event.preventDefault();
        first?.focus();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      if (previousFocus?.isConnected && previousFocus.getClientRects().length > 0)
        previousFocus.focus();
      else document.querySelector<HTMLButtonElement>(".connection-workbench")?.focus();
    };
  }, [floatingWorkbench, ready, toggleRight]);

  // Global workspace shortcuts
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement | null;
      const typing =
        el &&
        (el.tagName === "INPUT" ||
          el.tagName === "TEXTAREA" ||
          el.isContentEditable);
      const mod = e.ctrlKey || e.metaKey;
      if (!mod || useStore.getState().settingsOpen) return;
      let view: RightView | null = null;
      if (e.shiftKey && (e.key === "G" || e.key === "g")) view = "review";
      else if (!e.shiftKey && !e.altKey && (e.key === "p" || e.key === "P"))
        view = "files";
      else if (!e.shiftKey && !e.altKey && e.key === "`") {
        e.preventDefault();
        useStore.getState().toggleWorkspaceBottom();
        return;
      }
      if (view) {
        if (typing && view === "files") return;
        e.preventDefault();
        useStore.getState().openWorkspaceView(view);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  useEffect(() => {
    document.documentElement.lang = lang === "zh" ? "zh-CN" : "en";
  }, [lang]);

  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  if (!ready) {
    return (
      <div className="boot">
        <div className="boot-inner">
          <span
            className="brand-mark"
            style={{ width: 40, height: 40, borderRadius: 12 }}
          >
            <Icon name="sparkle" size={20} />
          </span>
          <span className="spinner" />
          <span>{t("app.booting")}</span>
        </div>
      </div>
    );
  }

  return (
    <ErrorBoundary label="app">
      <div className="desktop-shell">
        <ConnectionBar />
        <div
          className={`app ${sidebarOpen ? "" : "sidebar-collapsed"} ${musicActive && !settingsOpen ? "music-active" : ""} ${settingsOpen ? "settings-active" : ""} ${floatingWorkbench ? "workbench-floating" : ""}`}
          style={
            {
              "--sidebar-w": `${sidebarWidth}px`,
              "--display-sidebar-w": `${sidebarWidth}px`,
              "--workbench-w": `${rightWidth}px`,
            } as React.CSSProperties
          }
        >
          {sidebarOpen ? (
            <>
              {/* Mobile: tap backdrop to close sidebar drawer */}
              {mobile && (
                <button
                  type="button"
                  className="sidebar-mobile-backdrop"
                  onClick={() => toggleSidebar()}
                  aria-label="收起会话列表"
                />
              )}
              <ErrorBoundary label="sidebar">
                <Sidebar />
              </ErrorBoundary>
              {isElectron && (
                <Resizer
                  axis="x"
                  sign={1}
                  title={t("sidebar.resize")}
                  getValue={() => sidebarWidth}
                  onChange={(v) => setWorkspaceSize("sidebarWidth", v)}
                />
              )}
            </>
          ) : (
            <>
              {/* Far-left hot zone: hovering it floats the collapsed sidebar out — desktop only */}
              {isElectron && !settingsOpen && (
                <>
                  <div
                    className="sidebar-reveal-zone"
                    onMouseEnter={() => setRevealed(true)}
                  />
                  {revealed && (
                    <div
                      className="sidebar-overlay"
                      onMouseLeave={() => setRevealed(false)}
                    >
                      <ErrorBoundary label="sidebar">
                        <Sidebar />
                      </ErrorBoundary>
                    </div>
                  )}
                </>
              )}
            </>
          )}
          {/* The workspace shell wraps the session view (chat + optional right sidebar) */}
          <div className="settings-session" hidden={settingsOpen} inert={settingsOpen}>
            <ErrorBoundary label="session">
              <SessionView />
            </ErrorBoundary>
          </div>
          {settingsOpen && <ErrorBoundary label="settings"><AgentSettings /></ErrorBoundary>}
          {workspace.rightOpen && workspace.rightView && (
            <>
              {floatingWorkbench && (
                <button
                  type="button"
                  className="workbench-backdrop"
                  aria-label="返回对话"
                  onClick={toggleRight}
                  tabIndex={-1}
                />
              )}
              {isElectron && !settingsOpen && workspace.rightView && !floatingWorkbench && !mobile && (
                <Resizer
                  axis="x"
                  getValue={() => rightWidth}
                  onChange={(v) => setWorkspaceSize("rightWidth", v)}
                />
              )}
              <div
                className="workspace-right"
                hidden={settingsOpen}
                inert={settingsOpen}
                ref={rightPanel}
                tabIndex={floatingWorkbench ? -1 : undefined}
                role={floatingWorkbench ? "dialog" : undefined}
                aria-modal={floatingWorkbench ? true : undefined}
                aria-label={floatingWorkbench ? "工作台" : undefined}
              >
                <ErrorBoundary label="right-sidebar">
                  <RightSidebar />
                </ErrorBoundary>
              </div>
            </>
          )}
          <UpdateBanner />
          <GlobalMusicBar />
          <MobileUpdateDialog />
          <WorkspacePathDialog />
        </div>
      </div>
    </ErrorBoundary>
  );
}
