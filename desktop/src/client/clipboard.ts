export async function copyText(text: string): Promise<void> {
  if (window.piAPI) return window.piAPI.copyText(text);
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const previous = document.activeElement as HTMLElement | null;
  const field = document.createElement("textarea");
  field.value = text;
  field.style.cssText = "position:fixed;left:-10000px;top:0";
  document.body.append(field);
  field.select();
  try {
    if (!document.execCommand("copy"))
      throw new Error("复制失败，请选中文字手动复制");
  } finally {
    field.remove();
    previous?.focus();
  }
}
