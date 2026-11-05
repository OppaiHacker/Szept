<script setup lang="ts">
import { onBeforeUnmount, ref } from "vue";
import { api } from "@/api/wails";
import type { BootInfo } from "@/api/types";
import { errText } from "@/lib/format";
import { canScan, scanQR } from "@/lib/host";
import { boot } from "@/store/actions";
import { S } from "@/store/state";
import { confirmBox } from "@/ui/dialogs";
import { toast } from "@/ui/toast";
import Icon from "@/components/base/Icon.vue";
import GateCard from "./GateCard.vue";

// Linked devices (docs/multi-device.md): a new device joins the account of
// another one by scanning (or pasting) the code that device shows. wait: the
// request went out, the other device has to answer.
const props = defineProps<{ wait?: boolean }>();
const code = ref(""), pass = ref(""), err = ref("");
const passEl = ref<HTMLInputElement>();

async function scan() {
  const t = await scanQR();
  if (t) code.value = t;
}

async function link() {
  err.value = "";
  if (!code.value.trim()) return void (err.value = "Wklej albo zeskanuj kod z drugiego urządzenia.");
  if (!pass.value && !(await confirmBox("Bez hasła?",
    "Hasło szyfruje klucz tożsamości i historię rozmów zapisane na tym urządzeniu. Bez niego są przechowywane bez szyfrowania. Kontynuować bez hasła?"))) {
    return passEl.value?.focus();
  }
  try { await api("LinkSetup", code.value.trim(), pass.value); await boot(); } catch (e) { err.value = errText(e); }
}

async function retry() {
  err.value = "";
  if (!code.value.trim()) return void (err.value = "Wklej nowy kod z drugiego urządzenia.");
  try { await api("RetryLink", code.value.trim()); toast("Wysłano. Czekam na odpowiedź.", "ok"); } catch (e) { err.value = errText(e); }
}

// while waiting, poll until the other device has let this one in
const poll = props.wait ? setInterval(async () => {
  const b = await api<BootInfo>("Boot").catch(() => null);
  if (b && b.stage !== "link") { clearInterval(poll); await boot(); }
}, 2000) : 0;
onBeforeUnmount(() => clearInterval(poll));
</script>

<template>
  <GateCard>
    <template v-if="wait">
      <h3>Czekam na drugie urządzenie</h3>
      <p class="hint-text pad">Szept musi być włączony na urządzeniu, które pokazało kod. Połączenie skończy się samo, zwykle w kilka sekund.</p>
      <div class="field"><label>Nowy kod, jeśli tamten wygasł</label>
        <input v-model="code" class="input mono" placeholder="SZL-…" autocomplete="off" spellcheck="false" @keydown.enter="retry"></div>
      <p v-if="err" class="hint-text err">{{ err }}</p>
      <div class="btn-row">
        <button v-if="canScan()" class="btn" @click="scan"><Icon name="qr" />Skanuj</button>
        <button class="btn primary" @click="retry">Wyślij ponownie</button>
      </div>
    </template>
    <template v-else>
      <h3>Połącz z kontem</h3>
      <p class="hint-text pad">Na urządzeniu, na którym już masz szepta, otwórz Ustawienia, potem Urządzenia, i wybierz „Pokaż kod”.</p>
      <div v-if="canScan()" class="btn-row"><button class="btn wide" @click="scan"><Icon name="qr" />Skanuj kod QR</button></div>
      <div class="field"><label>Kod łączenia</label>
        <input v-model="code" v-focus class="input mono" placeholder="SZL-…" autocomplete="off" spellcheck="false" @keydown.enter="link"></div>
      <div class="field"><label>Hasło klucza <span class="muted-note">(zalecane)</span></label>
        <input ref="passEl" v-model="pass" class="input" type="password" placeholder="•••••" @keydown.enter="link"></div>
      <p class="hint-text pad">To urządzenie dostanie własny klucz. Nazwa, znajomi i rozmowy przyjdą z konta. Tamto urządzenie musi być w tym czasie włączone.</p>
      <p v-if="err" class="hint-text err">{{ err }}</p>
      <div class="btn-row"><button class="btn primary wide" @click="link">Połącz</button></div>
      <div class="btn-row"><button class="btn link wide" @click="S.gateLink = false">Wróć do zakładania nowego konta</button></div>
    </template>
  </GateCard>
</template>
