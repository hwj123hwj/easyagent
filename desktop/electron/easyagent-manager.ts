import { ChildProcess, spawn } from "child_process";
import * as path from "path";
import * as fs from "fs";
import * as net from "net";
import { randomBytes } from "crypto";
import { app } from "electron";

export interface EasyAgentServerInfo {
  url: string;
  port: number;
}
export interface BackendStatus {
  state: "stopped" | "starting" | "ready" | "error";
  message?: string;
}

export class EasyAgentManager {
  private child: ChildProcess | null = null;
  private info: EasyAgentServerInfo | null = null;
  private pending: Promise<EasyAgentServerInfo> | null = null;
  private stopping = false;
  private status: BackendStatus = { state: "stopped" };
  constructor(private readonly providerEnvironment: () => NodeJS.ProcessEnv = () => ({})) {}
  readonly token = randomBytes(32).toString("hex");
  onStatus: (status: BackendStatus) => void = () => {};
  getStatus(): BackendStatus {
    return this.status;
  }
  private report(status: BackendStatus): void {
    this.status = status;
    this.onStatus(status);
  }
  start(): Promise<EasyAgentServerInfo> {
    if (this.info) return Promise.resolve(this.info);
    if (this.pending) return this.pending;
    this.pending = this.launch().finally(() => {
      this.pending = null;
    });
    return this.pending;
  }
  private async launch(): Promise<EasyAgentServerInfo> {
    this.stopping = false;
    this.report({ state: "starting", message: "正在启动本地 Agent" });
    try {
      const port = await new Promise<number>((resolve, reject) => {
        const server = net.createServer();
        server.on("error", reject);
        server.listen(0, "127.0.0.1", () => {
          const port = (server.address() as net.AddressInfo).port;
          server.close(() => resolve(port));
        });
      });
      const url = `http://127.0.0.1:${port}`;
      const root = path.resolve(__dirname, "../../..");
      const binary = app.isPackaged
        ? path.join(process.resourcesPath, "easyagent")
        : [
            path.join(root, "bin/easyagent"),
            path.join(root, "easyagent"),
            path.join(app.getPath("home"), ".easyagent/bin/easyagent"),
          ].find((p) => fs.existsSync(p)) || "easyagent";
      if (app.isPackaged && !fs.existsSync(binary))
        throw new Error("安装包缺少 Agent 核心，请重新安装");
      // A managed process must never open another service's run receipts.
      const dataDirectory = path.join(app.getPath("userData"), "core-data");
      fs.mkdirSync(dataDirectory, { recursive: true, mode: 0o700 });
      const env = {
        ...process.env,
        ...this.providerEnvironment(),
        EA_DATA_DIR: dataDirectory,
        EA_SERVER_API_KEY: this.token,
        EA_ALLOW_NO_AUTH: "0",
        EA_ALLOWED_ORIGINS: "",
        ...(app.isPackaged
          ? {
              PATH:
                path.join(process.resourcesPath, "runtime/bin") +
                path.delimiter +
                (process.env.PATH || ""),
              EA_WORKFLOW_RUNTIME: path.join(
                process.resourcesPath,
                "workflow-runtime.mjs",
              ),
            }
          : {}),
      };
      this.child = spawn(binary, ["serve", "--listen", `127.0.0.1:${port}`], {
        cwd: app.isPackaged ? app.getPath("home") : root,
        env,
        stdio: ["ignore", "ignore", "ignore"],
      });
      const child = this.child;
      let failure: Error | undefined;
      child.once("error", (error) => {
        if (this.child !== child) return;
        failure = new Error("本地 Agent 启动失败，请检查核心安装和模型配置");
        this.info = null;
        this.report({
          state: "error",
          message: failure.message,
        });
      });
      child.once("exit", (code) => {
        if (this.child !== child) return;
        this.child = null;
        this.info = null;
        if (!this.stopping) {
          failure = new Error(`本地 Agent 已退出（${code ?? "signal"}）`);
          this.report({ state: "error", message: failure.message });
        }
      });
      const deadline = Date.now() + 15000;
      while (Date.now() < deadline) {
        if (failure) throw failure;
        try {
          const response = await fetch(`${url}/health`, {
            signal: AbortSignal.timeout(700),
          });
          if (response.ok && !failure && this.child === child) {
            this.info = { url, port };
            this.report({ state: "ready" });
            return this.info;
          }
        } catch {
          /* Readiness poll has a bounded per-request timeout. */
        }
        await new Promise((resolve) => setTimeout(resolve, 200));
      }
      throw new Error("本地 Agent 15 秒内未就绪，可切换迷你主机或重试启动");
    } catch (error) {
      await this.stop();
      this.report({
        state: "error",
        message: error instanceof Error ? error.message : "启动失败",
      });
      throw error;
    }
  }
  async restart(): Promise<EasyAgentServerInfo> {
    if (this.pending) await this.pending.catch(() => {});
    await this.stop();
    return this.start();
  }
  async stop(): Promise<void> {
    this.stopping = true;
    const child = this.child;
    this.child = null;
    this.info = null;
    if (child && child.exitCode === null) {
      await new Promise<void>((resolve) => {
        const timer = setTimeout(() => {
          child.kill("SIGKILL");
          resolve();
        }, 16000);
        child.once("exit", () => {
          clearTimeout(timer);
          resolve();
        });
        child.kill("SIGTERM");
      });
    }
    this.report({ state: "stopped" });
  }
  getServerInfo(): EasyAgentServerInfo | null {
    return this.info;
  }
}
