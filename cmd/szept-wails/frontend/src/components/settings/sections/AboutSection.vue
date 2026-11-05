<script setup lang="ts">
import { api } from "@/api/wails";
import type { UpdateInfo } from "@/api/types";
import { S } from "@/store/state";
import { toast } from "@/ui/toast";

async function check() {
  S.update = await api<UpdateInfo>("CheckUpdate");
  if (!S.update.version) toast("Masz najnowszą wersję", "ok");
}
async function install() {
  toast("Pobieram aktualizację…");
  S.update = await api<UpdateInfo>("InstallUpdate");
  toast(S.update.installed ? `Zainstalowano ${S.update.version}` : "Instalacja się nie udała", S.update.installed ? "ok" : "err");
}
const restart = () => api("Restart");
</script>

<template>
  <h2>O szepcie</h2>
  <div class="block">
    <div class="card-id about">
      <div class="nm">szept</div>
      <div class="st">wersja {{ S.version || "dev" }}</div>
      <template v-if="!S.boot?.mobile">
        <div v-if="!S.update?.version" class="btn-row"><button class="btn" @click="check">Sprawdź aktualizacje</button></div>
        <template v-else-if="S.update.installed">
          <div class="st">{{ S.update.version }} zainstalowana, działa od następnego uruchomienia</div>
          <div class="btn-row"><button class="btn primary" @click="restart">Uruchom ponownie</button></div>
        </template>
        <template v-else-if="S.update.err">
          <div class="st">nie udało się zainstalować {{ S.update.version }}: {{ S.update.err }}. Pobierz ją z github.com/OppaiHacker/Szept/releases.</div>
          <div class="btn-row"><button class="btn" @click="install">Spróbuj ponownie</button></div>
        </template>
        <template v-else>
          <div class="st">jest nowa wersja {{ S.update.version }}</div>
          <div class="btn-row"><button class="btn primary" @click="install">Zainstaluj</button></div>
        </template>
      </template>
      <div class="bio">Szyfrowany komunikator P2P. Historia to podpisany DAG zdarzeń,
transport to WebRTC (pion), a stan leży lokalnie w bbolt.
Connector kojarzy peerów i nie widzi treści.</div>
    </div>
  </div>
  <div class="block">
    <h4>Licencja</h4>
    <p class="hint-text">Szept to wolne oprogramowanie na licencji GNU GPL w wersji 3 lub dowolnej późniejszej
(GPL-3.0-or-later). Możesz go używać, zmieniać i rozpowszechniać na jej warunkach.</p>
    <p class="hint-text">Program jest dostarczany „tak jak jest”, <b>bez żadnej gwarancji</b>, w tym gwarancji
bezpieczeństwa i przydatności do określonego celu, w zakresie dozwolonym przez prawo.
Szczegóły w punktach 15 i 16 licencji: https://www.gnu.org/licenses/gpl-3.0.html</p>
  </div>
</template>
