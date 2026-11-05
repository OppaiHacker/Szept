<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive } from "vue";
import { lastPress } from "@/lib/env";

// Tooltips for [data-tip] (the rail). The rail scrolls, so a CSS ::after
// would be clipped by it; this one floats over everything.
const tip = reactive({ on: false, text: "", x: 0, y: 0 });

function over(e: PointerEvent) {
  const el = (e.target as Element).closest?.<HTMLElement>("[data-tip]");
  // a click repaints the rail under a still pointer: that is not a new hover
  const still = lastPress && e.clientX === lastPress[0] && e.clientY === lastPress[1];
  if (!el || still) return void (tip.on = false);
  const r = el.getBoundingClientRect();
  Object.assign(tip, { on: true, text: el.dataset.tip, x: r.right + 14, y: r.top + r.height / 2 });
}
const hide = () => (tip.on = false);

onMounted(() => {
  document.addEventListener("pointerover", over);
  document.addEventListener("pointerdown", hide);
});
onBeforeUnmount(() => {
  document.removeEventListener("pointerover", over);
  document.removeEventListener("pointerdown", hide);
});
</script>

<template>
  <div class="tip" :class="{ on: tip.on }" :style="{ left: tip.x + 'px', top: tip.y + 'px' }">{{ tip.text }}</div>
</template>
