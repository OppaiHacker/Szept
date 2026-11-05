<script setup lang="ts">
import { ref } from "vue";
import { api } from "@/api/wails";
import { saveSettings } from "@/store/settings";
import { S } from "@/store/state";
import Toggle from "./Toggle.vue";

// A local HTTP webhook: a script on this computer can post into chats.
const on = ref(S.settings!.webhookOn), port = ref(S.settings!.webhookPort || 8787), token = ref(S.settings!.webhookToken);
const gen = async () => { token.value = await api<string>("NewWebhookToken"); };
const save = () => saveSettings({ webhookOn: on.value, webhookPort: +port.value, webhookToken: token.value.trim() });
</script>

<template>
  <h2>Boty</h2>
  <p class="lead">Lokalny webhook HTTP: skrypt na tym komputerze może wrzucać wiadomości do czatów.</p>
  <div class="block">
    <Toggle title="Webhook" :on="on" @flip="on = !on">nasłuch na localhost</Toggle>
    <div class="field"><label>Port</label><input v-model.number="port" class="input" type="number"></div>
    <div class="field"><label>Token</label><input v-model="token" class="input" placeholder="wygeneruj"></div>
    <div class="btn-row">
      <button class="btn ghost" @click="gen">Nowy token</button>
      <button class="btn primary" @click="save">Zapisz</button>
    </div>
  </div>
</template>
