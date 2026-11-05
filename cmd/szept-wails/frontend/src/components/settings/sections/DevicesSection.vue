<script setup lang="ts">
import { computed } from "vue";
import { api } from "@/api/wails";
import type { LinkCode } from "@/api/types";
import { dateOf } from "@/lib/format";
import { copy } from "@/store/messages";
import { reloadSettings } from "@/store/settings";
import { S } from "@/store/state";
import { confirmBox } from "@/ui/dialogs";
import { fail, toast } from "@/ui/toast";
import Icon from "@/components/base/Icon.vue";

// One account on several devices (docs/multi-device.md). Only the account's
// first device links and unlinks the others.
const s = computed(() => S.settings!);

async function newCode() {
  try { S.linkCode = await api<LinkCode>("NewLinkCode"); } catch (e) { fail(e); }
}
async function unlink(pub: string, name: string) {
  if (!await confirmBox(`Odłączyć „${name || "urządzenie"}”?`,
    "Urządzenie wypadnie ze wszystkich rozmów, a klucze rozmów się zmienią, więc nie przeczyta nic nowego. W grupach innych osób stanie się to, gdy ich właściciel będzie online. To, co już jest zapisane na tamtym urządzeniu, tam zostaje.")) return;
  try { await api("Unlink", pub); await reloadSettings(); toast("Odłączono.", "ok"); } catch (e) { fail(e); }
}
</script>

<template>
  <h2>Urządzenia</h2>
  <p class="lead">{{ s.linked
    ? "To urządzenie należy do konta z pierwszego urządzenia. Nowe urządzenia łączy się i odłącza tam."
    : "Jedno konto na kilku urządzeniach naraz. Każde ma własny klucz, a znajomi widzą jedną osobę." }}</p>
  <div class="block"><h4>Twoje urządzenia</h4>
    <div v-for="d in s.devices || []" :key="d.pub" class="list-line">
      <span class="dev"><b>{{ d.name || "Urządzenie" }}</b><span class="mono">{{ d.pub.slice(0, 16) }}</span><span>{{ d.ts ? "połączone " + dateOf(d.ts) : "" }}</span></span>
      <span class="tag">{{ d.me ? "to urządzenie" : d.online ? "online" : "offline" }}</span>
      <button v-if="!s.linked && !d.me" class="btn ghost" @click="unlink(d.pub, d.name)">Odłącz</button>
    </div>
  </div>
  <div v-if="!s.linked" class="block">
    <h4>Połącz nowe urządzenie</h4>
    <template v-if="S.linkCode">
      <img class="qr" :src="S.linkCode.qr" alt="Kod QR łączenia urządzenia">
      <div class="code-box">{{ S.linkCode.code }}</div>
      <p class="hint-text">Na nowym urządzeniu wybierz „Mam już konto na innym urządzeniu” i zeskanuj kod. Działa raz, przez 10 minut. Kto go zeskanuje, dostanie dostęp do twoich rozmów, więc nie pokazuj go nikomu.</p>
      <div class="btn-row">
        <button class="btn" @click="copy(S.linkCode.code)"><Icon name="copy" />Kopiuj</button>
        <button class="btn ghost" @click="newCode">Nowy kod</button>
      </div>
    </template>
    <template v-else>
      <p class="hint-text">Pokaże kod QR do zeskanowania na telefonie albo do wklejenia na drugim komputerze.</p>
      <div class="btn-row"><button class="btn primary" @click="newCode"><Icon name="qr" />Pokaż kod</button></div>
    </template>
  </div>
</template>
