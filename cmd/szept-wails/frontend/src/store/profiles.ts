import { api } from "@/api/wails";
import { confirmBox, promptBox } from "@/ui/dialogs";
import { fail } from "@/ui/toast";
import { boot } from "./actions";

// Accounts on this device: each is its own data directory and identity.
// Switching one restarts the app's core, so everything boots again.
export async function useProfile(name: string) {
  try { await api("UseProfile", name); await boot(); } catch (e) { fail(e); }
}

export async function newProfile() {
  const name = await promptBox({
    title: "Nowe konto", label: "Nazwa", placeholder: "np. praca",
    hint: "Małe litery, cyfry i myślniki. Aplikacja uruchomi się od nowa.", ok: "Utwórz",
  });
  if (name) await useProfile(name);
}

export async function switchProfile(name: string) {
  if (await confirmBox("Przełączyć konto?", "Aplikacja uruchomi się od nowa.", false)) await useProfile(name);
}
