import type { ChatItem } from "./protocol";

export interface ConversationMatch {
  itemId: string;
  offset: number;
  snippet: string;
}
export function searchableText(item: ChatItem): string {
  return item.kind === "tool"
    ? Array.from(
        new Set([
          item.title,
          JSON.stringify(item.rawInput || {}),
          item.terminalOutput || "",
          ...item.content.map((part) => part.text || ""),
          JSON.stringify(item.details || {}),
        ]),
      ).join("\n")
    : item.text;
}
export function findConversation(
  items: ChatItem[],
  query: string,
): ConversationMatch[] {
  if (!query.trim()) return [];
  const needle = query.toLocaleLowerCase();
  const matches: ConversationMatch[] = [];
  for (const item of items) {
    const text = searchableText(item),
      folded = text.toLocaleLowerCase();
    let offset = 0;
    while ((offset = folded.indexOf(needle, offset)) >= 0) {
      matches.push({
        itemId: item.id,
        offset,
        snippet: text.slice(
          Math.max(0, offset - 40),
          offset + query.length + 100,
        ),
      });
      offset += Math.max(1, needle.length);
      if (matches.length >= 5000) return matches;
    }
  }
  return matches;
}
