<script setup lang="ts">
import { computed } from "vue";
import { nameFx } from "@/lib/profileStyle";
import { act } from "@/store/actions";
import { callChat, styleOf } from "@/store/getters";
import { openSettings } from "@/store/settings";
import { S } from "@/store/state";
import { profilePop } from "@/store/dialogs";
import Icon from "@/components/base/Icon.vue";
import PresenceAvatar from "@/components/base/PresenceAvatar.vue";

// The account strip under the list: who you are, whether the connector
// carries you, and the way into settings. Mic and headphones live here like
// on Discord: in a call they drive the call, outside one they are what the
// next call starts with.
const me = computed(() => S.snap!.me);
const k = computed(() => callChat()?.call);
const mic = computed(() => (k.value ? k.value.mic : !S.selfMute));
const deaf = computed(() => (k.value ? k.value.deaf : S.selfDeaf));

function toggleMic() {
  const g = callChat();
  if (g) return act("ToggleMic", g.callKey);
  S.selfMute = !S.selfMute;
}
function toggleDeaf() {
  const g = callChat();
  if (g) return act("ToggleDeafen", g.callKey);
  S.selfDeaf = !S.selfDeaf;
}
</script>

<template>
  <div class="userbar">
    <button class="ub-id" title="Mój profil" @click="profilePop(me.pub, $event.currentTarget as Element)">
      <PresenceAvatar :url="me.avatar" :name="me.name" :seed="me.pub" :online="me.connUp" size="s" />
      <span class="nm"><b v-bind="nameFx(styleOf(me.pub))">{{ me.name }}</b><span>{{ me.status || (me.connUp ? "online" : "offline") }}</span></span>
    </button>
    <button class="icon-btn" :class="{ off: !mic }" :title="mic ? 'Wycisz mikrofon' : 'Włącz mikrofon'" @click="toggleMic">
      <Icon :name="mic ? 'mic' : 'micOff'" />
    </button>
    <button class="icon-btn" :class="{ off: deaf }" :title="deaf ? 'Włącz dźwięk' : 'Wyłącz dźwięk'" @click="toggleDeaf">
      <Icon :name="deaf ? 'deaf' : 'volume'" />
    </button>
    <button class="icon-btn" title="Ustawienia" @click="openSettings()"><Icon name="gear" /></button>
  </div>
</template>
