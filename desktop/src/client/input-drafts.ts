import type { DraftInputs, InputAttachment } from "./protocol";
// Storage is user-controlled; damaged drafts must not prevent opening the client.
export function sanitizeInputDrafts(
  value: unknown,
): Record<string, Record<string, DraftInputs>> {
  const out: Record<string, Record<string, DraftInputs>> = {};
  if (!value || typeof value !== "object" || Array.isArray(value)) return out;
  for (const [profile, sessions] of Object.entries(value)) {
    if (!sessions || typeof sessions !== "object" || Array.isArray(sessions))
      continue;
    out[profile] = {};
    for (const [id, draft] of Object.entries(sessions)) {
      if (!draft || typeof draft !== "object") continue;
      const input = draft as Partial<DraftInputs>;
      const attachments = Array.isArray(input.attachments)
        ? input.attachments
            .filter(
              (item): item is InputAttachment =>
                !!item &&
                [
                  item.id,
                  item.name,
                  item.path,
                  item.workspace,
                  item.mime_type,
                ].every((v) => typeof v === "string") &&
                Number.isFinite(item.size) &&
                item.size >= 0,
            )
            .slice(0, 8)
        : [];
      const files = Array.isArray(input.files)
        ? input.files
            .filter(
              (item) =>
                !!item &&
                typeof item.path === "string" &&
                typeof item.workspace === "string",
            )
            .slice(0, 12)
        : [];
      out[profile][id] = { attachments, files };
    }
  }
  return out;
}
