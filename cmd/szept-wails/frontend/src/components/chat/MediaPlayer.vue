<script setup lang="ts">
import { onMounted, reactive, ref, watch } from "vue";
import type { Msg } from "@/api/types";
import { clock, humanSize } from "@/lib/format";
import { mediaURL } from "@/lib/frames";
import Icon from "@/components/base/Icon.vue";
import { volume } from "./volume";

// Audio and video in a message. WebKitGTK's own controls only mute and leave
// <audio> on a spinner forever, so the bar is ours.
const props = defineProps<{ m: Msg; kind: "audio" | "video" }>();
const media = ref<HTMLMediaElement>();
const box = ref<HTMLElement>();
const st = reactive({ paused: true, cur: 0, dur: 0, muted: false });

function sync() {
  const el = media.value!;
  Object.assign(st, { paused: el.paused, cur: el.currentTime, dur: el.duration, muted: el.muted || !el.volume });
}
onMounted(() => {
  media.value!.volume = volume.value;
  mediaURL(props.m.blob!).then((u) => { if (media.value) media.value.src = u; }).catch(() => {});
});
// one volume for every player, remembered across restarts
watch(volume, (v) => { if (media.value) { media.value.volume = v; media.value.muted = false; } });

const toggle = () => { const el = media.value!; el.paused ? el.play().catch(() => {}) : el.pause(); };
const seek = (e: Event) => {
  const el = media.value!, v = +(e.target as HTMLInputElement).value;
  if (el.duration > 0) el.currentTime = (v / 1000) * el.duration;
};
const setVol = (e: Event) => { volume.value = +(e.target as HTMLInputElement).value; };
const full = () => (document.fullscreenElement ? document.exitFullscreen() : box.value!.requestFullscreen());
const pos = () => (st.dur > 0 && isFinite(st.dur) ? (st.cur / st.dur) * 1000 : 0);
</script>

<template>
  <div ref="box" :class="kind === 'video' ? 'video' : 'file audio'">
    <span v-if="kind === 'audio'" class="nm"><b>{{ m.file }}</b><span>{{ humanSize(m.size) }}</span></span>
    <component :is="kind" ref="media" preload="metadata"
      @timeupdate="sync" @durationchange="sync" @play="sync" @pause="sync" @ended="sync" @volumechange="sync"
      @click="kind === 'video' && toggle()" />
    <div class="mbar">
      <button class="mb-play" title="Odtwórz" @click="toggle"><Icon :name="st.paused ? 'play' : 'pause'" /></button>
      <span class="mb-cur">{{ clock(st.cur) }}</span>
      <input class="slider mb-seek" type="range" min="0" max="1000" :value="pos()" @input="seek">
      <span class="mb-dur">{{ clock(st.dur) }}</span>
      <span class="mb-v">
        <button class="mb-mute" title="Wycisz" @click="media!.muted = !media!.muted"><Icon :name="st.muted ? 'deaf' : 'volume'" /></button>
        <input class="slider mb-vol" type="range" min="0" max="1" step="0.02" :value="st.muted ? 0 : volume" title="Głośność" @input="setVol">
      </span>
      <button v-if="kind === 'video'" class="mb-full" title="Pełny ekran" @click="full"><Icon name="expand" /></button>
    </div>
  </div>
</template>
