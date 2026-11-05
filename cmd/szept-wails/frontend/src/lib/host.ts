// The Android host (cmd/szept-wails/android, SzeptHost) injects window.szeptHost.
// On the desktop it is missing and every helper here falls back.
import { toast } from "@/ui/toast";

declare global {
  interface Window {
    szeptHost?: {
      setLightBars(light: boolean): void;
      scanQR(): void;
      micGranted(): boolean;
      askMic(): void;
      exportFile(path: string, mime: string): string;
      insets(): string;
    };
    szeptBack?: () => boolean;
  }
}

export const host = window.szeptHost;
export const canScan = () => !!host?.scanQR;

// Android draws the app under the status and navigation bars; the host
// reports how much of the screen they cover.
function applyInsets(i?: Record<string, number>) {
  if (!i) return;
  const st = document.documentElement.style;
  for (const k of ["top", "bottom", "left", "right"]) st.setProperty("--sa-" + k, (i[k] || 0) + "px");
}
if (host) {
  document.documentElement.classList.add("mobile");
  try { applyInsets(JSON.parse(host.insets())); } catch { /* defaults stay 0 */ }
  addEventListener("szept-insets", (e) => applyInsets((e as CustomEvent).detail));
}

// scanQR: the host's camera scanner answers with a "szept-qr" event.
export function scanQR(): Promise<string> {
  return new Promise((resolve) => {
    addEventListener("szept-qr", (e) => resolve((e as CustomEvent).detail?.text || ""), { once: true });
    host!.scanQR();
  });
}

// micOK: voice runs in Go, so Android's microphone permission has to be
// asked for by the host before the first call. Desktop: always yes.
export async function micOK(): Promise<boolean> {
  if (!host || host.micGranted()) return true;
  const got = new Promise<boolean>((r) => addEventListener("szept-mic", (e) => r((e as CustomEvent).detail), { once: true }));
  host.askMic();
  if (await got) return true;
  toast("Bez dostępu do mikrofonu rozmowa nie ruszy. Zezwól na niego w ustawieniach systemu, w uprawnieniach szepta.", "err");
  return false;
}

// exported: a file Go wrote. On Android it sits in the sandbox until the host
// moves it to Downloads; on the desktop it is already where the user chose.
export function exported(path: string, mime = "") {
  if (!host) return toast("Zapisane: " + path, "ok");
  const where = host.exportFile(path, mime);
  toast(where ? "Zapisane w " + where : "Nie udało się zapisać pliku w Pobranych", where ? "ok" : "err");
}
