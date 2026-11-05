<script setup lang="ts">
import { computed } from "vue";
import { act, setPeer } from "@/store/actions";
import { profilePop } from "@/store/dialogs";
import { chatById } from "@/store/getters";
import { befriend, dmWith } from "@/store/people";
import { openSettings } from "@/store/settings";
import { S, peerVol } from "@/store/state";
import MenuItem from "./MenuItem.vue";

// What you can do with a person in the open chat. Your own entry carries your
// call controls instead.
const props = defineProps<{ pub: string }>();
const m = computed(() => S.conv?.members.find((p) => p.pub === props.pub));
const g = computed(() => chatById(S.conv?.id || ""));
const v = computed(() => ({ gain: 1, mute: false, ...peerVol.get(props.pub) }));
const friend = computed(() => S.snap!.friends.some((f) => f.pub === props.pub));
// the call they are in: the voice map lists names per channel
const theirCall = computed(() => {
  const x = g.value, p = m.value;
  if (!x || !p) return "";
  const ch = Object.keys(x.voice || {}).find((k) => x.voice[k].includes(p.name));
  return ch !== undefined ? x.id + ch : p.inCall ? x.callKey : "";
});
</script>

<template>
  <div v-if="m" class="menu">
    <MenuItem label="Profil" :run="() => profilePop(pub)" />
    <template v-if="m.me">
      <MenuItem label="Edytuj profil" :run="() => openSettings('profile', true)" />
      <template v-if="g?.call">
        <i class="menu-sep" />
        <MenuItem :label="g.call.mic ? 'Wycisz mikrofon' : 'Włącz mikrofon'" :run="() => act('ToggleMic', g!.callKey)" />
        <MenuItem :label="g.call.deaf ? 'Włącz dźwięk' : 'Wycisz dźwięk'" :run="() => act('ToggleDeafen', g!.callKey)" />
        <MenuItem label="Rozłącz" danger :run="() => act('LeaveCall', g!.callKey)" />
      </template>
    </template>
    <template v-else>
      <MenuItem label="Napisz wiadomość" :run="() => dmWith(pub)" />
      <MenuItem v-if="!friend" label="Dodaj do znajomych" :run="() => befriend(pub)" />
      <template v-if="m.inCall">
        <i class="menu-sep" />
        <div class="menu-vol"><span>Głośność użytkownika <b class="mono">{{ Math.round(v.gain * 100) }}%</b></span>
          <input class="slider" type="range" min="0" max="2" step="0.05" :value="v.gain"
            @input="setPeer(pub, { gain: +($event.target as HTMLInputElement).value })"></div>
        <label class="menu-item check">Wycisz<input type="checkbox" :checked="v.mute"
          @change="setPeer(pub, { mute: ($event.target as HTMLInputElement).checked })"></label>
      </template>
      <template v-if="g?.isOwner && theirCall">
        <i class="menu-sep" />
        <MenuItem label="Rozłącz z kanału" danger :run="() => act('KickFromCall', theirCall, pub)" />
      </template>
    </template>
  </div>
</template>
