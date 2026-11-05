// Window-wide behaviour that belongs to no single pane.
import { closeLightbox, lightboxSrc } from "@/ui/lightbox";
import { closeModal, modalOpen } from "@/ui/modal";
import { closePopover, popOpen } from "@/ui/popover";
import { fail } from "@/ui/toast";
import { S } from "./state";
import { setFocus } from "./actions";
import { closeSettings } from "./settings";

// A rejected call that nobody caught is still a broken button: say so out
// loud instead of leaving the UI silently doing nothing.
window.addEventListener("unhandledrejection", (e) => fail(e.reason));
window.addEventListener("error", (e) => fail(e.message));

// The webview's own menu (reload, inspect element) is the browser leaking
// through; it shows only in developer mode. Our menus prevent it themselves.
document.addEventListener("contextmenu", (e) => { if (!S.snap?.me.dev) e.preventDefault(); });

// A drop must never navigate the webview away. Files never get here: the
// native layer takes them (see "files-dropped" in live.ts).
for (const ev of ["dragenter", "dragover", "drop"]) document.addEventListener(ev, (e) => e.preventDefault());

document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    if (popOpen()) return closePopover();
    if (modalOpen()) return closeModal(null);
    if (lightboxSrc.value) return closeLightbox();
    if (S.settingsOpen) return closeSettings();
    if (S.focus) return setFocus(S.full ? S.focus : null);
  }
  const mod = e.ctrlKey || e.metaKey;
  if (mod && e.key === "f" && S.chat) { e.preventDefault(); document.getElementById("hs")?.focus(); }
  if (mod && e.key === "k") { e.preventDefault(); document.getElementById("q")?.focus(); }
});
