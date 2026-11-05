import { ref } from "vue";

// media queries as refs, so templates follow a window resize
function query(q: string) {
  const mq = matchMedia(q);
  const r = ref(mq.matches);
  mq.addEventListener("change", () => (r.value = mq.matches));
  return r;
}

// keep in step with the 640px breakpoint in styles/responsive.css
export const narrow = query("(max-width: 640px)");
export const reducedMotion = query("(prefers-reduced-motion: reduce)");
export const prefersLight = query("(prefers-color-scheme: light)");

// autoFocus is for focus nobody asked for (a dialog opens, a chat loads). On a
// touch screen it would pop the keyboard up over the page, so it stays off.
export const TOUCH = matchMedia("(pointer: coarse)").matches;
export function autoFocus(el?: Element | null) {
  if (!TOUCH) (el as HTMLElement | null)?.focus();
}

// Where the last press landed. A sheet that grows out of the control you
// pressed keeps the spatial relationship between trigger and content obvious.
export let lastPress: [number, number] | null = null;
document.addEventListener("pointerdown", (e) => { lastPress = [e.clientX, e.clientY]; }, true);

// localStorage can throw in an opaque origin; settings then live for the session
export function load<T>(key: string, fallback: T): T {
  try {
    const v = localStorage.getItem(key);
    return v === null ? fallback : (JSON.parse(v) as T);
  } catch { return fallback; }
}
export function save(key: string, v: unknown) {
  try { localStorage.setItem(key, JSON.stringify(v)); } catch { /* not persisted */ }
}
