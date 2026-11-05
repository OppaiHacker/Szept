import { ref, shallowRef } from "vue";
import { api } from "@/api/wails";
import type { Settings } from "@/api/types";
import { micOK } from "@/lib/host";
import { applyAccent } from "@/lib/theme";
import { closeModal } from "@/ui/modal";
import { fail, toast } from "@/ui/toast";
import { S } from "./state";
import { refresh } from "./actions";

// Settings float over the app as one big sheet: the chat stays underneath and
// keeps syncing, so closing it lands exactly where you were.
export async function openSettings(section = "profile", paged = false) {
  closeModal(null);
  S.section = section;
  S.setPaged = paged; // on a phone: straight into the section, not the list
  S.settings = await api<Settings>("Settings");
  S.settingsOpen = true;
}

// Unsaved edits hold the layer open: the bar shakes instead, the way it says
// "save or reset first". dirty is the settings block with the edited field.
export const dirty = shallowRef<HTMLElement | null>(null);
export const shakes = ref(0);

export function holdUnsaved(): boolean {
  if (!dirty.value) return false;
  shakes.value++;
  return true;
}

export function closeSettings() {
  if (holdUnsaved()) return;
  S.draftStyle = undefined;
  S.settingsOpen = false;
  if (S.micTest) micTest(false);
  if (S.camTest) camTest(false);
}

export function goSection(id: string) {
  if (holdUnsaved()) return;
  S.section = id;
  S.setPaged = true;
}

export async function saveSettings(patch: Partial<Settings> = {}) {
  try {
    await api("SaveSettings", { ...S.settings, ...patch });
    toast("Zapisane", "ok");
  } catch (e) { fail(e); }
  S.settings = await api<Settings>("Settings");
  applyAccent(S.settings.accent);
  await refresh();
}

export const reloadSettings = async () => { S.settings = await api<Settings>("Settings"); };

// camTest: live preview of the saved camera in the voice settings.
export async function camTest(on: boolean) {
  S.camTest = on;
  try { await api("CamTest", on); } catch (e) { S.camTest = false; fail(e); }
}

// micTest: the level meter in the voice settings. It reads your own entry of
// VoiceLevels, so in a call it shows the call's microphone instead.
export const micLevel = ref(0);
let meterTimer = 0;
export async function micTest(on: boolean) {
  clearInterval(meterTimer);
  micLevel.value = 0;
  S.micTest = on;
  if (on && !(await micOK())) { S.micTest = false; return; }
  try { await api("MicTest", on); } catch (e) { S.micTest = false; fail(e); }
  if (!S.micTest) return;
  meterTimer = window.setInterval(async () => {
    const lv = (await api<Record<string, number>>("VoiceLevels").catch(() => ({} as Record<string, number>)))[S.snap!.me.pub] || 0;
    micLevel.value = Math.min(1, lv * 2.5);
  }, 80);
}
