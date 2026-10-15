import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { apiRequest, useStore } from "../store";
import { Icon } from "./Icon";

// All attachment reads go through the same authenticated IPC/browser API as
// the rest of the client. Tokens never appear in image URLs or renderer state.
export function AttachmentImage({ url, attachmentId, sessionId, name, className }: {
 url?: string; attachmentId?: string; sessionId?: string; name: string; className: string;
}) {
 const profile = useStore(state => state.selectedProfile);
 const [source, setSource] = useState(url || ""), [error, setError] = useState(""), [open, setOpen] = useState(false);
 const trigger = useRef<HTMLButtonElement>(null), close = useRef<HTMLButtonElement>(null);
 useEffect(() => {
  let cancelled = false;
  setSource(url || ""); setError(""); setOpen(false);
  if (!url && attachmentId && sessionId) void apiRequest<{ data_url: string }>("GET", `/sessions/${encodeURIComponent(sessionId)}/attachments/${encodeURIComponent(attachmentId)}/raw?format=data_url`)
   .then(value => { if (!cancelled) setSource(value.data_url); })
   .catch(() => { if (!cancelled) setError("图片无法读取"); });
  return () => { cancelled = true; };
 }, [url, attachmentId, sessionId, profile]);
 useEffect(() => {
  if (!open) return;
  const previous = document.activeElement as HTMLElement | null;
  close.current?.focus();
  const keydown = (event: KeyboardEvent) => {
   if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); setOpen(false); }
   if (event.key === "Tab") { event.preventDefault(); close.current?.focus(); }
  };
  document.addEventListener("keydown", keydown, true);
  return () => { document.removeEventListener("keydown", keydown, true); previous?.focus(); };
 }, [open]);
 return <>
  <button type="button" ref={trigger} className="attachment-image-trigger" aria-label={`预览 ${name}`} disabled={!source || !!error} onClick={() => setOpen(true)}>
   {source && !error ? <img className={className} src={source} alt={name} onError={() => setError("图片无法读取")} /> : <span className="attachment-image-status">{error || "加载图片…"}</span>}
  </button>
  {open && createPortal(<div className="attachment-image-lightbox" role="dialog" aria-modal="true" aria-label={name} onClick={() => setOpen(false)}>
   <div className="attachment-image-lightbox-content" onClick={event => event.stopPropagation()}>
    <header><span>{name}</span><button type="button" ref={close} className="icon-btn" aria-label="关闭图片预览" onClick={() => setOpen(false)}><Icon name="x" size={16} /></button></header>
    <img src={source} alt={name} />
   </div>
  </div>, document.body)}
 </>;
}
