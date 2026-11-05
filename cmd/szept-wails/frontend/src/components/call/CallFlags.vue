<script setup lang="ts">
import Icon from "@/components/base/Icon.vue";

// What a call member says about themselves (letters from core's Call.State:
// m mic off, d deafened, c camera, s screen) as small icons.
defineProps<{ flags?: string }>();
</script>

<template>
  <span v-if="flags && /[mdcs]/.test(flags)" class="vflags">
    <i v-if="flags.includes('m')" class="off" title="Wyciszony mikrofon"><Icon name="micOff" /></i>
    <i v-if="flags.includes('d')" class="off" title="Wyciszone dźwięki"><Icon name="deaf" /></i>
    <i v-if="flags.includes('c')" title="Kamera włączona"><Icon name="cam" /></i>
    <b v-if="flags.includes('s')" class="live" title="Udostępnia ekran">Na żywo</b>
  </span>
</template>
