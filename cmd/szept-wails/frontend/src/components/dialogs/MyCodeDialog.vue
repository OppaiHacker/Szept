<script setup lang="ts">
import { ref } from "vue";
import { copy } from "@/store/messages";
import { S } from "@/store/state";
import { closeModal } from "@/ui/modal";
import Icon from "@/components/base/Icon.vue";
import KeyBlock from "@/components/base/KeyBlock.vue";
import ModalFrame from "@/components/overlay/ModalFrame.vue";

// Your code as QR and text, with a field for someone else's.
defineProps<{ qr: string }>();
const me = S.snap!.me;
const theirs = ref("");
</script>

<template>
  <ModalFrame title="Twój kod">
    <img class="qr" :src="qr" alt="">
    <div class="code-box">{{ me.code }}</div>
    <KeyBlock :pub="me.pub" />
    <div class="btn-row"><button class="btn" @click="copy(me.code)"><Icon name="copy" />Kopiuj</button></div>
    <div class="field"><label>Kod znajomego</label><input v-model="theirs" class="input" placeholder="SZF-…"></div>
    <template #actions>
      <button class="btn ghost" @click="closeModal(null)">Zamknij</button>
      <button class="btn primary" @click="closeModal(theirs)">Dodaj znajomego</button>
    </template>
  </ModalFrame>
</template>
