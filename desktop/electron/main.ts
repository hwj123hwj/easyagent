import {
  app,
  BrowserWindow,
  ipcMain,
  shell,
  dialog,
  clipboard,
} from "electron";
import * as path from "path";
import * as fs from "fs";
import * as http from "http";
import { spawn } from "child_process";
import WebSocket from "ws";
import { EasyAgentManager } from "./easyagent-manager";
import { ProfileStore } from "./profile-store";
import { ProviderStore, ProviderConfigInput } from "./provider-store";
import { checkForUpdate } from "./update-checker";

// Development and smoke tests can keep profiles separate from the user's app.
if (process.env.EA_DESKTOP_USER_DATA) {
  const directory = path.resolve(process.env.EA_DESKTOP_USER_DATA);
  fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
  app.setPath("userData", directory);
}

let mainWindow: BrowserWindow | null = null;
let profiles: ProfileStore;
let providerStore: ProviderStore;
let providerApplying = false;
let socket: WebSocket | null = null;
let connectionRevision = 0;
const manager = new EasyAgentManager(() => providerStore.environment());
const publish = (value: unknown) =>
  mainWindow?.webContents.send("agent-event", value);
manager.onStatus = (status) => publish({ type: "backend", ...status });

function trusted(event: Electron.IpcMainInvokeEvent): void {
  if (
    !mainWindow ||
    event.sender !== mainWindow.webContents ||
    event.senderFrame !== mainWindow.webContents.mainFrame
  )
    throw new Error("不允许此窗口访问 Agent");
}
function handle(name: string, handler: (...args: any[]) => any) {
  ipcMain.handle(name, (event, ...args) => {
    trusted(event);
    return handler(...args);
  });
}
async function endpoint() {
  const profile = profiles.get();
  if (profile.kind === "local") {
    if (providerApplying) throw new Error("本地模型配置正在应用，请稍后重试");
    return { url: (await manager.start()).url, token: manager.token };
  }
  return { url: profile.url, token: profiles.token() };
}
async function request(method: string, resource: string, body?: unknown) {
  return requestAt(await endpoint(), method, resource, body);
}
async function requestAt(
  connection: { url: string; token: string },
  method: string,
  resource: string,
  body?: unknown,
) {
  if (
    !/^\/(?!\/)/.test(resource) ||
    resource.includes("\\") ||
    !["GET", "POST", "PUT", "DELETE"].includes(method)
  )
    throw new Error("请求路径或方法无效");
  const { url, token } = connection;
  const response = await fetch(`${url}${resource}`, {
    method,
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(20000),
    redirect: "error",
  });
  const text = await response.text();
  let value: any;
  try {
    value = JSON.parse(text);
  } catch {
    value = text;
  }
  if (!response.ok)
    throw new Error(
      response.status === 401
        ? "服务认证失败，请检查连接令牌"
        : value?.error || `HTTP ${response.status}`,
    );
  return value;
}
async function openExternal(value: string) {
  const url = new URL(value);
  if (!["https:", "http:"].includes(url.protocol))
    throw new Error("只允许打开 HTTP 或 HTTPS 链接");
  await shell.openExternal(url.href);
}
handle("profiles-list", () => profiles.list());
handle("profiles-save", (value) => {
  profiles.save(value);
  return profiles.list();
});
handle("profiles-select", (id: string) => {
  profiles.select(id);
  connectionRevision++;
  socket?.close();
  socket = null;
  return profiles.list();
});
handle("agent-request", request);
handle(
  "upload-audio",
  async (data: string, mimeType: string, filename: string) => {
    if (data.length > 40 * 1024 * 1024) throw new Error("录音超过上传大小限制");
    const { url, token } = await endpoint();
    const form = new FormData();
    form.append(
      "file",
      new Blob([Buffer.from(data, "base64")], { type: mimeType }),
      filename,
    );
    const response = await fetch(`${url}/asr/transcribe`, {
      method: "POST",
      headers: token ? { Authorization: `Bearer ${token}` } : {},
      body: form,
      signal: AbortSignal.timeout(60000),
      redirect: "error",
    });
    const value = await response.json();
    if (!response.ok) throw new Error(value.error || "语音识别失败");
    return value;
  },
);

