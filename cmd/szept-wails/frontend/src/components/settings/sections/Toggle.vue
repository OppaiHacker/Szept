<script setup lang="ts">
// A settings row with a title, an explanation and a switch.
defineProps<{ title: string; on: boolean }>();
defineEmits<{ flip: [] }>();
</script>

<template>
  <div class="toggle">
    <div class="tx"><b>{{ title }}</b><span><slot /></span></div>
    <button class="switch" :class="{ on }" :aria-pressed="on" :aria-label="title" @click="$emit('flip')" />
  </div>
</template>
