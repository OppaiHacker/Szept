<script setup lang="ts">
import { computed } from "vue";
import { CARDFX, PAL, parseStyle } from "@/lib/profileStyle";
import ParticleLayer from "./ParticleLayer.vue";

// A profile card's frame and animated effect. tag "i" is the tiny preview
// tile in the effects editor.
const props = withDefaults(defineProps<{ styleStr?: string; tag?: string }>(), { tag: "div", styleStr: "" });
const st = computed(() => parseStyle(props.styleStr));
const fx = computed(() => (CARDFX[st.value.x]?.[1] ? st.value.x : 0));
</script>

<template>
  <component :is="tag" class="pcard" :data-fr="st.r" :data-fx="st.x" :style="{ '--fa': PAL[st.a], '--fb': PAL[st.b] }">
    <ParticleLayer v-if="fx" :x="fx" />
    <slot />
  </component>
</template>
