<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { setPeer } from "@/store/actions";
import { S, peerVol } from "@/store/state";
import { closeModal } from "@/ui/modal";
import Avatar from "@/components/base/Avatar.vue";
import ModalFrame from "@/components/overlay/ModalFrame.vue";

// Drag a participant around the room to place them in the stereo field. You
// sit at the bottom middle; distance from you sets a person's gain, the side
// sets the pan. Positions stay in peerVol, so reopening the room shows where
// everyone was left.
const EAR = [0.5, 0.86] as const;
const me = computed(() => S.conv?.members.find((m) => m.me));
const peers = computed(() => S.conv?.members.filter((m) => m.inCall && !m.me) || []);
const room = ref<HTMLElement>();
const pos = reactive(new Map<string, [number, number]>());
const held = ref("");

const mix = ([fx, fy]: [number, number]) => ({
  pan: Math.max(-1, Math.min(1, (fx - EAR[0]) * 2.2)),
  gain: Math.max(0.1, Math.min(1.5, 1.35 - Math.hypot(fx - EAR[0], fy - EAR[1]) * 1.6)),
});
function label(pub: string) {
  const { gain, pan } = mix(pos.get(pub) || [EAR[0], EAR[1]]);
  return `${Math.round(gain * 100)}%${Math.abs(pan) > 0.05 ? ` · ${pan < 0 ? "L" : "P"}${Math.round(Math.abs(pan) * 100)}` : ""}`;
}
function place(pub: string, p: [number, number], apply: boolean) {
  pos.set(pub, p);
  if (apply) setPeer(pub, { ...mix(p), pos: p });
}

// someone placed before keeps their spot; a newcomer goes on an arc around
// you, spread left to right
onMounted(() => peers.value.forEach((m, i) => {
  const was = peerVol.get(m.pub)?.pos;
  if (was) return place(m.pub, was, false);
  const n = peers.value.length, a = (n === 1 ? 0 : i / (n - 1) - 0.5) * 1.6;
  place(m.pub, [EAR[0] + Math.sin(a) * 0.3, EAR[1] - Math.cos(a) * 0.45], true);
}));

function down(e: PointerEvent, pub: string) {
  e.preventDefault(); // no text selection, no native image drag
  (e.currentTarget as Element).setPointerCapture(e.pointerId);
  held.value = pub;
}
function move(e: PointerEvent, pub: string) {
  if (!(e.currentTarget as Element).hasPointerCapture(e.pointerId)) return;
  const r = room.value!.getBoundingClientRect();
  place(pub, [
    Math.max(0.06, Math.min(0.94, (e.clientX - r.left) / r.width)),
    Math.max(0.1, Math.min(0.9, (e.clientY - r.top) / r.height)),
  ], true);
}
const at = (p?: [number, number]) => (p ? { left: p[0] * 100 + "%", top: p[1] * 100 + "%" } : {});
</script>

<template>
  <ModalFrame title="Przestrzeń dźwięku" wide>
    <p>Przeciągnij osobę: bliżej ciebie = głośniej, w bok = w lewy lub prawy głośnik.</p>
    <div ref="room" class="room">
      <i v-for="r in [0.25, 0.5, 0.75]" :key="r" class="room-ring" :style="{ '--r': r }" />
      <span class="room-side l">L</span><span class="room-side r">P</span>
      <div class="room-me" :style="at([EAR[0], EAR[1]])"><Avatar :url="me?.avatar" :name="me?.name || 'Ty'" :seed="me?.pub || 'me'" size="m" /><b>Ty</b></div>
      <div v-for="m in peers" :key="m.pub" class="room-peer" :class="{ held: held === m.pub }" :style="at(pos.get(m.pub))"
        @pointerdown="down($event, m.pub)" @pointermove="move($event, m.pub)" @pointerup="held = ''" @lostpointercapture="held = ''">
        <Avatar :url="m.avatar" :name="m.name" :seed="m.pub" size="m" /><b>{{ m.name }}</b><span class="mono">{{ label(m.pub) }}</span>
      </div>
    </div>
    <template #actions><button class="btn ghost" @click="closeModal(null)">Gotowe</button></template>
  </ModalFrame>
</template>
