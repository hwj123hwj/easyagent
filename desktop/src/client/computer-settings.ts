export interface ComputerSettings {
  enabled: boolean;
  provider: string;
  available: boolean;
  granted: boolean;
  missing?: string[];
  approval_policy: "ask" | "auto";
  approved_apps: string[];
  lease_holder: string;
  helper_path?: string;
}

// Old or misrouted hosts may return HTML with HTTP 200. Report a load error
// rather than letting an unchecked IPC response crash the settings page.
export function parseComputerSettings(value: unknown): ComputerSettings {
  const data = value as Partial<ComputerSettings> | null;
  if (!data || typeof data !== "object" ||
      typeof data.enabled !== "boolean" || typeof data.provider !== "string" ||
      typeof data.available !== "boolean" || typeof data.granted !== "boolean" ||
      !["ask", "auto"].includes(data.approval_policy || "") ||
      typeof data.lease_holder !== "string" ||
      (data.approved_apps != null && (!Array.isArray(data.approved_apps) ||
        !data.approved_apps.every((app) => typeof app === "string")))) {
    throw new Error("主机未返回有效的电脑控制设置，请更新 Agent 核心后重试");
  }
  return { ...data, approved_apps: data.approved_apps ?? [] } as ComputerSettings;
}
