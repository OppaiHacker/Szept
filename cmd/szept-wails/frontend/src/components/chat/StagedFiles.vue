<script setup lang="ts">
import { computed, onBeforeUnmount } from "vue";
import { S } from "@/store/state";
import Icon from "@/components/base/Icon.vue";

// Pasted, dropped or chosen files waiting over the composer; Enter sends them.
let urls: string[] = [];
const free = () => { urls.forEach((u) => URL.revokeObjectURL(u)); urls = []; };
const items = computed(() => {
  free();
  return S.files.map((f) => {
    const thumb = f.type.startsWith("image/") ? URL.createObjectURL(f) : "";
    if (thumb) urls.push(thumb);
    return { name: f.name || "obraz.png", thumb };
  });
});
onBeforeUnmount(free);
const drop = (i: number) => (S.files = S.files.filter((_, j) => j !== i));
</script>

<template>
  <div class="staged" :hidden="!items.length">
    <div v-for="(f, i) in items" :key="i" class="pend">
      <img v-if="f.thumb" :src="f.thumb" alt=""><Icon v-else name="file" />
      <span>{{ f.name }}</span>
      <button class="icon-btn" title="Usuń" @click="drop(i)"><Icon name="close" /></button>
    </div>
  </div>
</template>
