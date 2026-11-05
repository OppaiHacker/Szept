<script setup lang="ts">
import type { Member } from "@/api/types";
import { relayDialog } from "@/store/dialogs";

// The tail of the header subtitle: a link through a relay, hidden messages.
defineProps<{ relayed: Member[]; hidden: number }>();
</script>

<template>
  <template v-if="relayed.length || hidden"> · </template>
  <button v-if="relayed.length" class="relay-tag" :title="relayed.map((m) => `${m.name}: ${m.relay}`).join(', ')"
    @click="relayDialog()">przez przekaźnik</button>
  <template v-if="relayed.length && hidden"> · </template>
  <em v-if="hidden">{{ hidden }} schowanych</em>
</template>
