import { useEffect, useState } from "react";
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
  const t = useT();

  // Auto-reveal collapsed sidebar on hover (VSCode-style) — desktop only
  const [revealed, setRevealed] = useState(false);
  const sidebarOpen = workspace.sidebarOpen;
  useEffect(() => {
    setRevealed(false);
  }, [sidebarOpen]);

  useEffect(() => {
    void init();
  }, [init]);

  // Mark body as mobile for CSS targeting
  useEffect(() => {
    const update = () =>
      document.body.classList.toggle("mobile", innerWidth < 769);
    update();
    window.addEventListener("resize", update);
    return () => window.removeEventListener("resize", update);
  }, []);

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
      if (!mod) return;
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
          className={`app ${sidebarOpen ? "" : "sidebar-collapsed"} ${musicActive ? "music-active" : ""}`}
          style={
            {
              "--sidebar-w": `${workspace.sidebarWidth}px`,
            } as React.CSSProperties
          }
        >
          {sidebarOpen ? (
            <>
              {/* Mobile: tap backdrop to close sidebar drawer */}
              {!isElectron && (
                <div
                  className="sidebar-mobile-backdrop"
                  onClick={() => toggleSidebar()}
                  style={{
                    position: "fixed",
                    inset: 0,
                    background: "rgba(0,0,0,0.5)",
                    zIndex: 150,
                  }}
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
                  getValue={() => useStore.getState().workspace.sidebarWidth}
                  onChange={(v) => setWorkspaceSize("sidebarWidth", v)}
                />
              )}
            </>
          ) : (
            <>
              {/* Far-left hot zone: hovering it floats the collapsed sidebar out — desktop only */}
              {isElectron && (
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
              {/* Mobile: floating hamburger to open sidebar */}
              {!isElectron && (
                <button
                  className="mobile-sidebar-toggle"
                  onClick={() => toggleSidebar()}
                  aria-label="Open sessions"
                  style={{
                    position: "fixed",
                    top: "calc(env(safe-area-inset-top) + 8px)",
                    left: 12,
                    zIndex: 100,
                    width: 40,
                    height: 40,
                    borderRadius: 10,
                    border: "1px solid var(--border)",
                    background: "var(--bg-elev)",
                    color: "var(--text-dim)",
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                  }}
                >
                  <Icon name="menu" size={18} />
                </button>
              )}
            </>
          )}
          {/* The workspace shell wraps the session view (chat + optional right sidebar) */}
          <ErrorBoundary label="session">
            {settingsOpen ? <AgentSettings /> : <SessionView />}
          </ErrorBoundary>
          {workspace.rightOpen && !settingsOpen && (
            <>
              {isElectron && workspace.rightView && (
                <Resizer
                  axis="x"
                  getValue={() => useStore.getState().workspace.rightWidth}
                  onChange={(v) => setWorkspaceSize("rightWidth", v)}
                />
              )}
              <ErrorBoundary label="right-sidebar">
                <RightSidebar />
              </ErrorBoundary>
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
