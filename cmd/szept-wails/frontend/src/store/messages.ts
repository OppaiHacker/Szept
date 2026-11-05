// What you can do to a message, and sending new ones.
import { api } from "@/api/wails";
import type { Msg } from "@/api/types";
import { copyText, readAsDataURL } from "@/lib/files";
import { exported } from "@/lib/host";
import { confirmBox } from "@/ui/dialogs";
import { fail, toast } from "@/ui/toast";
import { S } from "./state";
import { focusComposer } from "./actions";

const run = (p: Promise<unknown>) => p.catch(fail);

export const react = (id: string, emoji: string) => run(api("React", S.chat, id, emoji));
export const pin = (id: string, unpin: boolean) => run(api("Pin", S.chat, id, unpin));
export const hide = (id: string) => run(api("HideMsg", S.chat, id));

export async function remove(id: string) {
  if (await confirmBox("Usunąć wiadomość?", "Zniknie u wszystkich w czacie.")) run(api("DeleteMsg", S.chat, id));
}

export function setReply(m: Msg | null) {
  S.reply = m;
  if (m) focusComposer();
}

export async function copy(t: string) {
  if (await copyText(t)) toast("Skopiowane", "ok"); else toast("Nie udało się skopiować", "err");
}

// a file someone sent: fetch it from them, or save the copy we have
export async function fetchFile(m: Msg) {
  try { await api("Fetch", S.chat, m.blob, m.mime, m.size, m.author); toast("Pobieram…"); } catch (e) { fail(e); }
}
export async function saveFile(m: Msg) {
  try {
    const p = await api<string>("SaveAs", m.blob, m.file);
    if (p) exported(p, m.mime);
  } catch (e) { fail(e); }
}

export async function sendFile(file: File, caption = "") {
  if (!S.chat) return toast("Najpierw wybierz czat.", "err");
  try {
    toast("Wysyłam " + file.name);
    await api("SendData", S.chat, file.name || "obraz.png", await readAsDataURL(file), caption);
  } catch (e) { fail(e); }
}

// send: text and staged files. The text rides as the first file's caption,
// unless it answers someone. False when nothing went out (the caller keeps the text).
export async function send(text: string): Promise<boolean> {
  if (!S.chat) { toast("Najpierw wybierz czat.", "err"); return false; }
  api("Typing", S.chat, false).catch(() => {});
  const reply = S.reply;
  setReply(null);
  if (S.files.length) {
    const files = S.files;
    S.files = [];
    for (const [i, f] of files.entries()) await sendFile(f, i || reply ? "" : text);
    if (!text || !reply) return true;
  }
  try {
    await (reply ? api("SendReply", S.chat, text, reply.id) : api("Send", S.chat, text));
    return true;
  } catch (e) {
    fail(e);
    setReply(reply);
    return false;
  }
}
