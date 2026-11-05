<script setup lang="ts">
import { talking } from "@/store/state";

// Speaking ring: a solid ring around whatever is inside while pub talks.
defineProps<{ pub?: string }>();
</script>

<template>
  <span class="spk" :class="{ talking: !!pub && talking.has(pub) }" :data-speak="pub || ''"><slot /></span>
</template>
