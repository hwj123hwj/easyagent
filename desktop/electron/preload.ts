import { contextBridge, ipcRenderer } from "electron";
const call = (channel: string, ...args: unknown[]) =>
  ipcRenderer.invoke(channel, ...args);
contextBridge.exposeInMainWorld("piAPI", {
  platform: process.platform,
  profiles: () => call("profiles-list"),
  saveProfile: (profile: unknown) => call("profiles-save", profile),
  selectProfile: (id: string) => call("profiles-select", id),
  request: (method: string, path: string, body?: unknown) =>
    call("agent-request", method, path, body),
  connect: () => call("agent-connect"),
  disconnect: () => call("agent-disconnect"),
  send: (value: unknown) => call("agent-send", value),
  onAgentEvent: (handler: (value: unknown) => void) => {
    const listener = (_event: Electron.IpcRendererEvent, value: unknown) =>
      handler(value);
    ipcRenderer.on("agent-event", listener);
    return () => ipcRenderer.removeListener("agent-event", listener);
  },
  backendStatus: () => call("backend-status"),
  providerConfig: () => call("provider-config"),
  saveProviderConfig: (input: unknown) => call("provider-save", input),
  checkProviderConfig: (input?: unknown) => call("provider-check", input),
  getServerUrl: () => call("get-server-url"),
  startServer: () => call("start-server"),
  checkForUpdate: () => call("check-for-update"),
  openDownloadPage: (url: string) => call("open-download-page", url),
  pickFolder: () => call("pick-folder"),
  exportFile: (session: string, path: string, profile: string) => call("export-file", session, path, profile),
  revealInFolder: (path: string) => call("reveal-in-folder", path),
  openInTerminal: (dir: string) => call("open-in-terminal", dir),
  openExternal: (url: string) => call("open-external", url),
  uploadAudio: (data: string, mimeType: string, filename: string) =>
    call("upload-audio", data, mimeType, filename),
  copyText: (text: string) => call("copy-text", text),
  loginMCP: (name: string, workspace: string) =>
    call("mcp-login", name, workspace),
});
