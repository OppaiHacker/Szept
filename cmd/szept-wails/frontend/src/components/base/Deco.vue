<script setup lang="ts">
import { computed } from "vue";
import { DECO, decoSrc } from "@/lib/profileStyle";

// The sprites of avatar decoration d, laid out in the avatar's own box, with
// the optional ring under them. The ring colors come from --da/--db.
const props = defineProps<{ d: number }>();
const deco = computed(() => DECO[props.d] ?? DECO[0]);
const ring = computed(() => deco.value[2]);
const arcTurn = computed(() => `rotate(${Number(ring.value) + 39} 50 50)`);

const spriteStyle = ([, x, y, w, r, , dl]: (typeof DECO)[number][1][number]) => ({
  left: `calc(${x}% + ${w / 2}% * (1 - var(--ds, 1)))`,
  top: `calc(${y}% + ${w / 2}% * (1 - var(--ds, 1)))`,
  width: `calc(${w}% * var(--ds, 1))`,
  rotate: `${r}deg`,
  animationDelay: `${dl}s`,
});
</script>

<template>
  <svg v-if="ring !== undefined" class="ring" viewBox="-12 -12 124 124">
    <template v-if="ring === true">
      <circle cx="50" cy="50" r="53.5" fill="none" stroke="var(--da)" stroke-width="4" stroke-linecap="round" />
      <circle cx="50" cy="50" r="58.5" fill="none" stroke="var(--db)" stroke-width="1.5" stroke-linecap="round" />
    </template>
    <template v-else>
      <circle cx="50" cy="50" r="53.5" fill="none" stroke="var(--da)" stroke-width="3.5" stroke-linecap="round" stroke-dasharray="265 75" :transform="arcTurn" />
      <circle cx="50" cy="50" r="58.5" fill="none" stroke="var(--db)" stroke-width="1.2" stroke-linecap="round" stroke-dasharray="120 248" :transform="arcTurn" />
    </template>
  </svg>
  <img v-for="(s, i) in deco[1]" :key="i" :src="decoSrc(s[0])" alt="" draggable="false"
    :class="s[5] ? 'm-' + s[5] : ''" :style="spriteStyle(s)">
</template>
