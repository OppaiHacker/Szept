<script setup lang="ts">
import { computed } from "vue";
import type { Conversation } from "@/api/types";
import MemberRow from "./MemberRow.vue";

const props = defineProps<{ conv: Conversation }>();
const online = computed(() => props.conv.members.filter((m) => m.online));
const offline = computed(() => props.conv.members.filter((m) => !m.online));
</script>

<template>
  <div v-appear="conv.id" class="scroll">
    <div class="sec">Dostępni · {{ online.length }}</div>
    <div class="rows"><MemberRow v-for="m in online" :key="m.pub" :m="m" fx /></div>
    <template v-if="offline.length">
      <div class="sec">Niedostępni · {{ offline.length }}</div>
      <div class="rows"><MemberRow v-for="m in offline" :key="m.pub" :m="m" fx /></div>
    </template>
  </div>
</template>
