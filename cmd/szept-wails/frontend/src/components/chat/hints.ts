import type { Conversation } from "@/api/types";
import { COMMANDS } from "@/store/commands";

// A suggestion over the composer. A command replaces the whole line; a
// mention (at = the "@…" span) only its own token.
export interface Hint { n: string; a?: string; d: string; at?: [number, number] }

export function matchCommands(text: string): Hint[] {
  if (!text.startsWith("/") || text.includes(" ")) return [];
  const q = text.toLowerCase();
  return COMMANDS.filter((c) => c.n.startsWith(q)).slice(0, 7);
}

// matchMentions: "@par" right before the caret -> members (not me) and
// @everyone/@here whose name starts that way
export function matchMentions(value: string, caret: number, conv: Conversation | null): Hint[] {
  const before = value.slice(0, caret);
  const tok = before.match(/(^|\s)@([^\s@]*)$/);
  if (!tok || !conv) return [];
  const q = tok[2].toLowerCase(), from = before.length - tok[2].length - 1;
  const people = conv.members.filter((m) => !m.me).map((m) => ({ n: "@" + m.name, d: m.online ? "online" : "" }));
  const all = conv.direct ? [] : [{ n: "@everyone", d: "wszyscy na serwerze" }, { n: "@here", d: "wszyscy, którzy są online" }];
  return [...people, ...all].filter((h) => h.n.slice(1).toLowerCase().startsWith(q))
    .slice(0, 7).map((h) => ({ ...h, at: [from, caret] as [number, number] }));
}

// applyHint returns the new text and caret
export function applyHint(value: string, h: Hint): [string, number] {
  if (!h.at) { const v = h.n + (h.a ? " " : ""); return [v, v.length]; }
  const [a, b] = h.at;
  return [value.slice(0, a) + h.n + " " + value.slice(b), a + h.n.length + 1];
}
