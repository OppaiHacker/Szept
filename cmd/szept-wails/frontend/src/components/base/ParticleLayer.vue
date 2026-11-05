<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import type { Container } from "@tsparticles/engine";
import { reducedMotion } from "@/lib/env";
import { startFx } from "@/lib/particles";

// The particle canvas of a profile card. It sizes itself from the card it
// sits in and pauses when the system asks for less motion.
const props = defineProps<{ x: number }>();
const el = ref<HTMLElement>();
let live: Container | undefined;
let gone = false;

async function start() {
  live?.destroy();
  const card = el.value!.parentElement!;
  const c = await startFx(el.value!, props.x, card.offsetWidth || 300, card.offsetHeight || 260);
  if (gone) return c?.destroy();
  live = c;
  if (reducedMotion.value) setTimeout(() => live?.pause(), 60);
}

onMounted(start);
watch(() => props.x, start);
onBeforeUnmount(() => { gone = true; live?.destroy(); });
</script>

<template>
  <div ref="el" class="fxl" />
</template>
