import { app, safeStorage } from "electron";
import * as fs from "fs";
import * as path from "path";

export interface ConnectionProfile {
  id: string;
  name: string;
  kind: "local" | "remote";
  url: string;
  hasToken: boolean;
}
interface StoredProfile extends Omit<ConnectionProfile, "hasToken"> {
  encryptedToken?: string;
}

export function normalizeServerURL(value: string): string {
  const url = new URL(value);
  if (
    !["http:", "https:"].includes(url.protocol) ||
    url.username ||
    url.password ||
    url.search ||
    url.hash ||
    url.pathname !== "/"
  ) {
    throw new Error(
      "服务地址须为 http(s)://主机:端口，不能包含凭据、路径或参数",
    );
  }
  return url.origin;
}

export class ProfileStore {
  private profiles: StoredProfile[];
  private selected = "local";
  private readonly filename = path.join(
    app.getPath("userData"),
    "connections.json",
  );

  constructor() {
    this.profiles = [
      { id: "local", name: "此 Mac", kind: "local", url: "" },
      {
        id: "mini",
        name: "迷你主机",
        kind: "remote",
        url: "http://192.168.5.16:8080",
      },
    ];
    try {
      const value = JSON.parse(fs.readFileSync(this.filename, "utf8"));
      if (Array.isArray(value.profiles)) {
        for (const profile of value.profiles) {
          if (profile.kind !== "remote" || typeof profile.id !== "string")
            continue;
          this.profiles = this.profiles.filter((p) => p.id !== profile.id);
          this.profiles.push({
            id: profile.id,
            name: String(profile.name || "远程服务"),
            kind: "remote",
            url: normalizeServerURL(profile.url),
            encryptedToken: profile.encryptedToken,
          });
        }
      }
      if (this.profiles.some((p) => p.id === value.selected))
        this.selected = value.selected;
    } catch {
      /* First run or invalid saved configuration: keep usable defaults. */
    }
  }

  list(): { profiles: ConnectionProfile[]; selected: string } {
    return {
      profiles: this.profiles.map(({ encryptedToken, ...p }) => ({
        ...p,
        hasToken: !!encryptedToken,
      })),
      selected: this.selected,
    };
  }
  get(id = this.selected): StoredProfile {
    const profile = this.profiles.find((p) => p.id === id);
    if (!profile) throw new Error("连接配置不存在");
    return profile;
  }
  token(id = this.selected): string {
    const value = this.get(id).encryptedToken;
    if (!value) return "";
    if (!safeStorage.isEncryptionAvailable())
      throw new Error("系统安全存储不可用，无法读取连接令牌");
    return safeStorage.decryptString(Buffer.from(value, "base64"));
  }
  save(input: {
    id: string;
    name: string;
    url: string;
    token?: string;
    clearToken?: boolean;
  }): void {
    if (!/^[a-zA-Z0-9_-]{1,64}$/.test(input.id) || input.id === "local")
      throw new Error("连接标识无效");
    const previous = this.profiles.find((p) => p.id === input.id);
    const profile: StoredProfile = {
      id: input.id,
      name: input.name.trim().slice(0, 80) || "远程服务",
      kind: "remote",
      url: normalizeServerURL(input.url),
      encryptedToken: previous?.encryptedToken,
    };
    if (input.clearToken) delete profile.encryptedToken;
    if (input.token) {
      if (
        !safeStorage.isEncryptionAvailable() ||
        (process.platform === "linux" &&
          safeStorage.getSelectedStorageBackend() === "basic_text")
      ) {
        throw new Error(
          "系统安全存储不可用，令牌未保存；请启用系统钥匙环后重试",
        );
      }
      profile.encryptedToken = safeStorage
        .encryptString(input.token.trim())
        .toString("base64");
    }
    const old = this.profiles;
    this.profiles = [
      ...this.profiles.filter((p) => p.id !== profile.id),
      profile,
    ];
    try {
      this.persist();
    } catch (error) {
      this.profiles = old;
      throw error;
    }
  }
  select(id: string): void {
    this.get(id);
    const old = this.selected;
    this.selected = id;
    try {
      this.persist();
    } catch (error) {
      this.selected = old;
      throw error;
    }
  }
  private persist(): void {
    fs.mkdirSync(path.dirname(this.filename), { recursive: true });
    const temp = `${this.filename}.tmp`;
    fs.writeFileSync(
      temp,
      JSON.stringify({ selected: this.selected, profiles: this.profiles }),
      { mode: 0o600 },
    );
    fs.renameSync(temp, this.filename);
  }
}
