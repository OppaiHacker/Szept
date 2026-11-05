<script setup lang="ts">
import { ref } from "vue";
import { closeModal } from "@/ui/modal";
import ModalFrame from "@/components/overlay/ModalFrame.vue";

const props = defineProps<{ title: string; label: string; value?: string; placeholder?: string; hint?: string; multiline?: boolean; ok?: string }>();
const v = ref(props.value ?? "");
</script>

<template>
  <ModalFrame :title="title">
    <div class="field">
      <label>{{ label }}</label>
      <textarea v-if="multiline" v-model="v" class="input" :placeholder="placeholder" />
      <input v-else v-model="v" class="input" :placeholder="placeholder" @keydown.enter.prevent="closeModal(v)">
      <p v-if="hint" class="hint-text">{{ hint }}</p>
    </div>
    <template #actions>
      <button class="btn ghost" @click="closeModal(null)">Anuluj</button>
      <button class="btn primary" @click="closeModal(v)">{{ ok || "Zapisz" }}</button>
    </template>
  </ModalFrame>
</template>
