// Openers for the popover menus and pickers (components/menus).
import type { Channel } from "@/api/types";
import { openPopover, type Anchor } from "@/ui/popover";
import { S } from "./state";
import UserMenu from "@/components/menus/UserMenu.vue";
import MsgMenu from "@/components/menus/MsgMenu.vue";
import ChannelMenu from "@/components/menus/ChannelMenu.vue";
import ChatMenu from "@/components/menus/ChatMenu.vue";
import EmojiPicker from "@/components/menus/EmojiPicker.vue";
import GifPicker from "@/components/menus/GifPicker.vue";
import PinsPop from "@/components/menus/PinsPop.vue";

// userMenu: what you can do with a person. Your own entry carries your call
// controls instead.
export function userMenu(pub: string, at: Anchor) {
  if (S.conv?.members.some((p) => p.pub === pub)) openPopover(at, UserMenu, { pub }, "bottom", "menu-pop");
}

// msgMenu: a right click on a message: the hover tools plus what does not fit there
export function msgMenu(id: string, at: Anchor) {
  const m = S.conv?.msgs.find((x) => x.id === id);
  if (m && m.kind !== "info") openPopover(at, MsgMenu, { id }, "bottom", "menu-pop");
}

// channelMenu: the owner's menu on the channel list. x is the channel or
// category under the pointer; cat and kind preselect a new channel.
export function channelMenu(at: Anchor, x: Channel | undefined, cat: string, kind: string) {
  openPopover(at, ChannelMenu, { x, cat, kind }, "bottom", "menu-pop");
}

export function chatMenu(at: Anchor) {
  if (S.conv) openPopover(at, ChatMenu, {}, "bottom", "menu-pop");
}

export const emojiPicker = (at: Anchor, pick: (e: string) => void) => openPopover(at, EmojiPicker, { pick }, "top", "emoji-pop");
export const gifPicker = (at: Anchor) => openPopover(at, GifPicker, {}, "top", "gif-pop");
export const pinsPop = (at: Anchor) => openPopover(at, PinsPop, {}, "bottom", "pins-pop");
