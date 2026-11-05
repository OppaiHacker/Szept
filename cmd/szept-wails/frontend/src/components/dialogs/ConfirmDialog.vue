<script setup lang="ts">
import { closeModal } from "@/ui/modal";
import ModalFrame from "@/components/overlay/ModalFrame.vue";

defineProps<{ title: string; text: string; danger?: boolean; ok?: string }>();
</script>

<template>
  <ModalFrame :title="title">
    <p>{{ text }}</p>
    <template #actions>
      <button class="btn ghost" @click="closeModal(false)">Anuluj</button>
      <button class="btn" :class="danger ? 'danger' : 'primary'" @click="closeModal(true)">{{ ok || "Tak" }}</button>
    </template>
  </ModalFrame>
</template>
