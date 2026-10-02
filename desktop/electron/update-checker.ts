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
    if (release.draft || release.prerelease) continue;
    for (const asset of release.assets || []) {
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
      const url = new URL(asset.browser_download_url);
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
export async function checkForUpdate(): Promise<UpdateInfo | null> {
  const response = await net.fetch(
    `https://api.github.com/repos/${REPO}/releases?per_page=30`,
    { signal: AbortSignal.timeout(15000) },
  );
  if (!response.ok)
    throw new Error(`无法检查桌面更新（HTTP ${response.status}）`);
  const releases = await response.json();
  if (!Array.isArray(releases)) throw new Error("版本服务返回格式无效");
  return desktopUpdate(releases, app.getVersion(), process.arch);
}
