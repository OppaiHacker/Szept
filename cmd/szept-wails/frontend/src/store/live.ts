// What arrives from Go on its own: events, call frames, voice levels, and the
// slow poll for connector state (it changes without touching the DAG).
import { api, onEvent } from "@/api/wails";
import type { Settings, UpdateInfo } from "@/api/types";
import { dataURLFile } from "@/lib/files";
import { fail, toast } from "@/ui/toast";
import { S, frames, talking } from "./state";
import { focusComposer, isRefreshing, refresh } from "./actions";

onEvent<UpdateInfo>("update", (u) => {
  S.update = u;
  toast(u.installed
    ? `Zainstalowano szepta ${u.version}. Uruchom ponownie w ustawieniach, w zakładce O szepcie.`
    : `Jest szept ${u.version}, ale nie udało się go zainstalować: ${u.err}`, u.installed ? "ok" : "warn");
});

// GTK grabs file drags before the page sees them; Go hands over the paths and
// each file comes back through ReadDropped into the composer.
onEvent<string[]>("files-dropped", async (paths) => {
  if (!S.chat) return toast("Najpierw wybierz czat.", "err");
  const got: File[] = [];
  for (const p of paths) {
    try { got.push(dataURLFile(await api<string>("ReadDropped", p), p.split(/[\\/]/).pop()!)); } catch (e) { fail(e); }
  }
  S.files = [...S.files, ...got];
  focusComposer();
});

onEvent("sync", async () => {
  if (S.stage !== "ready") return;
  refresh();
  // the device list changes when a device links in or comes online
  if (S.settingsOpen && S.section === "devices") {
    const s = await api<Settings>("Settings").catch(() => null);
    if (!s || JSON.stringify(s.devices) === JSON.stringify(S.settings?.devices)) return;
    if ((s.devices || []).length > (S.settings?.devices || []).length) S.linkCode = null; // the code was used
    S.settings = s;
  }
});

onEvent<{ key: string; seq: number; kind: string; from: string }>("frame", (d) => {
  const name = S.conv?.members.find((m) => m.pub === d.from)?.name || frames.get(d.key)?.name || "peer";
  frames.set(d.key, { seq: d.seq, kind: d.kind, name, at: Date.now() });
});
onEvent<string>("frame-end", (k) => frames.delete(k));
onEvent<string>("frames-cleared", (key) => {
  for (const k of [...frames.keys()]) if (k.startsWith(key + ".")) frames.delete(k);
});

// Speaking ring: while you are in a call, everyone heard lately is talking.
// The mic gate (media/denoise.go) only sends speech and holds 400 ms over word
// gaps, so "heard lately" is "talking".
setInterval(async () => {
  const live = S.stage === "ready" && S.snap?.chats.some((c) => c.call);
  const lv = live ? await api<Record<string, number>>("VoiceLevels").catch(() => ({} as Record<string, number>)) : {};
  const on = new Set(Object.keys(lv).filter((k) => lv[k] > 0));
  for (const p of [...talking]) if (!on.has(p)) talking.delete(p);
  for (const p of on) if (!talking.has(p)) talking.add(p);
}, 100);

setInterval(() => { if (S.stage === "ready" && !isRefreshing()) refresh(); }, 4000);
