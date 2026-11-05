<script setup lang="ts">
import { onMounted, ref, watch } from "vue";
import { setFrame } from "@/lib/frames";

// A live call picture: each new src is the next frame.
const props = defineProps<{ src: string }>();
const cv = ref<HTMLCanvasElement>();
onMounted(() => setFrame(cv.value!, props.src));
watch(() => props.src, (s) => cv.value && setFrame(cv.value, s));
</script>

<template>
  <canvas ref="cv" class="v" />
</template>
