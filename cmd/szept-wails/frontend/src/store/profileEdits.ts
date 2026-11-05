// Edits to your own profile card. Everything is signed with your key and
// reaches every chat you are in.
import { api } from "@/api/wails";
import { promptBox } from "@/ui/dialogs";
import { fail } from "@/ui/toast";
import { S } from "./state";
import { afterEdit } from "./actions";

async function edit(method: string, v: string | null) {
  if (v === null) return;
  try { await api(method, v); await afterEdit("Zapisane"); } catch (e) { fail(e); }
}

export async function editNick() {
  const v = await promptBox({ title: "Nazwa", label: "Jak mają cię widzieć", value: S.snap!.me.name });
  if (v) await edit("SetNick", v);
}
export async function editStatus() {
  await edit("SetStatus", await promptBox({ title: "Status", label: "Krótka linijka", value: S.snap!.me.status, placeholder: "np. koduję" }));
}
export async function editBio() {
  await edit("SetBio", await promptBox({ title: "Opis", label: "O tobie", value: S.snap!.me.bio, multiline: true }));
}
