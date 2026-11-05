<script setup lang="ts">
import { computed, shallowRef, watchEffect } from "vue";
import type { Chat } from "@/api/types";
import { act, openChat, openVoice } from "@/store/actions";
import { startVideo } from "@/store/calls";
import { spatialDialog } from "@/store/dialogs";
import { callChat, chanName } from "@/store/getters";
import { S } from "@/store/state";
import Icon from "@/components/base/Icon.vue";

// The voice panel sits above the account strip while you are in a call. It
// keeps its old content while it folds away, so the collapse has something to show.
const live = computed(() => callChat());
const last = shallowRef<Chat | null>(null);
watchEffect(() => { if (live.value) last.value = live.value; });
const c = computed(() => live.value || last.value);
const where = computed(() => {
  const x = c.value!;
  return x.direct ? x.name : `${chanName(x, x.callKey.slice(x.id.length), "voice")} / ${x.name}`;
});
const el = (e: Event) => e.currentTarget as Element;

async function spatial() {
  if (c.value && c.value.id !== S.chat) await openChat(c.value.id);
  spatialDialog();
}
</script>

<template>
  <div class="voice" :class="{ open: !!live }">
    <div class="voice-in">
      <template v-if="c?.call">
        <div class="vc-top">
          <button class="vc-tx" @click="openVoice(c.callKey)"><b>Połączono z głosem</b><span>{{ where }}</span></button>
          <button class="icon-btn hang" title="Rozłącz" @click="act('LeaveCall', c.callKey)"><Icon name="hangup" /></button>
        </div>
        <div class="vc-ctl">
          <button class="ph" :class="{ off: !c.call.mic }" title="Mikrofon" @click="act('ToggleMic', c.callKey)"><Icon :name="c.call.mic ? 'mic' : 'micOff'" /></button>
          <button class="ph" :class="{ off: c.call.deaf }" title="Dźwięk" @click="act('ToggleDeafen', c.callKey)"><Icon :name="c.call.deaf ? 'deaf' : 'volume'" /></button>
          <button :class="{ on: c.call.cam }" title="Kamera" @click="startVideo(el($event), 'cam', c.callKey)"><Icon name="cam" /></button>
          <button :class="{ on: c.call.screen }" title="Ekran" @click="startVideo(el($event), 'screen', c.callKey)"><Icon name="screen" /></button>
          <button title="Przestrzeń dźwięku" @click="spatial"><Icon name="people" /></button>
        </div>
      </template>
    </div>
  </div>
</template>
