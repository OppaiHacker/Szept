<script setup lang="ts">
import { toasts } from "@/ui/toast";
</script>

<template>
  <TransitionGroup tag="div" class="toasts" role="status" aria-live="polite" leave-active-class="out" type="transition">
    <div v-for="t in toasts" :key="t.id" class="toast" :class="t.kind">{{ t.text }}</div>
  </TransitionGroup>
</template>
