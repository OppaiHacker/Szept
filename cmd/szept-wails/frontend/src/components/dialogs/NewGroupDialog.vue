<script setup lang="ts">
import { ref } from "vue";
import { closeModal } from "@/ui/modal";
import ModalFrame from "@/components/overlay/ModalFrame.vue";

const props = defineProps<{ name?: string }>();
const name = ref(props.name || ""), history = ref(false);
const ok = () => closeModal({ name: name.value.trim(), history: history.value });
</script>

<template>
  <ModalFrame title="Nowa grupa">
    <div class="field"><label>Nazwa</label><input v-model="name" class="input" placeholder="np. ekipa" @keydown.enter="ok"></div>
    <label class="check-line"><input v-model="history" type="checkbox"> Nowi członkowie widzą wcześniejszą historię</label>
    <p class="hint-text">Bez tego nowa osoba widzi tylko to, co napisano po jej dołączeniu: dostaje klucze od momentu wejścia, a Ty po każdym dodaniu kogoś zmieniasz klucz automatycznie.</p>
    <template #actions>
      <button class="btn ghost" @click="closeModal(null)">Anuluj</button>
      <button class="btn primary" @click="ok">Załóż</button>
    </template>
  </ModalFrame>
</template>
