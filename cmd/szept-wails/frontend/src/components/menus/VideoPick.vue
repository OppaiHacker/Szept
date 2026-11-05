<script setup lang="ts">
import { reactive } from "vue";
import type { VideoOpts } from "@/api/types";
import { act } from "@/store/actions";
import { closePopover } from "@/ui/popover";

// How to start a camera or a screen share. The last pick is preselected.
const props = defineProps<{ kind: "cam" | "screen"; callKey: string; picks: VideoOpts; monitors: string[] }>();

const CHOICES = {
  fps: [[15, "15 fps"], [30, "30 fps"], [60, "60 fps"]],
  cam: [[480, "480p"], [720, "720p"], [1080, "1080p"]],
  screen: [[720, "720p"], [1080, "1080p"], [1440, "1440p"]],
  quality: [[40, "Niska"], [60, "Średnia"], [85, "Wysoka"]],
} as const;

const v = reactive({ d: 0, fps: props.picks.fps ?? 30, height: props.picks.height ?? 720, quality: props.picks.quality ?? 60 });
const rows = [
  { k: "fps" as const, label: "Klatki", list: CHOICES.fps },
  { k: "height" as const, label: "Rozdzielczość", list: CHOICES[props.kind] },
  { k: "quality" as const, label: "Jakość", list: CHOICES.quality },
];

function go() {
  closePopover();
  const o = { fps: v.fps, height: v.height, quality: v.quality };
  if (props.kind === "cam") act("ToggleCam", props.callKey, o);
  else act("ToggleScreen", props.callKey, v.d, o);
}
</script>

<template>
  <div class="vpick">
    <label v-if="monitors.length > 1" class="vp-row"><span>Monitor</span>
      <select v-model.number="v.d" class="input">
        <option v-for="(d, i) in monitors" :key="i" :value="i">Monitor {{ i + 1 }} · {{ d }}</option>
      </select></label>
    <label v-for="r in rows" :key="r.k" class="vp-row"><span>{{ r.label }}</span>
      <select v-model.number="v[r.k]" class="input">
        <option v-for="[n, t] in r.list" :key="n" :value="n">{{ t }}</option>
      </select></label>
    <button class="btn primary" @click="go">{{ kind === "cam" ? "Włącz kamerę" : "Udostępnij ekran" }}</button>
  </div>
</template>
