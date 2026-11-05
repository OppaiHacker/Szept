<script setup lang="ts">
import { computed, watchEffect } from "vue";
import type { Conversation } from "@/api/types";
import { act, leaveChat, setFocus } from "@/store/actions";
import { joinVoice, startVideo } from "@/store/calls";
import { chanName, chatById, liveFrames } from "@/store/getters";
import { S, peerVol } from "@/store/state";
import Icon from "@/components/base/Icon.vue";
import VideoTile from "./VideoTile.vue";
import type { Tile } from "./tile";

// The voice view: one tile per person in the call, plus one per shared screen.
const props = defineProps<{ conv: Conversation }>();
const key = computed(() => S.voice!);
const ch = computed(() => key.value.slice(S.chat.length));
const grp = computed(() => chatById(S.chat));
const joined = computed(() => grp.value?.callKey === key.value);
const k = computed(() => grp.value?.call);

const tiles = computed(() => {
  const people = joined.value ? props.conv.members.filter((m) => m.inCall)
    : props.conv.members.filter((m) => (grp.value?.voice[ch.value] || []).includes(m.name));
  const frames = new Map(liveFrames(key.value));
  const out: Tile[] = [];
  for (const m of people) {
    const base = { pub: m.pub, name: m.name, avatar: m.avatar, flags: m.flags, muted: peerVol.get(m.pub)?.mute };
    const cam = frames.get(`${key.value}.${m.pub}.c`);
    out.push({ ...base, key: m.pub, src: cam && `/frame/${key.value}.${m.pub}.c?n=${cam.seq}` });
    const scr = frames.get(`${key.value}.${m.pub}.s`);
    if (scr) out.push({ ...base, key: m.pub + ".s", screen: true, src: `/frame/${key.value}.${m.pub}.s?n=${scr.seq}` });
  }
  return out;
});

// a focused tile whose picture stopped drops back into the grid
watchEffect(() => { if (S.focus && !tiles.value.some((t) => t.key === S.focus && t.src)) setFocus(null); });
const el = (e: Event) => e.currentTarget as Element;
</script>

<template>
  <header class="conv-head">
    <div class="head-main">
      <button class="icon-btn back" title="Wróć" @click="leaveChat({ fpage: false })"><Icon name="chev" /></button>
      <span class="h-ico"><Icon name="volume" /></span><b class="t">{{ chanName(grp, ch, "voice") }}</b><i class="h-div" />
      <span class="s">{{ grp?.name || "" }}</span><span class="spacer" />
    </div>
  </header>
  <div v-appear class="vview">
    <div class="vtiles" :class="{ focused: !!S.focus }" :data-n="Math.min(tiles.length, 4)">
      <VideoTile v-for="t in tiles" :key="t.key" :t="t" />
      <div v-if="!tiles.length" class="empty"><i class="empty-ico"><Icon name="volume" /></i><strong>Pusto</strong>Nikogo nie ma w tym kanale.</div>
    </div>
    <div class="vbar">
      <template v-if="joined && k">
        <button class="ctl" :class="{ off: !k.mic }" title="Mikrofon" @click="act('ToggleMic', key)"><Icon :name="k.mic ? 'mic' : 'micOff'" /></button>
        <button class="ctl" :class="{ off: k.deaf }" title="Głośnik" @click="act('ToggleDeafen', key)"><Icon :name="k.deaf ? 'deaf' : 'volume'" /></button>
        <button class="ctl" :class="{ on: k.cam }" title="Kamera" @click="startVideo(el($event), 'cam', key)"><Icon name="cam" /></button>
        <button class="ctl" :class="{ on: k.screen }" title="Udostępnij ekran" @click="startVideo(el($event), 'screen', key)"><Icon name="screen" /></button>
        <button class="ctl hang" title="Rozłącz" @click="act('LeaveCall', key)"><Icon name="hangup" /></button>
      </template>
      <button v-else class="btn join" @click="joinVoice(S.chat, ch)"><Icon name="phone" />Dołącz do kanału</button>
    </div>
  </div>
</template>
