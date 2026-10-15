import { app, safeStorage } from "electron";
import * as fs from "fs";
import * as path from "path";
import { randomBytes } from "crypto";

export interface ProviderConfig {
  mode: "inherit" | "override";
  provider?: "openai" | "anthropic";
  baseUrl?: string;
  model?: string;
  hasKey: boolean;
}
export interface ProviderConfigInput {
  mode: "inherit" | "override";
  provider?: "openai" | "anthropic";
  baseUrl?: string;
  model?: string;
  apiKey?: string;
}
interface StoredProvider {
  provider: "openai" | "anthropic";
  baseUrl: string;
  model: string;
  encryptedKey: string;
}
export interface ProviderCheckResult {
  ok: boolean;
  models: Array<{ id: string; name: string }>;
  message: string;
}

export function normalizeProviderURL(value: string): string {
  let url: URL;
  try {
    url = new URL(value.trim());
  } catch {
    throw new Error("模型服务地址须为有效的 HTTP 或 HTTPS 地址");
  }
  if (
    !["https:", "http:"].includes(url.protocol) ||
    url.username || url.password || url.search || url.hash ||
    /[\x00-\x20\x7f]/.test(value)
  ) throw new Error("模型服务地址不能包含凭据、参数、片段或控制字符");
  url.pathname = url.pathname.replace(/\/+$/, "").replace(/\/v1$/, "");
  return url.href.replace(/\/+$/, "");
}

function secureStorage(): void {
  if (
    !safeStorage.isEncryptionAvailable() ||
    (process.platform === "linux" && safeStorage.getSelectedStorageBackend() === "basic_text")
  ) throw new Error("系统安全存储不可用，密钥未保存；请启用系统钥匙环后重试");
}

function fields(input: ProviderConfigInput): Omit<StoredProvider, "encryptedKey"> {
  if (input.provider !== "openai" && input.provider !== "anthropic")
    throw new Error("请选择 OpenAI 兼容或 Anthropic 服务");
  if (typeof input.baseUrl !== "string" || !input.baseUrl)
    throw new Error("请填写模型服务地址");
  if (typeof input.model !== "string" || !input.model.trim() || input.model.length > 200 || /[\x00-\x1f\x7f]/.test(input.model))
    throw new Error("请填写有效的模型标识");
  return { provider: input.provider, baseUrl: normalizeProviderURL(input.baseUrl), model: input.model.trim() };
}

export class ProviderStore {
  private readonly filename = path.join(app.getPath("userData"), "provider-config.json");

