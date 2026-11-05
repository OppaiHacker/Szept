<script setup lang="ts">
import { computed } from "vue";
import { newProfile, switchProfile } from "@/store/profiles";
import { S } from "@/store/state";
import Icon from "@/components/base/Icon.vue";

const s = computed(() => S.settings!);
</script>

<template>
  <h2>Konta</h2>
  <p class="lead">Każde konto to osobny katalog danych i osobna tożsamość. Przełączenie restartuje aplikację.</p>
  <div class="block">
    <p v-if="s.pinned" class="hint-text pad">Aplikacja działa z <span class="mono">-data</span>, konta są wyłączone.</p>
    <template v-else>
      <div v-for="p in s.profiles" :key="p" class="list-line"><span class="mono">{{ p || "domyślne" }}</span>
        <span v-if="p === s.profile" class="tag">aktywne</span>
        <button v-else class="btn ghost" @click="switchProfile(p)">Przełącz</button>
      </div>
      <div class="btn-row"><button class="btn" @click="newProfile"><Icon name="plus" />Nowe konto</button></div>
    </template>
  </div>
  <div class="block">
    <h4>Katalog danych</h4>
    <div class="code-box dir">{{ s.dataDir }}</div>
  </div>
</template>
