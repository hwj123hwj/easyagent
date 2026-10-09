// Skill-marketplace client: browse/search the official store, start installs,
// and stream install progress over SSE.
//
// Two transports:
// - Electron (window.piAPI): requests go over IPC; SSE uses fetch + ReadableStream
//   against getServerUrl() since EventSource cannot send auth headers.
// - Browser dev mode: same fetch approach with baseUrl + authHeaders from store.

import { apiRequest, authHeaders, getBaseUrl } from "../store";

export interface MarketSkill {
  id: number;
  name: string;
  display_name: string;
  description: string;
  category: string;
  tags: string[];
  version: string;
  install_count: number;
  featured: boolean;
  size_bytes: number;
  sha256?: string | null;
  section_id?: number | null;
  section_name?: string | null;
  icon_url?: string | null;
}

export interface MarketSearchResult {
  skills: MarketSkill[];
  total: number;
  page: number;
}

export interface MarketSection {
  id: number;
  name: string;
}

export interface InstalledSkill {
  id: number;
  name: string;
  display_name?: string;
  version?: string;
  installed_at: string;
  dir: string;
}

export type InstallPhase =
  | "resolving"
  | "downloading"
  | "verifying"
  | "extracting"
  | "done";

export interface InstallJobState {
  job_id: string;
  skill_id: number;
  state: "running" | "succeeded" | "failed";
  phase?: InstallPhase;
  name?: string;
  display_name?: string;
  version?: string;
  bytes_done?: number;
  bytes_total?: number;
  error?: string;
}

export async function marketSearch(params: {
  q?: string;
  category?: string;
  sort?: string;
  page?: number;
}): Promise<MarketSearchResult> {
  const usp = new URLSearchParams();
  if (params.q) usp.set("q", params.q);
  if (params.category) usp.set("category", params.category);
  if (params.sort) usp.set("sort", params.sort);
  if (params.page && params.page > 1) usp.set("page", String(params.page));
  const qs = usp.toString();
  return apiRequest<MarketSearchResult>(
    "GET",
    "/skills/market/search" + (qs ? "?" + qs : ""),
  );
}

export async function marketSections(): Promise<MarketSection[]> {
  const res = await apiRequest<{ sections: MarketSection[] }>(
    "GET",
    "/skills/market/sections",
  );
  return res.sections ?? [];
}

export async function marketInstalled(): Promise<InstalledSkill[]> {
  const res = await apiRequest<{ installed: InstalledSkill[] }>(
    "GET",
    "/skills/market/installed",
  );
  return res.installed ?? [];
}

export async function marketInstallStart(
  id: number,
): Promise<{ job_id: string; events_url: string }> {
  return apiRequest<{ job_id: string; events_url: string }>(
    "POST",
    "/skills/market/install",
    { id },
  );
}

export async function marketJobStatus(
  jobId: string,
): Promise<InstallJobState & { started_at: string }> {
  return apiRequest<InstallJobState & { started_at: string }>(
    "GET",
    `/skills/market/jobs/${encodeURIComponent(jobId)}`,
  );
}

export async function marketUninstall(name: string): Promise<void> {
  await apiRequest("DELETE", `/skills/market/installed/${encodeURIComponent(name)}`);
}

/**
 * Subscribe to an install job's SSE progress stream.
 *
 * Uses fetch + ReadableStream (not EventSource) so the Authorization header
 * works outside loopback. Falls back gracefully: any transport error
 * surfaces via onError; callers should poll marketJobStatus to recover.
 *
 * Returns a cleanup function that aborts the underlying request.
 */
export function subscribeInstallJob(
  jobId: string,
  handlers: {
    onEvent: (evt: InstallJobState) => void;
    onError?: (err: Error) => void;
  },
): () => void {
  const controller = new AbortController();
  const base = typeof window !== "undefined" && window.piAPI?.getServerUrl
    ? null // resolved async below
    : getBaseUrl();

  (async () => {
    try {
      let url: string;
      let headers: Record<string, string>;
      if (window.piAPI) {
        const serverUrl = await window.piAPI.getServerUrl();
        url = `${serverUrl ?? "http://127.0.0.1:8080"}/skills/market/jobs/${encodeURIComponent(jobId)}/events`;
        headers = {}; // loopback IPC-spawned server needs no token
      } else {
        url = `${base}/skills/market/jobs/${encodeURIComponent(jobId)}/events`;
        headers = authHeaders();
      }

      const res = await fetch(url, {
        headers,
        signal: controller.signal,
      });
      if (!res.ok || !res.body) {
        throw new Error(`SSE HTTP ${res.status}`);
      }

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buf = "";
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });

        // Parse complete SSE frames: event: X\ndata: {...}\n\n
        let idx: number;
        while ((idx = buf.indexOf("\n\n")) >= 0) {
          const frame = buf.slice(0, idx);
          buf = buf.slice(idx + 2);
          const dataLine = frame
            .split("\n")
            .find((l) => l.startsWith("data: "));
          if (!dataLine) continue;
          try {
            const evt = JSON.parse(dataLine.slice(6)) as InstallJobState;
            handlers.onEvent(evt);
            if (evt.state === "succeeded" || evt.state === "failed") {
              controller.abort();
              return;
            }
          } catch {
            // Malformed frame: skip, keep the stream alive.
          }
        }
      }
    } catch (err) {
      if (!controller.signal.aborted) {
        handlers.onError?.(err instanceof Error ? err : new Error(String(err)));
      }
    }
  })();

  return () => controller.abort();
}

export function formatBytes(n?: number): string {
  if (!n || n <= 0) return "";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}
