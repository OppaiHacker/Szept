<script setup lang="ts">
import { ICONS, type IconName } from "@/lib/icons";

defineProps<{ name: IconName }>();
</script>

<template>
  <!-- the paths are our own constants, never peer data -->
  <svg viewBox="0 0 24 24" v-html="ICONS[name]" />
</template>
