<script setup lang="ts">
import { onMounted, ref } from "vue";
import { autoFocus, lastPress } from "@/lib/env";
import { closeModal } from "@/ui/modal";
import Icon from "@/components/base/Icon.vue";

// The sheet every dialog is drawn in: title bar, body, a footer of buttons.
// bare: no title bar and no buttons, the body is the whole sheet (a profile
// card whose frame has to go around all of it).
const props = defineProps<{ title: string; wide?: boolean; bare?: boolean }>();
const box = ref<HTMLElement>();

onMounted(() => {
  const el = box.value!;
  // grow out of the control that was pressed
  if (lastPress) {
    const r = el.getBoundingClientRect();
    const pct = (v: number, size: number) => Math.max(-15, Math.min(115, (v / size) * 100)).toFixed(1) + "%";
    el.style.setProperty("--ox", pct(lastPress[0] - r.left, r.width));
    el.style.setProperty("--oy", pct(lastPress[1] - r.top, r.height));
  }
  const first = el.querySelector<HTMLInputElement>("input:not([type=radio]):not([type=checkbox]), textarea, select");
  autoFocus(first);
  first?.select?.();
});
</script>

<template>
  <div ref="box" class="modal" :class="{ wide, bare }" role="dialog" aria-modal="true" :aria-label="bare ? props.title : undefined">
    <header v-if="!bare">
      <h3>{{ title }}</h3>
      <button class="icon-btn" @click="closeModal(null)"><Icon name="x" /></button>
    </header>
    <div class="body"><slot /></div>
    <footer v-if="$slots.actions"><slot name="actions" /></footer>
  </div>
</template>
