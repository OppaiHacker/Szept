<script setup lang="ts">
import { computed, ref } from "vue";
import { api } from "@/api/wails";
import type { Settings } from "@/api/types";
import { relayDialog } from "@/store/dialogs";
import { saveSettings } from "@/store/settings";
import { S } from "@/store/state";
import { fail, toast } from "@/ui/toast";
import Icon from "@/components/base/Icon.vue";
import Toggle from "./Toggle.vue";

const s = computed(() => S.settings!);
const me = computed(() => S.snap!.me);
const conn = ref(s.value.connector), newConn = ref(""), newList = ref("");

const addConn = () => { const v = newConn.value.trim(); if (v) saveSettings({ connectors: [...new Set([...s.value.connectors, v])] }); };
function addList() {
  const v = newList.value.trim();
  if (!/^https:\/\//.test(v)) return toast("Adres listy musi zaczynać się od https://", "err");
  saveSettings({ connectorLists: [...new Set([...s.value.connectorLists, v])] });
}
async function refreshLists() {
  try { S.settings = await api<Settings>("RefreshConnectors"); toast("Listy odświeżone", "ok"); } catch (e) { fail(e); }
}
async function relay() {
  if (s.value.relayOnly) return saveSettings({ relayOnly: false });
  if (await relayDialog(true)) saveSettings({ relayOnly: true });
}
</script>

<template>
  <h2>Connector</h2>
  <p class="lead">Lekki serwer spotkań: kojarzy peerów i przekazuje sygnalizację. Treści nie widzi: ruch idzie P2P i jest szyfrowany.</p>
  <div class="block">
    <h4>Aktywny</h4>
    <div class="field"><input v-model="conn" class="input" placeholder="ws://twoj-vps:7777"></div>
    <p class="hint-text pad">Stan: {{ me.connUp ? "połączony" + (me.connVer ? `, connector w wersji ${me.connVer}` : "") : me.connErr || "łączenie…" }}</p>
    <div class="btn-row"><button class="btn primary" @click="saveSettings({ connector: conn })">Zapisz i połącz</button></div>
  </div>
  <div class="block">
    <Toggle title="Ukryj mój adres IP" :on="s.relayOnly" @flip="relay">połączenia idą tylko przez przekaźnik connectora, rozmówcy widzą jego adres zamiast twojego. Wolniej. {{ me.connRelay ? "Ten connector ma przekaźnik." : "Ten connector nie ma przekaźnika. Z włączoną opcją nie połączysz się z nikim." }}</Toggle>
    <Toggle title="Ukryj listę kontaktów przed connectorem" :on="s.quietLookup" @flip="saveSettings({ quietLookup: !s.quietLookup })">Normalnie aplikacja co 30 s pyta connector o każdy kontakt, więc connector zna całą Twoją listę kontaktów. Z tą opcją pyta tylko o osoby z czatów otwartych albo używanych w ostatnich 15 minutach. Kropki obecności pozostałych kontaktów nie będą aktualne.</Toggle>
  </div>
  <div class="block">
    <h4>Zapisane adresy</h4>
    <div v-for="c in s.connectors" :key="c" class="list-line"><span class="mono">{{ c }}</span>
      <span v-if="c === s.connector" class="tag">aktywny</span>
      <button v-else class="btn ghost" @click="saveSettings({ connector: c })">Użyj</button>
      <button class="icon-btn" @click="saveSettings({ connectors: s.connectors.filter((x) => x !== c) })"><Icon name="trash" /></button>
    </div>
    <p v-if="!s.connectors.length" class="hint-text pad">Lista pusta.</p>
    <div class="field"><label>Dodaj</label><input v-model="newConn" class="input" placeholder="ws://inny-vps:7777"></div>
    <div class="btn-row"><button class="btn" @click="addConn"><Icon name="plus" />Dodaj</button></div>
  </div>
  <div class="block">
    <h4>Dynamiczne listy</h4>
    <p class="hint-text">Adres HTTP zwracający tekst, jeden connector (ws://…) na linię.</p>
    <div v-for="l in s.connectorLists" :key="l" class="list-line"><span class="mono">{{ l }}</span>
      <button class="icon-btn" @click="saveSettings({ connectorLists: s.connectorLists.filter((x) => x !== l) })"><Icon name="trash" /></button>
    </div>
    <div class="field"><label>Dodaj listę</label><input v-model="newList" class="input" placeholder="https://example.com/connectors.txt"></div>
    <div class="btn-row">
      <button class="btn" @click="addList"><Icon name="plus" />Dodaj</button>
      <button v-if="s.connectorLists.length" class="btn ghost" @click="refreshLists">Odśwież teraz</button>
    </div>
  </div>
</template>
