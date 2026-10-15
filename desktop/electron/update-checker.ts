import { app, net } from "electron";
import { valid, gt } from "semver";
const REPO = "hwj123hwj/easyagent";
export interface UpdateInfo {
  version: string;
  downloadUrl: string;
  releaseNotes: string;
}
interface Release {
  draft?: boolean;
  prerelease?: boolean;
  body?: string;
  assets?: Array<{ name: string; browser_download_url: string }>;
}
// Core v-tags and desktop versions are independent. Only a matching DMG proves
// that the desktop client has an installable update for this architecture.
export function desktopUpdate(
  releases: Release[],
  current: string,
  arch: string,
): UpdateInfo | null {
  let selected: UpdateInfo | null = null;
  for (const release of releases) {
    if (!release || typeof release !== "object") continue;
    if (release.draft || release.prerelease) continue;
    for (const asset of Array.isArray(release.assets) ? release.assets : []) {
      if (!asset || typeof asset.name !== "string" || typeof asset.browser_download_url !== "string") continue;
      const match = asset.name.match(
        /^EasyAgent-(\d+\.\d+\.\d+)-(arm64|x64)\.dmg$/,
      );
      if (
        !match ||
        match[2] !== arch ||
        !valid(match[1]) ||
        !valid(current) ||
        !gt(match[1], current) ||
        (selected && !gt(match[1], selected.version))
      )
        continue;
      let url: URL;
      try { url = new URL(asset.browser_download_url); } catch { continue; }
      if (
        url.protocol !== "https:" ||
        url.hostname !== "github.com" ||
        !url.pathname.startsWith("/" + REPO + "/releases/download/")
      )
        continue;
      selected = {
        version: match[1],
        downloadUrl: url.href,
        releaseNotes: release.body || "",
      };
    }
  }
  return selected;
}
// The published manifest is served through github.com, independently of the
// anonymous REST API quota shared by users of the same proxy/network exit.
export function manifestUpdate(value: unknown, current: string, arch: string): UpdateInfo | null {
  const manifest = value as { version?: unknown; commit?: unknown; assets?: unknown } | null;
  if (!manifest || typeof manifest.version !== "string" ||
      !/^v\d+\.\d+\.\d+$/.test(manifest.version) || !valid(manifest.version) ||
      typeof manifest.commit !== "string" || !/^[a-f0-9]{40}$/.test(manifest.commit) ||
      !manifest.assets || typeof manifest.assets !== "object" || Array.isArray(manifest.assets))
    throw new Error("正式发布清单格式无效");
  const assets = Object.entries(manifest.assets).filter(([name, digest]) =>
    new RegExp(`^EasyAgent-\\d+\\.\\d+\\.\\d+-${arch}\\.dmg$`).test(name) &&
    typeof digest === "string" && /^[a-f0-9]{64}$/.test(digest));
  if (!assets.length) throw new Error("正式发布清单没有适用于本机的桌面安装包");
  return desktopUpdate([{
    assets: assets.map(([name]) => ({ name, browser_download_url:
      `https://github.com/${REPO}/releases/download/${manifest.version}/${name}` })),
  }], current, arch);
}

let pending: Promise<UpdateInfo | null> | undefined;
let cached: { key: string; until: number; value: UpdateInfo | null } | undefined;
let apiRetryAt = 0;

async function queryUpdate(current: string, arch: string): Promise<UpdateInfo | null> {
  if (Date.now() >= apiRetryAt) {
    try {
      const response = await net.fetch(`https://api.github.com/repos/${REPO}/releases?per_page=30`, {
        headers: { "User-Agent": `EasyAgent-Desktop/${current}`, Accept: "application/vnd.github+json" },
        signal: AbortSignal.timeout(6000),
      });
      if (response.ok) {
        const releases = await response.json();
        if (!Array.isArray(releases)) throw new Error("版本服务返回格式无效");
        return desktopUpdate(releases, current, arch);
      }
      if (response.status === 403 || response.status === 429) {
        const retry = Number(response.headers.get("retry-after"));
        const reset = Number(response.headers.get("x-ratelimit-reset"));
        apiRetryAt = Math.max(Date.now() + 60000,
          Number.isFinite(retry) ? Date.now() + retry * 1000 : 0,
          Number.isFinite(reset) ? reset * 1000 : 0);
      }
    } catch {
      apiRetryAt = Date.now() + 60000;
    }
  }
  try {
    const response = await net.fetch(`https://github.com/${REPO}/releases/latest/download/release.json`, {
      signal: AbortSignal.timeout(10000),
    });
    if (!response.ok) throw new Error("发布清单不可用");
    return manifestUpdate(await response.json(), current, arch);
  } catch {
    throw new Error("暂时无法连接更新服务，请稍后重试，或打开 GitHub 发布页下载。已有应用可以继续使用。");
  }
}

export function checkForUpdate(): Promise<UpdateInfo | null> {
  const current = app.getVersion(), arch = process.arch;
  const key = `${current}:${arch}`;
  if (pending) return pending;
  if (cached?.key === key && cached.until > Date.now()) return Promise.resolve(cached.value);
  pending = queryUpdate(current, arch).then(value => {
    cached = { key, until: Date.now() + 60000, value };
    return value;
  }).finally(() => { pending = undefined; });
  return pending;
}
