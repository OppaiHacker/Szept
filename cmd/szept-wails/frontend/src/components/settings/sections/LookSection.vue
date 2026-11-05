<script setup lang="ts">
import { computed, ref } from "vue";
import { ACCENTS, DEFAULT_ACCENT, applyAccent, setTheme, themePref, type ThemePref } from "@/lib/theme";
import { saveSettings } from "@/store/settings";
import { S } from "@/store/state";
import { toast } from "@/ui/toast";
import Toggle from "./Toggle.vue";

const s = computed(() => S.settings!);
const custom = ref(s.value.accent || DEFAULT_ACCENT);
const THEMES: [ThemePref, string][] = [["auto", "Automatyczny"], ["light", "Jasny"], ["dark", "Ciemny"]];

const accent = (v: string) => { applyAccent(v); saveSettings({ accent: v }); };
function saveCustom() {
  const v = custom.value.replace("#", "").trim();
  if (!/^[0-9a-fA-F]{6}$/.test(v)) return toast("Podaj kolor jako 6 znaków hex, np. 8b7cf6", "err");
  accent(v);
}
</script>

<template>
  <h2>Wygląd</h2>
  <p class="lead">Jeden kolor akcentu przechodzi przez cały interfejs: przyciski, dymki, wskaźniki.</p>
  <div class="block">
    <h4>Motyw</h4>
    <p class="hint-text">Automatyczny idzie za ustawieniem pulpitu, więc wieczorem sam ściemnieje.</p>
    <div class="segmented">
      <button v-for="[v, t] in THEMES" :key="v" :class="{ sel: themePref === v }" @click="setTheme(v)">{{ t }}</button>
    </div>
  </div>
  <div class="block">
    <h4>Kolor akcentu</h4>
    <div class="swatches">
      <button v-for="c in ACCENTS" :key="c" class="swatch" :class="{ sel: (s.accent || DEFAULT_ACCENT).toLowerCase() === c }"
        :style="{ background: '#' + c }" @click="accent(c)" />
    </div>
    <div class="field"><label>Własny (hex)</label><input v-model="custom" class="input" placeholder="2f8a8a"></div>
    <div class="btn-row"><button class="btn primary" @click="saveCustom">Zastosuj</button></div>
  </div>
  <div class="block">
    <Toggle title="Tryb dewelopera" :on="s.devMode" @flip="saveSettings({ devMode: !s.devMode })">menu kontekstowe przeglądarki i inspektor. Inspektor działa po restarcie aplikacji</Toggle>
  </div>
</template>
