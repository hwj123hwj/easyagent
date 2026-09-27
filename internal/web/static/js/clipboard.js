// HTTP LAN pages cannot use the modern Clipboard API. Keep a synchronous
// selection-based fallback within the click gesture, and restore focus after it.
export async function copyText(text) {
  if (globalThis.isSecureContext && navigator.clipboard?.writeText) {
    try { await navigator.clipboard.writeText(text); return; } catch { /* Try legacy copy. */ }
  }
  const previous = document.activeElement;
  const selection = window.getSelection();
  const ranges = selection ? Array.from({length:selection.rangeCount}, (_, i) => selection.getRangeAt(i).cloneRange()) : [];
  const buffer = document.createElement('textarea');
  buffer.value = text;
  buffer.className = 'clipboard-buffer';
  buffer.setAttribute('readonly', '');
  document.body.append(buffer);
  try {
    buffer.focus({preventScroll:true});
    buffer.select();
    buffer.setSelectionRange(0, buffer.value.length);
    if (!document.execCommand('copy')) throw new Error('clipboard unavailable');
  } finally {
    buffer.remove();
    previous?.focus({preventScroll:true});
    if (selection) { selection.removeAllRanges(); for (const range of ranges) selection.addRange(range); }
  }
}
