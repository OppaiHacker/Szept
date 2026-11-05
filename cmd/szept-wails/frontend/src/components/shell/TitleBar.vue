<script setup lang="ts">
import { computed } from "vue";
import { act } from "@/store/actions";
import { S } from "@/store/state";
import Logo from "@/components/base/Logo.vue";

// The window is frameless: this bar is its chrome and its drag handle.
const me = computed(() => S.snap?.me);
const state = computed(() => (me.value?.connErr ? "err" : me.value?.connUp ? "up" : "wait"));
// the raw dial error is for whoever debugs it, not for the title bar
const text = computed(() => {
  const m = me.value!;
  return m.connErr ? "Brak połączenia z connectorem"
    : m.connUp ? "połączony · " + (m.connVer ? m.connVer + " · " : "") + m.connector : "łączenie z connectorem…";
});
</script>

<template>
  <header class="titlebar" style="--wails-draggable:drag">
    <div class="tb-brand">
      <Logo class="tb-mark" />
      <span class="tb-name">szept</span>
      <span class="tb-ver">{{ S.version }}</span>
      <span class="tb-sub">{{ me?.profile ? "· " + me.profile : "" }}</span>
    </div>
    <div v-if="me" class="tb-status" :data-state="state" :title="me.connErr || ''">
      <span class="dot" /><span class="txt">{{ text }}</span>
    </div>
    <div class="tb-buttons" style="--wails-draggable:no-drag">
      <button class="tb-btn" title="Minimalizuj" aria-label="Minimalizuj" @click="act('Minimise')">
        <svg viewBox="0 0 10 10"><path d="M1 5h8" /></svg>
      </button>
      <button class="tb-btn" title="Maksymalizuj" aria-label="Maksymalizuj" @click="act('ToggleMax')">
        <svg viewBox="0 0 10 10"><rect x="1.5" y="1.5" width="7" height="7" rx="1.5" /></svg>
      </button>
      <button class="tb-btn danger" title="Zamknij" aria-label="Zamknij" @click="act('CloseApp')">
        <svg viewBox="0 0 10 10"><path d="M2 2l6 6M8 2l-6 6" /></svg>
      </button>
    </div>
  </header>
</template>
