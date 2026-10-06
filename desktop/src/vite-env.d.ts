/// <reference types="vite/client" />

declare module '*.css' {
  const content: string;
  export default content;
}

declare module '*.png' {
  const src: string;
  export default src;
}

declare module '*.webp' {
  const src: string;
  export default src;
}

declare module '*.mp4' {
  const src: string;
  export default src;
}

declare const __APP_VERSION__: string;

// ── window.piAPI type augmentation ──────────────────────────────────────────
interface PiAPI {
  readonly platform?: string;
  profiles: () => Promise<{profiles: import('./client/protocol').ConnectionProfile[]; selected: string}>;
  saveProfile: (profile: {id:string;name:string;url:string;token?:string;clearToken?:boolean}) => Promise<{profiles: import('./client/protocol').ConnectionProfile[];selected:string}>;
  selectProfile: (id:string) => Promise<{profiles: import('./client/protocol').ConnectionProfile[];selected:string}>;
  request: (method:string,path:string,body?:unknown) => Promise<unknown>;
  connect: () => Promise<void>;
  disconnect: () => Promise<void>;
  send: (value:object) => Promise<boolean>;
  onAgentEvent: (handler:(value:any)=>void) => () => void;
  backendStatus: () => Promise<{state:string;message?:string}>;
  providerConfig: () => Promise<import('./types').ProviderConfig>;
  saveProviderConfig: (input: import('./types').ProviderConfigInput) => Promise<import('./types').ProviderConfig>;
  checkProviderConfig: (input?: import('./types').ProviderConfigInput) => Promise<import('./types').ProviderCheckResult>;
  uploadAudio: (data:string,mimeType:string,filename:string) => Promise<{text?:string}>;
  copyText: (text:string) => Promise<void>;
  notifyRunDone: (payload:{sessionTitle?:string; ok:boolean}) => Promise<void>;
  setBadge: (count:number) => Promise<void>;
  loginMCP: (name:string,workspace:string) => Promise<void>;

  getServerUrl: () => Promise<string | null>;
  startServer: () => Promise<{ url: string; port: number } | { error: string }>;
  checkForUpdate: () => Promise<{
    version: string;
    downloadUrl: string;
    releaseNotes: string;
  } | null>;
  openDownloadPage: (url: string) => Promise<void>;
  pickFolder: () => Promise<string | null>;
  exportFile: (session: string, path: string, profile: string) => Promise<void>;
  revealInFolder: (path: string) => Promise<void>;
  openInTerminal: (dir: string) => Promise<void>;
  terminalOpen: (id: string, session: string, profile: string, cols: number, rows: number) => Promise<void>;
  terminalSend: (id: string, message: object) => Promise<void>;
  terminalClose: (id: string) => Promise<void>;
  onTerminalEvent: (handler: (value: { id: string; type: string; data?: string; message?: string }) => void) => () => void;
  openExternal: (url: string) => Promise<void>;
}

interface Window {
  piAPI?: PiAPI;
}
