<script setup lang="ts">
import { ref } from "vue";
import { closeModal } from "@/ui/modal";
import ModalFrame from "@/components/overlay/ModalFrame.vue";

// An invite is two parts that travel separately: the code and a one-time PIN.
const props = defineProps<{ code?: string }>();
const code = ref(props.code || ""), pin = ref("");
const ok = () => closeModal({ code: code.value, pin: pin.value });
</script>

<template>
  <ModalFrame title="Wejdź do grupy">
    <div class="field"><label>Kod zaproszenia</label><input v-model="code" class="input" placeholder="SZC-…"></div>
    <div class="field"><label>PIN</label><input v-model="pin" class="input" placeholder="6 cyfr" inputmode="numeric" @keydown.enter="ok"></div>
    <template #actions>
      <button class="btn ghost" @click="closeModal(null)">Anuluj</button>
      <button class="btn primary" @click="ok">Wejdź</button>
    </template>
  </ModalFrame>
</template>
