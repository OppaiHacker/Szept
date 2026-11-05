import type { App, Directive } from "vue";
import { autoFocus, reducedMotion } from "@/lib/env";

// v-focus: focus the element when it appears (not on touch screens).
const focus: Directive<HTMLElement> = { mounted: (el) => autoFocus(el) };

// v-appear="key": new content settles in instead of snapping. It plays when
// the element mounts and again whenever key changes (another chat, tab,
// section); a background repaint of the same view stays still.
function settle(el: HTMLElement) {
  if (reducedMotion.value) return;
  el.animate([{ opacity: 0, transform: "translateY(4px)" }, { opacity: 1, transform: "none" }],
    { duration: 220, easing: "cubic-bezier(.23,1,.32,1)" });
}
const appear: Directive<HTMLElement, unknown> = {
  mounted: (el) => settle(el),
  updated: (el, b) => { if (b.value !== b.oldValue) settle(el); },
};

export function directives(app: App) {
  app.directive("focus", focus).directive("appear", appear);
}

declare module "vue" {
  interface ComponentCustomProperties {
    vFocus: typeof focus;
    vAppear: typeof appear;
  }
}
