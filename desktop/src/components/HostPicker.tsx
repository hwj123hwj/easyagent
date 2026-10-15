import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useStore } from "../store";
import { Icon } from "./Icon";

export function HostPicker() {
  const profiles = useStore((s) => s.profiles),
    selected = useStore((s) => s.selectedProfile),
    connected = useStore((s) => s.connected),
    connecting = useStore((s) => s.connectionState === "connecting"),
    settings = useStore((s) => s.settingsOpen);
  const [open, setOpen] = useState(false),
    [index, setIndex] = useState(0),
    [rect, setRect] = useState<DOMRect | null>(null);
  const trigger = useRef<HTMLButtonElement>(null),
    menu = useRef<HTMLDivElement>(null);
  const current = profiles.find((p) => p.id === selected);

  function close(restoreFocus = false) {
    setOpen(false);
    if (restoreFocus) trigger.current?.focus();
  }
  function focusItem(next: number) {
    setIndex(next);
    menu.current?.querySelectorAll<HTMLButtonElement>("[role^='menuitem']")[next]?.focus();
  }
  useEffect(() => { setOpen(false); }, [selected, settings, connecting]);
  useEffect(() => {
    if (!open) return;
    const position = () => setRect(trigger.current!.getBoundingClientRect());
    position();
    const frame = requestAnimationFrame(() =>
      focusItem(Math.max(0, profiles.findIndex((p) => p.id === selected))),
    );
    const outside = (event: PointerEvent) => {
      if (!menu.current?.contains(event.target as Node) && !trigger.current?.contains(event.target as Node))
        close();
    };
    document.addEventListener("pointerdown", outside);
    window.addEventListener("resize", position);
    return () => {
      cancelAnimationFrame(frame);
      document.removeEventListener("pointerdown", outside);
      window.removeEventListener("resize", position);
    };
  }, [open, profiles, selected]);

  return (
    <div className="connection-picker">
      <button
        ref={trigger}
        className="host-trigger"
        title="切换运行主机"
        aria-label={`选择运行主机：${current?.name || "未选择"}`}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? "desktop-host-menu" : undefined}
        disabled={connecting}
        onClick={() => setOpen(!open)}
        onKeyDown={(event) => {
          if (event.key === "ArrowDown" || event.key === "ArrowUp") {
            event.preventDefault();
            setOpen(true);
          }
        }}
      >
        <span className={"connection-dot " + (connected ? "online" : "")} aria-hidden="true" />
        <span className="host-trigger-name">{current?.name || "选择主机"}</span>
        <Icon name="chevron-down" size={12} />
      </button>
      {open && rect && createPortal(
        <div
          ref={menu}
          id="desktop-host-menu"
          role="menu"
          aria-label="运行主机"
          className="host-menu"
          style={{
            left: Math.max(12, Math.min(rect.left, innerWidth - 284)),
            top: rect.bottom + 8,
            width: Math.min(272, innerWidth - 24),
            maxHeight: Math.max(0, innerHeight - rect.bottom - 20),
          }}
          onBlur={(event) => {
            if (event.relatedTarget && !event.currentTarget.contains(event.relatedTarget as Node)) close();
          }}
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              event.preventDefault();
              event.stopPropagation();
              close(true);
            } else if (event.key === "Tab") close(true);
            else if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
              event.preventDefault();
              const count = profiles.length + 1;
              focusItem(event.key === "Home" ? 0 : event.key === "End" ? count - 1 :
                (index + (event.key === "ArrowDown" ? 1 : -1) + count) % count);
            }
          }}
        >
          <div className="host-menu-label">运行主机</div>
          {profiles.map((profile, i) => (
            <button
              key={profile.id}
              className="host-menu-option"
              role="menuitemradio"
              aria-checked={profile.id === selected}
              tabIndex={index === i ? 0 : -1}
              onFocus={() => setIndex(i)}
              onClick={() => {
                close(true);
                if (profile.id !== selected) void useStore.getState().connectProfile(profile.id);
              }}
            >
              <Icon name={profile.kind === "local" ? "laptop" : "globe"} size={17} />
              <span className="host-menu-copy">
                <span>{profile.name}</span>
                <small>{profile.kind === "local" ? "在这台电脑运行" : "远程 Agent"}</small>
              </span>
              {profile.id === selected && <Icon name="check" size={15} />}
            </button>
          ))}
          <div className="host-menu-footer">
            <button
              role="menuitem"
              tabIndex={index === profiles.length ? 0 : -1}
              onFocus={() => setIndex(profiles.length)}
              onClick={() => {
                close(true);
                useStore.getState().openSettings(true, "connections");
              }}
            >
              <Icon name="settings" size={15} />管理运行主机
            </button>
          </div>
        </div>, document.body,
      )}
    </div>
  );
}
