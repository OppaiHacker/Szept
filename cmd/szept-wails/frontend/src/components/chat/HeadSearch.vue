<script setup lang="ts">
import { ref } from "vue";
import { searchChat } from "@/store/actions";
import { S } from "@/store/state";
import Icon from "@/components/base/Icon.vue";

// The search field in the chat header. Enter searches, Esc clears.
const q = ref(S.search?.q || "");

function clear(e: KeyboardEvent) {
  e.stopPropagation();
  q.value = "";
  (e.target as HTMLInputElement).blur();
  searchChat("");
}
const blur = () => { if (!q.value) S.hsOpen = false; };
</script>

<template>
  <div class="search head-search">
    <input id="hs" v-model="q" placeholder="Szukaj" @keydown.enter.prevent="searchChat(q)" @keydown.esc="clear" @blur="blur">
    <Icon name="search" />
  </div>
</template>
