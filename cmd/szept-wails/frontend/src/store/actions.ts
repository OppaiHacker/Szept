import { nextTick } from "vue";
import { api } from "@/api/wails";
import type { BootInfo, Channel, Chat, Conversation, Snapshot, UpdateInfo } from "@/api/types";
import { autoFocus, narrow } from "@/lib/env";
import { errText } from "@/lib/format";
import { applyAccent } from "@/lib/theme";
import { fail, toast } from "@/ui/toast";
import { S, peerVol } from "./state";
import { chatById } from "./getters";
import { callSounds, notifyPings, warnRelayed } from "./notify";

/* ---------------------------- syncing ---------------------------- */

let refreshing = false, again = false;
export const isRefreshing = () => refreshing;

// refresh pulls a fresh snapshot (and the open chat). Calls that land while
// one is running fold into a single follow-up.
export async function refresh() {
  if (refreshing) { again = true; return; }
  refreshing = true;
  try {
    const snap = await api<Snapshot>("Snapshot");
    callSounds(snap);
    notifyPings(snap);
    warnRelayed(snap.friends);
    S.snap = snap;
    settleSpace();
    await refreshConv();
  } catch (e) {
    if (errText(e) !== "nie zalogowano") fail(e);
  } finally {
    refreshing = false;
    if (again) { again = false; refresh(); }
  }
}

export async function refreshConv() {
  if (!S.chat) { S.conv = null; return; }
  try {
    S.conv = await api<Conversation>("Conversation", S.chat, S.limit);
  } catch {
    S.conv = null;
    S.chat = "";
  }
}

// settleSpace: an open chat decides which space you are in; a space that went
// away (left the group) falls back home.
function settleSpace() {
  const chats = S.snap!.chats;
  const cur = chats.find((c) => c.id === S.chat);
  if (cur) S.space = cur.direct ? "home" : cur.id;
  else if (S.space !== "home" && S.space !== "servers" && !chats.some((c) => c.id === S.space)) S.space = "home";
  if (S.space === "servers") { // phone: the Serwery tab opens on a server as soon as there is one
    const g = chats.find((c) => !c.direct);
    if (g) S.space = g.id;
  }
  if (S.space !== "home" && S.space !== "servers") S.lastGroup = S.space;
}

// act: fire a bound method and surface its error as a toast.
export async function act(method: string, ...args: unknown[]) {
  try { await api(method, ...args); refresh(); } catch (e) { fail(e); }
}

// afterEdit: a profile change is not pushed by core fast enough to beat the
// user's eye, so pull once.
export async function afterEdit(msg?: string) {
  if (msg) toast(msg, "ok");
  await refresh();
}

/* ---------------------------- navigation ---------------------------- */

export const focusComposer = () => nextTick(() => autoFocus(document.getElementById("input")));

// openChat with no channel lands on the default text channel, or on the first
// live one once the default is archived or deleted.
export async function openChat(id: string, ch?: string) {
  if (!id) return;
  if (ch === undefined) {
    const g = chatById(id);
    const live = g?.channels.filter((x) => x.kind === "text" && !x.arch) || [];
    ch = g && !g.direct && !live.some((x) => !x.id) && live[0] ? live[0].id : "";
  }
  if (id !== S.chat) {
    const n = chatById(id)?.unread || 0;
    S.newMark = n ? { chat: id, n } : null;
  }
  if (id !== S.chat || ch !== S.ch) { S.reply = null; S.files = []; }
  S.chat = id;
  S.ch = ch;
  S.voice = null;
  S.tab = "";
  if (S.focus) setFocus(null);
  await api("OpenChannel", id, ch);
  S.search = null;
  S.limit = 60;
  await api("Open", id);
  await refresh();
  focusComposer();
}

// leaveChat closes the open chat and lands on a sidebar or the friends page.
export function leaveChat(to: Partial<Pick<typeof S, "space" | "fpage" | "tab">> = {}) {
  if (S.chat) S.lastChat = [S.chat, S.ch];
  Object.assign(S, { chat: "", ch: "", voice: null, search: null, filter: "", mMembers: false, conv: null }, to);
  if (S.focus) setFocus(null);
  api("Open", "").catch(() => {});
  refresh();
}

