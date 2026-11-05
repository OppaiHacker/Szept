<script setup lang="ts">
import { ref } from "vue";
import { saveSettings } from "@/store/settings";
import { S } from "@/store/state";

const lang = ref(S.settings!.lang);
</script>

<template>
  <h2>Język</h2>
  <p class="lead">Dotyczy komunikatów wysyłanych do innych (np. „dodał X do czatu”).</p>
  <div class="block">
    <div class="field"><label>Język</label>
      <div class="select-wrap"><select v-model="lang" class="input">
        <option value="pl">polski</option>
        <option value="en">english</option>
      </select></div></div>
    <div class="btn-row"><button class="btn primary" @click="saveSettings({ lang })">Zapisz</button></div>
  </div>
</template>
