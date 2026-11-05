<script setup lang="ts">
import { onBeforeUnmount, ref, watch, type VNode } from "vue";
import { closePopover, currentPop } from "@/ui/popover";

// Places the open popover next to its anchor, kept inside the window, with
// the transform origin on the anchor so it scales out of it.
const el = ref<HTMLElement>();

// runs as the popover is inserted, before the first paint
function place(v: VNode) {
  const p = currentPop.value, box = v.el as HTMLElement | null;
  if (!p || !box) return;
  const a = p.anchor.getBoundingClientRect(), r = box.getBoundingClientRect();
  let x = a.left, y = a.bottom + 6;
  if (p.side === "right") { x = a.right + 8; y = a.top; }
  if (p.side === "left") { x = a.left - r.width - 8; y = a.top; }
  if (p.side === "top") { x = a.right - r.width; y = a.top - r.height - 8; }
  x = Math.max(8, Math.min(innerWidth - r.width - 8, x));
  y = Math.max(8, Math.min(innerHeight - r.height - 8, y));
  Object.assign(box.style, {
    left: x + "px", top: y + "px",
    transformOrigin: `${a.left + a.width / 2 - x}px ${a.top + a.height / 2 - y}px`,
  });
}

// a press outside both the popover and its anchor closes it
function off(e: PointerEvent) {
  const p = currentPop.value, t = e.target as Node;
  if (p && !el.value?.contains(t) && !p.anchor.contains(t)) closePopover();
}

watch(() => currentPop.value?.id, (id) => {
  document.removeEventListener("pointerdown", off, true);
  if (!id) return;
  document.addEventListener("pointerdown", off, true);
});
onBeforeUnmount(() => document.removeEventListener("pointerdown", off, true));
</script>

<template>
  <Transition leave-active-class="out" type="transition">
    <div v-if="currentPop" ref="el" :key="currentPop.id" class="pop" :class="currentPop.cls" @vue:mounted="place">
      <component :is="currentPop.comp" v-bind="currentPop.props" />
    </div>
  </Transition>
</template>
