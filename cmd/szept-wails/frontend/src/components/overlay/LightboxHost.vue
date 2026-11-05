<script setup lang="ts">
import { closeLightbox, lightboxSrc } from "@/ui/lightbox";
</script>

<template>
  <Transition leave-active-class="out" type="animation">
    <div v-if="lightboxSrc" class="lightbox" @click="closeLightbox"><img :src="lightboxSrc" alt=""></div>
  </Transition>
</template>