handle("agent-connect", async () => {
  const revision = ++connectionRevision;
  socket?.close();
  socket = null;
  const { url, token } = await endpoint();
  if (revision !== connectionRevision) return;
  const current = new WebSocket(url.replace(/^http/, "ws") + "/ws", {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
    handshakeTimeout: 10000,
    maxPayload: 16 * 1024 * 1024,
  });
  socket = current;
  current.on("open", () => {
    if (socket === current) publish({ type: "transport", state: "connected" });
  });
  current.on("message", (data) => {
    if (socket === current) {
      try {
        publish({ type: "message", data: JSON.parse(data.toString()) });
      } catch {
        publish({
          type: "transport",
          state: "error",
          message: "服务消息格式无效",
        });
      }
    }
  });
  current.on("close", () => {
    if (socket === current) {
      socket = null;
      publish({ type: "transport", state: "disconnected" });
    }
  });
  current.on("error", (error) => {
    if (socket === current)
      publish({
        type: "transport",
        state: "error",
        message: `连接失败：${error.message}`,
      });
  });
});
handle("agent-disconnect", () => {
  connectionRevision++;
  const current = socket;
  socket = null;
  current?.close();
});
handle("agent-send", (value: object) => {
  if (!socket || socket.readyState !== WebSocket.OPEN) return false;
  socket.send(JSON.stringify(value));
  return true;
});
handle("backend-status", () => manager.getStatus());
function requireLocalProvider(): void {
  if (profiles.get().kind !== "local")
    throw new Error("模型配置只适用于桌面托管的本地 Agent；远程服务请在目标主机配置");
}
handle("provider-config", () => {
  requireLocalProvider();
  return providerStore.configuration();
});
handle("provider-check", (input?: ProviderConfigInput) => {
  requireLocalProvider();
  return providerStore.check(input);
});
handle("provider-save", async (input: ProviderConfigInput) => {
  requireLocalProvider();
  if (providerApplying) throw new Error("本地模型配置正在应用，请稍后重试");
  providerApplying = true;
  let lease = "";
  let connection: { url: string; token: string } | null = null;
  try {
    const previous = providerStore.capture();
    // Wait for an earlier launch before taking its server-side admission lock.
    if (manager.getStatus().state === "starting")
      await manager.start().catch(() => {});
    const running = manager.getServerInfo();
    if (running) {
      connection = { url: running.url, token: manager.token };
      try {
        const result = await requestAt(connection, "POST", "/admin/deploy");
        if (!result || typeof result.lease !== "string" || !result.lease)
          throw new Error();
        lease = result.lease;
      } catch {
        throw new Error("本地 Agent 仍有任务运行或无法确认空闲，请等待任务结束后保存；配置未修改");
      }
    }
    // A profile switch while checking readiness must not apply local settings.
    requireLocalProvider();
    const configuration = providerStore.save(input);
    connectionRevision++;
    const current = socket;
    socket = null;
    current?.close();
    publish({ type: "transport", state: "disconnected" });
    try {
      await manager.restart();
    } catch {
      try {
        providerStore.restore(previous);
      } catch {
        throw new Error("本地 Agent 重启失败，原配置恢复失败；请检查应用目录权限后重新配置");
      }
      try {
        await manager.restart();
      } catch {
        throw new Error("本地 Agent 重启失败；原模型配置已恢复，请检查核心安装后重试启动");
      }
      throw new Error("新配置未能启动本地 Agent，已恢复原模型配置，请检查配置后重试");
    }
    return configuration;
  } finally {
    if (lease && connection && manager.getServerInfo()?.url === connection.url) {
      await requestAt(connection, "DELETE", "/admin/deploy", { lease }).catch(() => {});
    }
    providerApplying = false;
  }
});
handle("get-server-url", async () => (await endpoint()).url);
handle("start-server", async () => {
  try {
    return await manager.start();
  } catch (error) {
    return { error: (error as Error).message };
  }
});
handle("check-for-update", checkForUpdate);
handle("open-download-page", openExternal);
handle("open-external", openExternal);
handle("copy-text", (text: string) => {
  if (typeof text !== "string") throw new Error("复制内容无效");
  clipboard.writeText(text);
});
handle("pick-folder", async () => {
  if (profiles.get().kind !== "local")
    throw new Error("远程工作区请填写主机上的路径");
  const result = await dialog.showOpenDialog(mainWindow!, {
    properties: ["openDirectory"],
  });
  return result.canceled ? null : result.filePaths[0] || null;
});
handle("reveal-in-folder", (file: string) => {
  if (profiles.get().kind !== "local")
    throw new Error("文件在远程主机，请在文件面板查看");
  shell.showItemInFolder(file);
});
handle("open-in-terminal", (dir: string) => {
  if (profiles.get().kind !== "local") throw new Error("路径属于远程主机");
  const child =
    process.platform === "darwin"
      ? spawn("open", ["-a", "Terminal", dir])
      : process.platform === "win32"
        ? spawn("cmd", ["/c", "start", "cmd"], { cwd: dir })
        : spawn("x-terminal-emulator", [], { cwd: dir });
  child.on("error", () =>
    publish({ type: "notice", message: "未找到本机终端程序" }),
  );
});
handle("mcp-login", async (name: string, workspace: string) => {
  const connection = await endpoint(); // Pin the issuing server even when the user switches profile.
  const suffix = `?workspace=${encodeURIComponent(workspace)}`;
  const resource = `/mcp/servers/${encodeURIComponent(name)}/login`;
  return new Promise<void>((resolve, reject) => {
    let state = "",
      finished = false;
    const finish = (error?: Error) => {
      if (finished) return;
      finished = true;
      clearTimeout(timer);
      server.close();
      error ? reject(error) : resolve();
    };
    const server = http.createServer(async (req, res) => {
      const url = new URL(req.url || "/", "http://127.0.0.1");
      if (req.method !== "GET" || url.pathname !== "/callback") {
        res.writeHead(404).end();
        return;
      }
      if (!state || url.searchParams.get("state") !== state) {
        res.writeHead(400).end("Authorization state mismatch");
        return;
      }
      const code = url.searchParams.get("code");
      if (!code || url.searchParams.get("error")) {
        res.writeHead(400).end("Authorization was declined");
        finish(new Error("授权已取消"));
        return;
      }
      try {
        await requestAt(connection, "POST", resource + "/complete" + suffix, {
          state,
          code,
        });
        res
          .writeHead(200, {
            "Content-Type": "text/html; charset=utf-8",
            "Cache-Control": "no-store",
          })
          .end("<p>EasyAgent 授权完成，可以关闭此窗口。</p>");
        finish();
      } catch {
        res.writeHead(502).end("Authorization failed. Return to EasyAgent.");
        finish(new Error("授权完成请求失败，请重新连接后重试"));
      }
    });
    const timer = setTimeout(
      () => finish(new Error("授权超时，请重新发起连接")),
      120000,
    );
    server.on("error", finish);
    server.listen(0, "127.0.0.1", async () => {
      try {
        const address = server.address() as import("net").AddressInfo;
        const value = await requestAt(connection, "POST", resource + suffix, {
          redirect_url: `http://127.0.0.1:${address.port}/callback`,
        });
        state = value.state;
        if (!state || !value.authorization_url)
          throw new Error("服务未返回授权链接");
        await openExternal(value.authorization_url);
      } catch (error) {
        finish(error as Error);
      }
    });
  });
});

