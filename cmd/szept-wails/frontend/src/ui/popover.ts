import { markRaw, shallowRef, type Component } from "vue";

// A small floating layer next to the control that opened it: menus, pickers,
// profile cards. It grows out of that control and closes on a click outside,
// Esc or a picked item. Pressing the same control again closes it.
export type Side = "bottom" | "top" | "left" | "right";
// an element, or a point (a right click) dressed up as one
export type Anchor = Element | { getBoundingClientRect(): DOMRect; contains(n: Node | null): boolean };

interface Open { id: number; comp: Component; props: Record<string, unknown>; anchor: Anchor; side: Side; cls: string }

export const currentPop = shallowRef<Open | null>(null);
let seq = 0;

export function openPopover(anchor: Anchor, comp: Component, props: Record<string, unknown> = {}, side: Side = "bottom", cls = "") {
  if (currentPop.value?.anchor === anchor) return closePopover();
  currentPop.value = { id: ++seq, comp: markRaw(comp), props, anchor, side, cls };
}

export function closePopover() {
  currentPop.value = null;
}

export const popOpen = () => !!currentPop.value;

// at: a right click as an anchor
export const pointAnchor = (e: MouseEvent): Anchor => ({
  getBoundingClientRect: () => new DOMRect(e.clientX, e.clientY, 0, 0),
  contains: () => false,
});
