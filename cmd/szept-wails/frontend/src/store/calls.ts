import { api } from "@/api/wails";
import type { VideoOpts } from "@/api/types";
import { micOK } from "@/lib/host";
import { openPopover } from "@/ui/popover";
import { S } from "./state";
import { chatById } from "./getters";
import { act, openVoice, reapplyPeers } from "./actions";
import VideoPick from "@/components/menus/VideoPick.vue";

// joinVoice: a voice channel joins its call and shows it; Discord does the
// same. Your mic and headphone choice from the account strip carries over.
export async function joinVoice(chat: string, ch: string) {
  const key = chat + ch;
  if (chatById(chat)?.callKey !== key) {
    if (!(await micOK())) return;
    await act("JoinCall", key);
    reapplyPeers();
    if (S.selfMute) await act("ToggleMic", key);
    if (S.selfDeaf) await act("ToggleDeafen", key);
  }
  await openVoice(key);
}

// Starting a camera or screen share asks how: monitor (when there is a
// choice), frame rate, resolution, quality. The last pick is preselected.
// Stopping, and the slash commands (no anchor), skip the question.
export async function startVideo(anchor: Element | null, kind: "cam" | "screen", key: string) {
  const method = kind === "cam" ? "ToggleCam" : "ToggleScreen";
  const pre = (o: VideoOpts) => (kind === "cam" ? [o] : [0, o]);
  if (S.snap!.chats.find((c) => c.callKey === key)?.call?.[kind]) return act(method, key, ...pre({}));
  const [picks, mons] = await Promise.all([
    api<Record<string, VideoOpts>>("VideoPicks").catch(() => ({} as Record<string, VideoOpts>)),
    kind === "screen" ? api<string[]>("Displays").catch(() => []) : Promise.resolve([] as string[]),
  ]);
  const p = picks[kind] || {};
  if (!anchor) return act(method, key, ...pre(p));
  openPopover(anchor, VideoPick, { kind, callKey: key, picks: p, monitors: mons }, "top", "menu-pop");
}
