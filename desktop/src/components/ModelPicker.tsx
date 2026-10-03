import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useStore } from "../store";
import { Icon } from "./Icon";
export function ModelPicker({
  value,
  onChange,
  disabled = false,
}: {
  value?: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  const models = useStore((s) => s.models),
    [open, setOpen] = useState(false),
    [query, setQuery] = useState(""),
    [index, setIndex] = useState(0),
    [rect, setRect] = useState<DOMRect | null>(null);
  const source = useStore((s) => s.modelSource);
  const notice = useStore((s) => s.modelsNotice);
  const button = useRef<HTMLButtonElement>(null),
    menu = useRef<HTMLDivElement>(null),
    search = useRef<HTMLInputElement>(null);
  const filtered = models.filter((m) =>
    (m.name + " " + m.modelId)
      .toLocaleLowerCase()
      .includes(query.toLocaleLowerCase()),
  );
  function close() {
    setOpen(false);
    button.current?.focus();
  }
  useEffect(() => {
    if (!open) return;
    setQuery("");
    setIndex(0);
    const position = () => setRect(button.current!.getBoundingClientRect());
    position();
    requestAnimationFrame(() => search.current?.focus());
    const down = (e: PointerEvent) => {
      if (
        !menu.current?.contains(e.target as Node) &&
        !button.current?.contains(e.target as Node)
      )
        setOpen(false);
    };
    document.addEventListener("pointerdown", down);
    window.addEventListener("resize", position);
    return () => {
      document.removeEventListener("pointerdown", down);
      window.removeEventListener("resize", position);
    };
  }, [open]);
  useEffect(() => {
    if (open)
      document
        .getElementById("model-" + index)
        ?.scrollIntoView({ block: "nearest" });
  }, [index, open]);
  const current = models.find((m) => m.modelId === value);
  return (
    <>
      <button
        ref={button}
        className="model-trigger"
        disabled={disabled}
        aria-expanded={open}
        aria-haspopup="listbox"
        onClick={() => setOpen(!open)}
      >
        <Icon name="cpu" size={14} />
        <span>{current?.name || value || "配置模型"}</span>
        <Icon name="chevron-down" size={12} />
      </button>
      {open &&
        rect &&
        createPortal(
          <div
            ref={menu}
            className="model-menu"
            style={{
              position: "fixed",
              left: Math.max(12, Math.min(rect.left, innerWidth - 340)),
              bottom: Math.max(12, innerHeight - rect.top + 8),
              width: Math.min(328, innerWidth - 24),
              maxHeight: Math.max(120, rect.top - 24),
            }}
          >
            <label className="model-search">
              <Icon name="search" size={15} />
              <input
                ref={search}
                aria-label="搜索模型"
                value={query}
                placeholder="搜索模型…"
                role="combobox"
                aria-controls="desktop-models"
                aria-expanded="true"
                aria-activedescendant={
                  filtered[index] ? "model-" + index : undefined
                }
                onChange={(e) => {
                  setQuery(e.target.value);
                  setIndex(0);
                }}
                onKeyDown={(e) => {
                  if (e.key === "Escape") {
                    e.preventDefault();
                    close();
                  } else if (e.key === "Tab") setOpen(false);
                  else if (["ArrowUp", "ArrowDown"].includes(e.key)) {
                    e.preventDefault();
                    setIndex(
                      (n) =>
                        (n +
                          (e.key === "ArrowDown" ? 1 : -1) +
                          filtered.length) %
                        Math.max(1, filtered.length),
                    );
                  } else if (e.key === "Enter" && filtered[index]) {
                    e.preventDefault();
                    onChange(filtered[index].modelId);
                    close();
                  }
                }}
              />
            </label>
            <div
              id="desktop-models"
              role="listbox"
              aria-label="模型目录"
              className="model-options"
            >
              {filtered.map((m, i) => (
                <button
                  id={"model-" + i}
                  key={m.modelId}
                  role="option"
                  aria-selected={value === m.modelId}
                  className={i === index ? "highlighted" : ""}
                  onMouseEnter={() => setIndex(i)}
                  onClick={() => {
                    onChange(m.modelId);
                    close();
                  }}
                >
                  <span>
                    <strong>{m.name}</strong>
                    <small>{m.modelId}</small>
                  </span>
                  {value === m.modelId && <Icon name="check" size={14} />}
                </button>
              ))}
              {!filtered.length && <p className="menu-empty">{models.length ? "没有匹配的模型" : "尚未获取模型目录，请先配置模型连接。"}</p>}
            </div>
            {source === "configured" && <p className="menu-hint">仅显示已配置模型，连接尚未验证</p>}
            {notice && <p className="menu-hint" role="status">{notice}</p>}
            <button className="model-settings-link" onClick={() => { setOpen(false); useStore.getState().openSettings(true, "models"); }}><Icon name="settings" size={14} />模型连接设置</button>
            <div className="menu-hint">↑ ↓ 选择 · Enter 确认 · Esc 关闭</div>
          </div>,
          document.body,
        )}
    </>
  );
}
