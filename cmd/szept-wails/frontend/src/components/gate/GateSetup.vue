<script setup lang="ts">
import { ref } from "vue";
import { api } from "@/api/wails";
import { errText } from "@/lib/format";
import { boot } from "@/store/actions";
import { S } from "@/store/state";
import { confirmBox } from "@/ui/dialogs";
import GateCard from "./GateCard.vue";
import ProfileSwitch from "./ProfileSwitch.vue";

// First run (a new identity) or unlocking an existing, password-protected one.
const setup = S.boot!.stage === "setup";
const name = ref(S.boot!.profile), conn = ref(S.boot!.connector), pass = ref("");
const err = ref("");
const passEl = ref<HTMLInputElement>();

async function go() {
  err.value = "";
  try {
    if (setup) {
      // seizure of the device is in the threat model: no password has to be a
      // deliberate choice, never a skipped field
      if (!pass.value && !(await confirmBox("Bez hasła?",
        "Hasło szyfruje klucz tożsamości i historię rozmów zapisane na tym urządzeniu. Bez niego są przechowywane bez szyfrowania. Hasło możesz ustawić także później w ustawieniach. Kontynuować bez hasła?"))) {
        return passEl.value?.focus();
      }
      await api("Setup", name.value, conn.value, pass.value);
    } else {
      await api("Unlock", pass.value);
    }
    await boot();
  } catch (e) { err.value = errText(e); }
}
</script>

<template>
  <GateCard>
    <h3>{{ setup ? (S.boot!.profile ? `Nowe konto „${S.boot!.profile}”` : "Pierwsze uruchomienie") : "Klucz tożsamości jest zaszyfrowany" }}</h3>
    <template v-if="setup">
      <div class="field"><label>Nazwa</label>
        <input v-model="name" v-focus class="input" placeholder="np. ala" @keydown.enter="go"></div>
      <div class="field"><label>Connector</label>
        <input v-model="conn" class="input" placeholder="ws://twoj-vps:7777" @keydown.enter="go"></div>
      <div class="field"><label>Hasło klucza <span class="muted-note">(zalecane)</span></label>
        <input ref="passEl" v-model="pass" class="input" type="password" placeholder="•••••" @keydown.enter="go"></div>
      <p class="hint-text pad">Connector tylko kojarzy urządzenia. Treści rozmów nie widzi.</p>
      <p class="hint-text pad">Hasło szyfruje klucz i historię rozmów na tym urządzeniu. Możesz je ustawić także później w ustawieniach.</p>
    </template>
    <div v-else class="field"><label>Hasło</label>
      <input ref="passEl" v-model="pass" v-focus class="input" type="password" placeholder="hasło klucza" @keydown.enter="go"></div>
    <p v-if="err" class="hint-text err">{{ err }}</p>
    <div class="btn-row"><button class="btn primary wide" @click="go">{{ setup ? "Zaczynamy" : "Odblokuj" }}</button></div>
    <div v-if="setup" class="btn-row"><button class="btn link wide" @click="S.gateLink = true">Mam już konto na innym urządzeniu</button></div>
    <template #after><ProfileSwitch /></template>
  </GateCard>
</template>
