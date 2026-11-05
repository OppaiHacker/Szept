<script setup lang="ts">
import { S } from "@/store/state";
import GateLink from "./GateLink.vue";
import GateSetup from "./GateSetup.vue";
</script>

<template>
  <GateLink v-if="S.boot?.stage === 'link'" wait />
  <GateLink v-else-if="S.boot?.stage === 'setup' && S.gateLink" />
  <GateSetup v-else :key="S.boot?.stage + (S.boot?.profile || '')" />
</template>
