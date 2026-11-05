// Openers for every dialog and popover, so a button anywhere is one call away
// from its sheet. The sheets themselves live in components/dialogs and
// components/menus.
import { api } from "@/api/wails";
import type { ProfileCard } from "@/api/types";
import { readAsDataURL, pickFiles } from "@/lib/files";
import { openModal } from "@/ui/modal";
import { closePopover, openPopover } from "@/ui/popover";
import { fail, toast } from "@/ui/toast";
import { S } from "./state";
import { chatById } from "./getters";
import { addChannel, openChat } from "./actions";

import NewGroupDialog from "@/components/dialogs/NewGroupDialog.vue";
import JoinDialog from "@/components/dialogs/JoinDialog.vue";
import MyCodeDialog from "@/components/dialogs/MyCodeDialog.vue";
import InviteDialog from "@/components/dialogs/InviteDialog.vue";
import MembersDialog from "@/components/dialogs/MembersDialog.vue";
import HelpDialog from "@/components/dialogs/HelpDialog.vue";
import ChannelDialog from "@/components/dialogs/ChannelDialog.vue";
import CropDialog from "@/components/dialogs/CropDialog.vue";
import ServerLookDialog from "@/components/dialogs/ServerLookDialog.vue";
import SpatialDialog from "@/components/dialogs/SpatialDialog.vue";
import RelayDialog from "@/components/dialogs/RelayDialog.vue";
import ProfileCardView from "@/components/menus/ProfileCardView.vue";
import { promptBox } from "@/ui/dialogs";

export async function newGroupDialog(name = "") {
  const r = await openModal<{ name: string; history: boolean }>(NewGroupDialog, { name });
  if (!r?.name) return;
  try { openChat(await api<string>("CreateGroup", r.name, r.history)); } catch (e) { fail(e); }
}

export async function joinDialog(code = "") {
  const r = await openModal<{ code: string; pin: string }>(JoinDialog, { code });
  if (!r?.code) return;
  try { await api("Redeem", r.code, r.pin); toast("Zaproszenie przyjęte", "ok"); } catch (e) { fail(e); }
}

export async function addFriend(code: string): Promise<boolean> {
  try { toast("Dodano: " + await api<string>("AddFriend", code), "ok"); return true; } catch (e) { fail(e); return false; }
}

export async function addFriendDialog(prefill = "") {
  const code = prefill || await promptBox({
    title: "Dodaj znajomego", label: "Kod znajomego", placeholder: "SZF-…",
    hint: "Kod dostaniesz od drugiej osoby. U niej to „Twój kod”.", ok: "Dodaj",
  });
  if (code) await addFriend(code);
}

export async function myCodeDialog() {
  const qr = await api<string>("QR", S.snap!.me.code);
  const code = await openModal<string>(MyCodeDialog, { qr });
  if (code) await addFriend(code);
}

export async function inviteDialog() {
  if (!S.chat) return toast("Najpierw wybierz czat.", "err");
  try { openModal(InviteDialog, { inv: await api("NewInvite", S.chat) }); } catch (e) { fail(e); }
}

export function membersDialog() {
  if (!S.conv) return toast("Najpierw wybierz czat.", "err");
  openModal(MembersDialog);
}

export const helpDialog = () => openModal(HelpDialog, {}).then(() => {});

export async function renameDialog(arg = "") {
  const name = arg || await promptBox({ title: "Zmień nazwę", label: "Nazwa czatu", value: S.conv?.name || "" });
  if (!name) return;
  try { await api("Rename", S.chat, name); } catch (e) { fail(e); }
}

// a channel of the open group; the category and kind preselect the dialog
export async function channelDialog(cat: string, kind: "text" | "voice") {
  const r = await openModal<{ name: string; kind: "text" | "voice" }>(ChannelDialog, { kind });
  if (r?.name) addChannel(r.kind, r.name, cat);
}

export async function categoryDialog() {
  const name = await promptBox({ title: "Utwórz kategorię", label: "Nazwa kategorii", placeholder: "nowa kategoria", ok: "Utwórz" });
  if (name?.trim()) addChannel("cat", name.trim(), "");
}

// pickPicture: file chooser, then pan/zoom framing, then Go.
// kind: avatar | banner, or with chat set a group's icon | banner
export async function pickPicture(kind: "avatar" | "banner" | "icon", chat?: string) {
  closePopover(); // a profile card would sit on top of the crop dialog
  const [file] = await pickFiles({ accept: "image/*" });
  if (!file) return;
  try { await openModal(CropDialog, { kind, chat, src: await readAsDataURL(file) }); } catch (e) { fail(e); }
  if (chat) serverLook(chat);
}

export function serverLook(chat: string) {
  if (chatById(chat)) openModal(ServerLookDialog, { chat });
}

export function spatialDialog() {
  if (!S.conv) return;
  if (!S.conv.members.some((m) => m.inCall && !m.me)) return toast("Nikogo jeszcze nie ma w rozmowie.");
  openModal(SpatialDialog);
}

// relayDialog: what a relay operator can and cannot do. ask: the user is about
// to turn relay-only on and has to agree.
export const relayDialog = (ask = false) => openModal<boolean>(RelayDialog, { ask });

// A profile opens as a card next to whatever you clicked (a name, a member,
// a friend). Without an anchor, e.g. from a slash command, it is a dialog.
export async function profilePop(pub: string, anchor?: Element | null) {
  let p: ProfileCard;
  try { p = await api<ProfileCard>("ProfileOf", pub); } catch (e) { return fail(e); }
  if (anchor?.isConnected && !anchor.closest(".modal, .pop")) {
    const side = anchor.closest(".userbar") ? "top" : anchor.closest(".members") ? "left" : "right";
    return openPopover(anchor, ProfileCardView, { p }, side, "card");
  }
  // the card is the whole sheet, so its frame and effect cover all of it
  closePopover();
  openModal(ProfileCardView, { p, sheet: true });
}

