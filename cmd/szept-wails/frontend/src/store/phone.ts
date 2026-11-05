import { narrow } from "@/lib/env";
import { closeLightbox, lightboxSrc } from "@/ui/lightbox";
import { closeModal, modalOpen } from "@/ui/modal";
import { closePopover, popOpen } from "@/ui/popover";
import { S } from "./state";
import { leaveChat, openChat, setFocus } from "./actions";
import { closeSettings, holdUnsaved } from "./settings";

// The phone works like Discord's app: four tabs at the bottom, a chat that
// slides in over the list and can be pushed back with a thumb, the member list
// as a sheet from the right. `narrow` decides the layout (a narrow desktop
// window gets it too); S.boot.mobile marks the Android build itself.

export type Tab = "servers" | "dms" | "friends" | "you";

export function curTab(): Tab {
  if (S.tab === "you") return "you";
  if (S.space !== "home") return "servers";
  return S.fpage ? "friends" : "dms";
}

export function goTab(t: Tab) {
  if (t === "servers") {
    const chats = S.snap!.chats;
    const g = chats.find((c) => c.id === S.lastGroup && !c.direct) || chats.find((c) => !c.direct);
    return leaveChat({ space: g ? g.id : "servers", fpage: false, tab: "" });
  }
  leaveChat(t === "you" ? { fpage: false, tab: "you" } : { space: "home", fpage: t === "friends", tab: "" });
}

export const closeSearch = () => { S.search = null; S.hsOpen = false; };

// The system back button walks back through what is open, the way the
// Discord app does; at the top it returns false and the host hides the app.
window.szeptBack = () => {
  if (popOpen()) return closePopover(), true;
  if (lightboxSrc.value) return closeLightbox(), true;
  if (modalOpen()) return closeModal(null), true;
  if (S.settingsOpen) {
    if (S.setPaged && narrow.value) { if (!holdUnsaved()) S.setPaged = false; }
    else closeSettings();
    return true;
  }
  if (S.focus) return setFocus(S.full ? S.focus : null), true;
  if (S.mMembers) return (S.mMembers = false), true;
  if (S.search) return closeSearch(), true;
  if (S.chat || S.voice) return leaveChat({ fpage: false }), true;
  if (S.stage === "ready" && curTab() !== "servers") return goTab("servers"), true;
  return false;
};

// Swipes, phone only. In a chat: right drags it away to the list, left pulls
// the member sheet. On the list: left brings the last chat back. The axis is
// locked after 10px so a vertical scroll never turns into a swipe.
(() => {
  let x0 = 0, y0 = 0, t0 = 0, dx = 0, axis: "x" | "y" | null = null;
  let mode: "chat" | "members" | "list" | null = null, pane: HTMLElement | null = null;
  const busy = () => S.settingsOpen || popOpen() || modalOpen() || !!lightboxSrc.value;
  document.addEventListener("touchstart", (e) => {
    mode = null;
    const shell = document.getElementById("shell");
    if (!narrow.value || !shell || e.touches.length !== 1 || busy()) return;
    if ((e.target as Element).closest("input[type=range], textarea, .vtiles, .crop-stage, pre, .tabs, .menu-reacts")) return;
    const t = e.touches[0];
    x0 = t.clientX; y0 = t.clientY; t0 = e.timeStamp; dx = 0; axis = null;
    const cl = shell.classList;
    const inChat = cl.contains("in-chat") && !cl.contains("page");
    mode = cl.contains("with-members") ? "members" : inChat ? "chat" : !cl.contains("in-chat") && S.lastChat ? "list" : null;
    pane = document.getElementById(mode === "chat" ? "conv" : mode === "members" ? "members" : "");
  }, { passive: true });
  document.addEventListener("touchmove", (e) => {
    if (!mode) return;
    const t = e.touches[0];
    dx = t.clientX - x0;
    const dy = t.clientY - y0;
    if (!axis) {
      if (Math.abs(dx) < 10 && Math.abs(dy) < 10) return;
      axis = Math.abs(dx) > Math.abs(dy) * 1.2 ? "x" : "y";
      if (axis === "x" && pane) pane.style.transition = "none";
    }
    if (axis !== "x" || !pane || dx <= 0) return;
    pane.style.transform = `translateX(${dx}px)`;
  }, { passive: true });
  const end = (e: TouchEvent) => {
    if (!mode || axis !== "x") { mode = null; return; }
    const v = dx / Math.max(1, e.timeStamp - t0); // px per ms
    const far = (w: number) => dx > w * 0.33 || (v > 0.45 && dx > 40);
    const back = (el: HTMLElement | null) => { if (el) { el.style.transition = ""; el.style.transform = ""; } };
    if (mode === "chat" && far(pane!.offsetWidth)) { back(pane); leaveChat({ fpage: false }); }
    else if (mode === "members" && far(pane!.offsetWidth)) { back(pane); S.mMembers = false; }
    else {
      back(pane);
      if (dx < -60 || v < -0.45) {
        if (mode === "chat" && S.conv && !S.conv.direct && !S.voice) S.mMembers = true;
        if (mode === "list" && S.lastChat && S.snap?.chats.some((c) => c.id === S.lastChat![0])) openChat(...S.lastChat);
      }
    }
    mode = null;
  };
  document.addEventListener("touchend", end);
  document.addEventListener("touchcancel", end);
})();
