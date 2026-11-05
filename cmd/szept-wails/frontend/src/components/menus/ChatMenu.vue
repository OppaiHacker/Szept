<script setup lang="ts">
import { computed } from "vue";
import { api } from "@/api/wails";
import { leaveChat } from "@/store/actions";
import { categoryDialog, channelDialog, inviteDialog, membersDialog, renameDialog, serverLook } from "@/store/dialogs";
import { S } from "@/store/state";
import { confirmBox } from "@/ui/dialogs";
import { fail, toast } from "@/ui/toast";
import MenuItem from "./MenuItem.vue";

// The open chat's menu (header gear, group name).
const c = computed(() => S.conv!);

async function rekey() {
  if (await confirmBox("Obrócić klucz?", "Powstanie nowa epoka; peerzy dostaną nowy klucz przy najbliższym kontakcie.", false)) {
    try { await api("Rekey", S.chat); toast("Klucz obrócony", "ok"); } catch (e) { fail(e); }
  }
}
const unhide = () => api("UnhideAll", S.chat).catch(fail);
async function leave() {
  const x = c.value, owner = x.isOwner;
  if (!(await confirmBox(owner ? "Usunąć serwer?" : "Opuścić serwer?",
    owner ? "Serwer zniknie z tego urządzenia razem z historią. Możesz go usunąć, gdy nie ma w nim już nikogo poza tobą."
      : "Pozostali zobaczą, że opuszczasz serwer. Historia zniknie z tego urządzenia; wrócić można tylko przez nowe zaproszenie."))) return;
  try { await api("Leave", x.id); leaveChat({ space: "home", fpage: false }); } catch (e) { fail(e); }
}
</script>

<template>
  <div v-if="S.conv" class="menu">
    <MenuItem v-if="!c.direct" label="Zaproś ludzi" icon="key" :run="inviteDialog" />
    <template v-if="!c.direct && c.isOwner">
      <MenuItem label="Ustawienia serwera" icon="edit" :run="() => serverLook(c.id)" />
      <MenuItem label="Utwórz kanał" icon="plus" :run="() => channelDialog('', 'text')" />
      <MenuItem label="Utwórz kategorię" icon="plus" :run="categoryDialog" />
    </template>
    <MenuItem label="Zmień nazwę" icon="edit" :run="() => renameDialog()" />
    <MenuItem v-if="!c.direct" label="Członkowie" icon="people" :run="membersDialog" />
    <MenuItem v-if="c.hidden" :label="`Pokaż schowane (${c.hidden})`" icon="eye" :run="unhide" />
    <i class="menu-sep" />
    <MenuItem label="Obróć klucz" icon="shield" :run="rekey" />
    <MenuItem v-if="!c.direct" :label="c.isOwner ? 'Usuń serwer' : 'Opuść serwer'" icon="logout" danger :run="leave" />
  </div>
</template>
