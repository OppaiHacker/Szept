import { ref, watchEffect } from "vue";
import { prefersLight, reducedMotion } from "./env";

// Dark unless the user picks light or "auto" (follow the desktop). The answer
// is stamped on <html>, so the stylesheet keeps one palette per theme.
export type ThemePref = "auto" | "light" | "dark";

let stored: string | null = null;
try { stored = localStorage.getItem("theme"); } catch { /* opaque origin */ }
export const themePref = ref<ThemePref>((stored as ThemePref) || "dark");

watchEffect(() => {
  const t = themePref.value === "auto" ? (prefersLight.value ? "light" : "dark") : themePref.value;
  document.documentElement.dataset.theme = t;
  window.szeptHost?.setLightBars(t === "light"); // Android: status bar icons follow the theme
});

export function setTheme(pref: ThemePref) {
  // the whole palette crossfades instead of flashing to the other theme
  const html = document.documentElement;
  if (!reducedMotion.value) {
    html.classList.add("theming");
    setTimeout(() => html.classList.remove("theming"), 320);
  }
  themePref.value = pref;
  try { localStorage.setItem("theme", pref); } catch { /* applied, not persisted */ }
}

export const DEFAULT_ACCENT = "2f8a8a";
export const ACCENTS = [DEFAULT_ACCENT, "4a6fa5", "5f7a4a", "b0703a", "8a5a78", "6b7280"];

// The accent is the user's to pick, so readability on top of it has to be
// derived, not assumed: white on amber is unreadable, black on indigo is worse.
// Ink follows the accent's luminance, and when the ink is white the filled
// surfaces are deepened until the label actually carries.
export function applyAccent(hex?: string) {
  const clean = /^[0-9a-fA-F]{6}$/.test(hex || "") ? hex! : DEFAULT_ACCENT;
  const lin = (c: number) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
  const [r, g, b] = [0, 2, 4].map((i) => lin(parseInt(clean.slice(i, i + 2), 16) / 255));
  const pale = 0.2126 * r + 0.7152 * g + 0.0722 * b > 0.32;

  const css = document.documentElement.style;
  css.setProperty("--accent", "#" + clean);
  css.setProperty("--on-accent", pale ? "#101319" : "#fff");
  css.setProperty("--accent-fill", pale ? "#" + clean : `color-mix(in oklab, #${clean} 82%, #000)`);
}
