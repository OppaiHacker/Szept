<script setup lang="ts">
import { ref } from "vue";
import { api } from "@/api/wails";
import { exported } from "@/lib/host";
import { useProfile } from "@/store/profiles";
import { confirmBox } from "@/ui/dialogs";
import { fail, toast } from "@/ui/toast";
import Icon from "@/components/base/Icon.vue";

const oldPass = ref(""), newPass = ref("");

async function change() {
  try {
    await api("ChangePass", oldPass.value, newPass.value);
    toast("Hasło zmienione", "ok");
    oldPass.value = newPass.value = "";
  } catch (e) { fail(e); }
}
async function backup() {
  try { const p = await api<string>("Backup"); if (p) exported(p, "application/zip"); } catch (e) { fail(e); }
}
// restoring never overwrites: the copy lands as a new account
async function restore() {
  try {
    const name = await api<string>("Restore");
    if (name && await confirmBox("Kopia wgrana", `Kopia wgrała się jako konto „${name}”. Przełączyć się teraz? Aplikacja uruchomi się od nowa.`, false)) {
      await useProfile(name);
    }
  } catch (e) { fail(e); }
}
</script>

<template>
  <h2>Hasło i kopia</h2>
  <p class="lead">Baza jest szyfrowana losowym kluczem, który chroni hasło. Zmiana hasła losuje nowy klucz i przepisuje bazę.</p>
  <div class="block">
    <h4>Hasło klucza</h4>
    <div class="field"><label>Stare hasło</label><input v-model="oldPass" class="input" type="password" placeholder="(puste jeśli nie było)"></div>
    <div class="field"><label>Nowe hasło</label><input v-model="newPass" class="input" type="password"></div>
    <div class="btn-row"><button class="btn primary" @click="change">Zmień hasło</button></div>
  </div>
  <div class="block">
    <h4>Kopia zapasowa</h4>
    <p class="hint-text">Zip z kluczem tożsamości i spójną migawką bazy. Bez hasła kopia jest bezużyteczna dla znalazcy.</p>
    <div class="btn-row">
      <button class="btn" @click="backup"><Icon name="save" />Zapisz kopię</button>
      <button class="btn ghost" @click="restore"><Icon name="file" />Przywróć z kopii</button>
    </div>
    <p class="hint-text pad-b">Przywracanie nigdy nie nadpisuje obecnych danych. Kopia ląduje jako nowe konto.</p>
  </div>
</template>
