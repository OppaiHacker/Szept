import type { Snapshot } from "@/api/types";
import { api } from "@/api/wails";
import { sfx } from "@/lib/sounds";
import { pingsMe } from "@/lib/text";
import { toast } from "@/ui/toast";
import { S } from "./state";

// Everything here diffs two snapshots, so a cue plays however the change
// happened: button, slash command or the peer's side.

let lastCall: { id: string; mic: boolean; deaf: boolean; cam: boolean; screen: boolean; peers: number } | null = null;
let lastUnread: number | null = null;

export function callSounds(snap: Snapshot) {
  const c = snap.chats.find((x) => x.call);
  const now = c ? { id: c.id, ...c.call!, peers: c.inCall.length } : null;
  const was = lastCall;
  lastCall = now;
  if (lastUnread !== null && snap.unread > lastUnread) sfx("msg");
  lastUnread = snap.unread;
  if (!was && !now) return;
  if (!was || !now || was.id !== now.id) return sfx(now ? "join" : "leave");
  if (now.deaf !== was.deaf) sfx(now.deaf ? "deaf" : "undeaf");
  else if (now.mic !== was.mic) sfx(now.mic ? "unmute" : "mute");
  if (now.screen !== was.screen) sfx(now.screen ? "streamOn" : "streamOff");
  else if (now.cam !== was.cam) sfx(now.cam ? "streamOn" : "streamOff");
  if (now.peers !== was.peers) sfx(now.peers > was.peers ? "peerJoin" : "peerLeave");
}

// notifyPings: a chat's unread count went up and its newest message is a DM or
// mentions me -> system notification, unless I am looking at that chat.
// ponytail: checks only the newest message; a ping followed by a plain message
// in the same refresh slips by. Count mentions in Go if that matters.
let lastUnreadBy: Record<string, number> | null = null;
export function notifyPings(snap: Snapshot) {
  const was = lastUnreadBy;
  lastUnreadBy = Object.fromEntries(snap.chats.map((c) => [c.id, c.unread]));
  if (!was || S.settings?.noNotify) return;
  for (const c of snap.chats) {
    if (!(c.unread > (was[c.id] ?? 0)) || (c.id === S.chat && document.hasFocus())) continue;
    if (c.direct || pingsMe(c.preview, snap.me.name)) api("Notify", c.id, c.name, c.preview).catch(() => {});
  }
}

// Tell once per person and session when a link goes through a relay, also
// when it was the other side who chose relay-only.
const relayWarned = new Set<string>();
export function warnRelayed(people: { pub: string; name: string; relay: string }[]) {
  for (const m of people) {
    if (!m.relay || relayWarned.has(m.pub)) continue;
    relayWarned.add(m.pub);
    toast(`Połączenie z ${m.name} idzie przez przekaźnik connectora (${m.relay}). Operator widzi, że rozmawiacie, ale nie treść.`, "warn");
  }
}
