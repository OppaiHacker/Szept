// Message text as pieces for <RichText>: plain text, links and @mentions.
// Vue escapes every piece, so nothing a peer types ever becomes markup.

export type Piece = { t: "text" | "link" | "ping"; v: string };

// @everyone and @here ping the whole chat; @name pings one member.
export const PING_ALL = ["everyone", "here"];

const quote = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

// Names may hold spaces, so mentions are matched against the chat's members,
// longest first, and only where an @ starts a word.
function mentionRe(names: string[]): RegExp {
  const alts = [...new Set([...PING_ALL, ...names.filter(Boolean)])]
    .sort((a, b) => b.length - a.length).map(quote);
  return new RegExp(`(^|[\\s(])@(${alts.join("|")})(?=$|[\\s.,!?;:)])`, "gi");
}

function splitMentions(text: string, names: string[]): Piece[] {
  if (!text.includes("@")) return [{ t: "text", v: text }];
  const out: Piece[] = [];
  let at = 0;
  for (const m of text.matchAll(mentionRe(names))) {
    const start = m.index! + m[1].length;
    if (start > at) out.push({ t: "text", v: text.slice(at, start) });
    out.push({ t: "ping", v: "@" + m[2] });
    at = start + 1 + m[2].length;
  }
  if (at < text.length) out.push({ t: "text", v: text.slice(at) });
  return out;
}

// pieces: URLs stay clickable (they open outside, never in the webview) and
// mentions get their highlight.
export function pieces(text: string, names: string[]): Piece[] {
  const out: Piece[] = [];
  let at = 0;
  for (const m of text.matchAll(/\bhttps?:\/\/[^\s<]+/g)) {
    if (m.index! > at) out.push(...splitMentions(text.slice(at, m.index), names));
    out.push({ t: "link", v: m[0] });
    at = m.index! + m[0].length;
  }
  if (at < text.length) out.push(...splitMentions(text.slice(at), names));
  return out;
}

// pingsMe: the text mentions my name or the whole chat
export function pingsMe(text: string | undefined, myName: string): boolean {
  if (!text?.includes("@")) return false;
  const t = text.toLowerCase();
  return [...PING_ALL, (myName || "").toLowerCase()].some((n) => n &&
    new RegExp(`(^|[\\s(])@${quote(n)}(?=$|[\\s.,!?;:)])`).test(t));
}
