<script setup lang="ts">
import { S } from "@/store/state";
import { closeModal } from "@/ui/modal";
import ModalFrame from "@/components/overlay/ModalFrame.vue";
import MemberRow from "@/components/chat/MemberRow.vue";
</script>

<template>
  <ModalFrame :title="`Członkowie · ${S.conv?.name || ''}`">
    <div class="rows in-modal">
      <MemberRow v-for="m in S.conv?.members || []" :key="m.pub" :m="m" :sub="m.status || (m.online ? 'online' : 'offline')" />
    </div>
    <template #actions><button class="btn ghost" @click="closeModal(null)">Zamknij</button></template>
  </ModalFrame>
</template>
