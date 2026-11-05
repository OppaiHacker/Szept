<script setup lang="ts">
import { COMMANDS } from "@/store/commands";
import ModalFrame from "@/components/overlay/ModalFrame.vue";
</script>

<template>
  <ModalFrame title="Komendy" wide>
    <div class="block help">
      <div v-for="c in COMMANDS" :key="c.n" class="list-line"><span class="mono">{{ c.n }} {{ c.a || "" }}</span><span class="muted">{{ c.d }}</span></div>
    </div>
  </ModalFrame>
</template>