  // This encrypted snapshot is private to the main process, including rollback.
  capture(): StoredProvider | null {
    if (!fs.existsSync(this.filename)) return null;
    try {
      const record = JSON.parse(fs.readFileSync(this.filename, "utf8"));
      const clean = fields(record);
      if (typeof record.encryptedKey !== "string" || !record.encryptedKey)
        throw new Error();
      return { ...clean, encryptedKey: record.encryptedKey };
    } catch {
      throw new Error("已保存的本地模型配置无法读取，请检查应用配置文件");
    }
  }
  configuration(): ProviderConfig {
    const record = this.capture();
    if (!record) return { mode: "inherit", hasKey: false };
    return { mode: "override", provider: record.provider, baseUrl: record.baseUrl, model: record.model, hasKey: true };
  }
  private key(record: StoredProvider): string {
    secureStorage();
    try {
      const value = safeStorage.decryptString(Buffer.from(record.encryptedKey, "base64"));
      if (!value || value.length > 8192 || /[\x00-\x20\x7f]/.test(value))
        throw new Error();
      return value;
    } catch {
      throw new Error("无法从系统安全存储读取模型密钥，请重新填写密钥");
    }
  }
  private candidate(input: ProviderConfigInput): StoredProvider | null {
    if (!input || (input.mode !== "inherit" && input.mode !== "override"))
      throw new Error("本地模型配置无效");
    if (input.mode === "inherit") return null;
    const clean = fields(input);
    if (input.apiKey !== undefined && typeof input.apiKey !== "string")
      throw new Error("模型密钥无效");
    const supplied = input.apiKey?.trim();
    if (supplied) {
      if (supplied.length > 8192 || /[\x00-\x20\x7f]/.test(supplied))
        throw new Error("模型密钥长度或字符无效");
      secureStorage();
      try {
        return { ...clean, encryptedKey: safeStorage.encryptString(supplied).toString("base64") };
      } catch {
        throw new Error("模型密钥加密失败，配置未保存");
      }
    }
    const previous = this.capture();
    if (!previous || previous.provider !== clean.provider)
      throw new Error("首次配置或切换服务类型时必须填写模型密钥");
    this.key(previous); // Verify the retained credential can actually be decrypted.
    return { ...clean, encryptedKey: previous.encryptedKey };
  }
  save(input: ProviderConfigInput): ProviderConfig {
    this.restore(this.candidate(input));
    return this.configuration();
  }
  restore(record: StoredProvider | null): void {
    fs.mkdirSync(path.dirname(this.filename), { recursive: true, mode: 0o700 });
    const temporary = `${this.filename}.${randomBytes(8).toString("hex")}.tmp`;
    try {
      if (record === null) {
        if (fs.existsSync(this.filename)) fs.unlinkSync(this.filename);
        return;
      }
      fs.writeFileSync(temporary, JSON.stringify(record) + "\n", { mode: 0o600, flag: "wx" });
      fs.renameSync(temporary, this.filename);
    } catch {
      throw new Error("本地模型配置保存失败，请检查应用目录权限");
    } finally {
      if (fs.existsSync(temporary)) fs.unlinkSync(temporary);
    }
  }
  environment(): NodeJS.ProcessEnv {
    const record = this.capture();
    if (!record) return {};
    const key = this.key(record);
    return record.provider === "openai"
      ? { EA_PROVIDER: "openai", EA_BASE_URL: record.baseUrl, EA_MODEL: record.model, EA_API_KEY: key, OPENAI_BASE_URL: record.baseUrl, OPENAI_MODEL: record.model, OPENAI_API_KEY: key }
      : { EA_PROVIDER: "anthropic", ANTHROPIC_BASE_URL: record.baseUrl, ANTHROPIC_MODEL: record.model, ANTHROPIC_API_KEY: key };
  }
  async check(input?: ProviderConfigInput): Promise<ProviderCheckResult> {
    const record = input ? this.candidate(input) : this.capture();
    if (!record) return { ok: false, models: [], message: "当前继承 CLI 配置；桌面无法确认实际加载的地址与密钥，请配置独立服务后检查连接。" };
    const key = this.key(record);
    try {
      const response = await fetch(record.baseUrl + "/v1/models", {
        method: "GET",
        headers: record.provider === "anthropic"
          ? { "x-api-key": key, Authorization: `Bearer ${key}`, "anthropic-version": "2023-06-01" }
          : { Authorization: `Bearer ${key}` },
        signal: AbortSignal.timeout(10000),
        redirect: "error",
      });
      if (!response.ok) return { ok: false, models: [], message: response.status === 401 || response.status === 403 ? "模型服务认证失败，请检查密钥和访问权限。" : `模型列表接口返回 HTTP ${response.status}；部分服务不支持此接口。` };
      const text = await response.text();
      if (text.length > 2 * 1024 * 1024) throw new Error();
      const result = JSON.parse(text);
      if (!Array.isArray(result.data)) throw new Error();
      const models: ProviderCheckResult["models"] = [];
      for (const item of result.data.slice(0, 10000)) {
        if (typeof item.id !== "string" || !item.id || item.id.length > 512 || /[\x00-\x1f\x7f]/.test(item.id)) continue;
        const name = typeof item.display_name === "string" ? item.display_name : item.id;
        if (item.id.includes(key) || name.includes(key)) throw new Error();
        models.push({ id: item.id, name: name.slice(0, 512) });
      }
      return { ok: models.length > 0, models, message: models.length ? `模型列表接口已连接，返回 ${models.length} 个模型；未执行对话请求。` : "接口响应正常，但未返回模型列表；请检查服务配置或手动填写模型标识。" };
    } catch {
      return { ok: false, models: [], message: "无法读取模型列表，请检查地址、网络或接口格式；未执行对话请求。" };
    }
  }
}
