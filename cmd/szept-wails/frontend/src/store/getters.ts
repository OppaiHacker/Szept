import { ref } from "vue";
import type { CallInfo, Channel, Chat } from "@/api/types";
import { S, frames } from "./state";

export const me = () => S.snap!.me;
export const chatById = (id: string) => S.snap?.chats.find((c) => c.id === id);
export const curGroup = () => chatById(S.space);
// the chat whose call you are in, if any
export const callChat = () => S.snap?.chats.find((c) => c.call);
export const callKey = () => callChat()?.callKey || S.chat;

// chanOf: a channel of group g. Both defaults have the ID "", so the kind
// tells them apart.
export const chanOf = (g: Chat | undefined, id: string, kind: Channel["kind"]) =>
  g?.channels.find((x) => x.id === id && (id || x.kind === kind));

export function chanName(g: Chat | undefined, id: string, kind: Channel["kind"] = "text") {
  return chanOf(g, id, kind)?.name || (id ? "?" : kind === "voice" ? "Rozmowa" : "czat");
}

// myFlags: your call state in the letters core uses for everyone else
// (m mic off, d deafened, c camera, s screen)
export const myFlags = (k?: CallInfo) =>
  k ? (k.mic ? "" : "m") + (k.deaf ? "d" : "") + (k.cam ? "c" : "") + (k.screen ? "s" : "") : "";

// styleOf: what we know about someone's profile effects from the last snapshot.
// Your own shows the draft while the effects editor is open.
export function styleOf(pub?: string): string {
  if (!pub || !S.snap) return "";
  if (pub === S.snap.me.pub) return S.draftStyle ?? S.snap.me.style ?? "";
  return S.conv?.members.find((m) => m.pub === pub)?.style
    ?? S.snap.friends.find((f) => f.pub === pub)?.style ?? "";
}

// A stream that stopped sending is dropped after 4 s. `now` ticks while
// frames are live so the tiles notice.
export const now = ref(Date.now());
setInterval(() => { if (frames.size) now.value = Date.now(); }, 1500);

export function liveFrames(key: string) {
  const t = now.value;
  return [...frames.entries()].filter(([k, f]) => k.startsWith(key + ".") && t - f.at < 4000);
}
