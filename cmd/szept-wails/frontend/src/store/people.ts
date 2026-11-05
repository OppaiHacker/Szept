// Friends and members: writing to them, adding and removing them.
import { api } from "@/api/wails";
import { confirmBox } from "@/ui/dialogs";
import { closeModal } from "@/ui/modal";
import { fail, toast } from "@/ui/toast";
import { S } from "./state";
import { openChat, refresh } from "./actions";

// dmWith: a member you have not befriended yet becomes a friend first: a
// direct chat is only made with friends.
export async function dmWith(pub: string) {
  closeModal(null);
  const f = S.snap!.friends.find((x) => x.pub === pub);
  try {
    if (!f && S.conv) await api("Befriend", S.conv.id, pub);
    openChat(f?.chat || await api<string>("CreateDirect", pub));
  } catch (e) { fail(e); }
}

export async function befriend(pub: string) {
  try { await api("Befriend", S.conv!.id, pub); toast("Dodano do znajomych"); refresh(); } catch (e) { fail(e); }
}

export async function unfriend(pub: string) {
  closeModal(null);
  if (await confirmBox("Usunąć ze znajomych?", "Historia rozmów zostaje. Znika tylko wpis w książce adresowej.")) {
    try { await api("DelFriend", pub); } catch (e) { fail(e); }
  }
}

export async function kick(pub: string) {
  closeModal(null);
  if (await confirmBox("Wyrzucić z czatu?", "Klucz zostanie obrócony, żeby usunięty nie czytał dalej.")) {
    try { await api("Kick", S.chat, pub); } catch (e) { fail(e); }
  }
}
