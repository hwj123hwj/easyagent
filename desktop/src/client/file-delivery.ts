import { apiRequest, useStore } from "../store";
export async function exportFile(session: string, path: string): Promise<void> {
  const profile = useStore.getState().selectedProfile;
  if (window.piAPI) {
    await window.piAPI.exportFile(session, path, profile);
    return;
  }
  const file = await apiRequest<{
    data: string;
    mimeType: string;
    name: string;
  }>(
    "GET",
    `/workspace/file-data?session_id=${encodeURIComponent(session)}&path=${encodeURIComponent(path)}`,
  );
  if (profile !== useStore.getState().selectedProfile)
    throw new Error("主机已切换，请重新导出");
  const bytes = Uint8Array.from(atob(file.data), (c) => c.charCodeAt(0));
  const url = URL.createObjectURL(new Blob([bytes], { type: file.mimeType }));
  const link = document.createElement("a");
  link.href = url;
  link.download = file.name;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
export interface RunFileChange {
  path: string;
  kind: string;
  attribution: string;
  size: number;
  can_undo: boolean;
  conflict?: string;
  before?: string;
  after?: string;
  binary: boolean;
  undone: boolean;
  version: string;
}
export interface RunFileSet {
  run_id: string;
  workspace: string;
  started_at?: string;
  complete: boolean;
  skipped: string[];
  files: RunFileChange[];
}