// openVoice shows the voice view of a call key (chat ID + channel ID).
export async function openVoice(key: string) {
  const chat = key.slice(0, 64);
  if (S.chat !== chat) await openChat(chat);
  S.voice = key;
}

// a phone shows the channel list first, a desktop goes straight to the chat
export function openSpace(id: string) {
  if (narrow.value) return leaveChat({ space: id, fpage: false });
  openChat(id);
}

/* ---------------------------- channels ---------------------------- */

export async function putChannel(id: string, kind: Channel["kind"], name: string, cat: string, arch = false) {
  try { await api("PutChannel", S.space, id, kind, name, cat, arch); await refresh(); } catch (e) { fail(e); }
}
export async function addChannel(kind: Channel["kind"], name: string, cat: string) {
  try { await api("AddChannel", S.space, kind, name, cat); await refresh(); } catch (e) { fail(e); }
}

// dropChannel archives (name kept) or deletes (name "") channel x, first
// leaving its call and its message list if you are in them.
export async function dropChannel(g: Chat, x: Channel, name: string, arch: boolean) {
  if (x.kind === "voice" && g.callKey === g.id + x.id) await act("LeaveCall", g.callKey);
  await putChannel(x.id, x.kind, name, x.cat, arch);
  if (x.kind === "text" && S.chat === g.id && S.ch === x.id) await openChat(g.id);
}

/* ---------------------------- calls ---------------------------- */

// setPeer changes how one person sounds in calls. A new call starts a new
// mixer, so reapplyPeers pushes everything again on every join.
export function setPeer(pub: string, patch: Partial<{ gain: number; pan: number; mute: boolean; pos: [number, number] }>) {
  const v = { gain: 1, pan: 0, mute: false, ...peerVol.get(pub), ...patch };
  peerVol.set(pub, v);
  api("SetVoice", pub, v.gain, v.pan).catch(() => {});
  api("MutePeer", pub, v.mute).catch(() => {});
}
export const reapplyPeers = () => { for (const pub of peerVol.keys()) setPeer(pub, {}); };

// setFocus: a click on a video tile blows it up over the stage (the rest
// shrink to a strip), full also covers the whole screen. null goes back.
export function setFocus(key: string | null, full = false) {
  if (S.full !== (full && !!key)) api("SetFullscreen", full && !!key).catch(() => {});
  S.focus = key;
  S.full = full && !!key;
}

/* ---------------------------- search ---------------------------- */

// Search works the way the big chat apps do it: type in the header, hits
// stack up in a side panel, and each one jumps back to its place in the chat.
export async function searchChat(q: string) {
  q = q.trim();
  if (!q || !S.chat) { S.search = null; return; }
  try {
    S.search = { q, chat: S.chat, hits: (await api<Conversation["msgs"]>("Search", S.chat, q)).reverse() };
  } catch (e) { fail(e); }
}

const msgEl = (id: string) => document.querySelector<HTMLElement>(`.msg[data-id="${CSS.escape(id)}"]`);

// jumpTo: scroll a message into view, pulling older pages until it is loaded.
export async function jumpTo(id: string) {
  for (let i = 0; i < 20 && S.conv && !msgEl(id); i++) {
    if (S.conv.msgs.length < S.limit) break; // whole history already loaded
    S.limit += 200;
    await refreshConv();
    await nextTick();
  }
  const el = msgEl(id);
  if (!el) return toast("Ta wiadomość jest schowana albo usunięta.");
  el.scrollIntoView({ block: "center" });
  el.classList.remove("flash"); void el.offsetWidth; el.classList.add("flash");
}

/* ---------------------------- boot ---------------------------- */

export async function boot() {
  S.boot = await api<BootInfo>("Boot");
  document.documentElement.classList.toggle("mobile", !!S.boot.mobile);
  S.version = await api<string>("Version");
  applyAccent(S.boot.accent);
  if (S.boot.stage === "ready") {
    S.update = await api<UpdateInfo>("Update");
    S.settings = await api("Settings");
    if (narrow.value) S.space = "servers"; // the phone starts on Serwery, like Discord
    await refresh();
    S.stage = "ready";
  } else {
    S.stage = "gate";
  }
}