const macWindowButtonPosition = { x: 14, y: 16 };

async function createWindow() {
  mainWindow = new BrowserWindow({
    width: 1280,
    height: 860,
    minWidth: 780,
    minHeight: 580,
    title: "EasyAgent",
    backgroundColor: "#101b1c",
    ...(process.platform === "darwin" ? {
      titleBarStyle: "hiddenInset" as const,
      trafficLightPosition: macWindowButtonPosition,
    } : {}),
    webPreferences: {
      preload: path.join(__dirname, "preload.js"),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
    },
  });
  if (process.platform === "darwin") {
    const window = mainWindow;
    const alignWindowButtons = () => {
      setImmediate(() => {
        if (!window.isDestroyed() && !window.isFullScreen()) {
          window.setWindowButtonPosition(macWindowButtonPosition);
        }
      });
    };
    // Wait for AppKit's layout to finish before restoring the header alignment.
    window.on("resized", alignWindowButtons);
    window.on("maximize", alignWindowButtons);
    window.on("unmaximize", alignWindowButtons);
    window.on("leave-full-screen", alignWindowButtons);
  }
  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    void openExternal(url).catch(() => {});
    return { action: "deny" };
  });
  mainWindow.webContents.on("will-navigate", (event, url) => {
    if (url !== mainWindow?.webContents.getURL()) {
      event.preventDefault();
      void openExternal(url).catch(() => {});
    }
  });
  if (!app.isPackaged) await mainWindow.loadURL("http://localhost:5173");
  else
    await mainWindow.loadFile(path.join(__dirname, "../renderer/index.html"));
  mainWindow.on("closed", () => {
    mainWindow = null;
    socket?.close();
    socket = null;
  });
}
if (!app.requestSingleInstanceLock()) app.quit();
else {
  app.on("second-instance", () => {
    if (mainWindow?.isMinimized()) mainWindow.restore();
    mainWindow?.focus();
  });
  app.whenReady().then(async () => {
    profiles = new ProfileStore();
    providerStore = new ProviderStore();
    await createWindow();
    app.on("activate", () => {
      if (!mainWindow) void createWindow();
    });
  });
}
app.on("window-all-closed", () => {
  if (process.platform !== "darwin") app.quit();
});
let quitting = false;
app.on("before-quit", (event) => {
  if (quitting) return;
  event.preventDefault();
  quitting = true;
  socket?.close();
  void manager.stop().finally(() => app.quit());
});
