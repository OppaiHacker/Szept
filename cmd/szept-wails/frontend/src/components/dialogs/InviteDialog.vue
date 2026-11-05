<script setup lang="ts">
import type { InviteCode } from "@/api/types";
import { copy } from "@/store/messages";
import { closeModal } from "@/ui/modal";
import Icon from "@/components/base/Icon.vue";
import ModalFrame from "@/components/overlay/ModalFrame.vue";

defineProps<{ inv: InviteCode }>();
</script>

<template>
  <ModalFrame title="Zaproszenie do czatu">
    <img class="qr" :src="inv.qr" alt="">
    <div class="code-box">{{ inv.code }}</div>
    <div class="field"><label>PIN jednorazowy</label>
      <div class="code-box pin">{{ inv.pin }}</div>
      <p class="hint-text">Przekaż PIN innym kanałem niż kod. Dopiero razem wpuszczają do grupy.</p></div>
    <div class="btn-row"><button class="btn" @click="copy(inv.code)"><Icon name="copy" />Kopiuj kod</button></div>
    <template #actions><button class="btn primary" @click="closeModal(null)">Gotowe</button></template>
  </ModalFrame>
</template>
