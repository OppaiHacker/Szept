<script setup lang="ts">
import { computed, ref } from "vue";
import { openVoice, setFocus } from "@/store/actions";
import { callChat, chanName, liveFrames } from "@/store/getters";
import { S } from "@/store/state";
import FrameCanvas from "./FrameCanvas.vue";

// Floating window: while you are in a call with video and look at something
// else, the picture keeps playing in a small window you can drag anywhere.
// A click on it goes back to the voice view.
const g = computed(() => callChat());
const pick = computed(() => {
  const key = g.value?.callKey;
  if (!key || S.voice === key) return null;
  // someone else's screen first, then any camera, your own preview last
  const me = S.snap!.me.pub;
  const fr = liveFrames(key).sort(([a], [b]) =>
    (+b.endsWith(".s") - +a.endsWith(".s")) || (+a.includes(me) - +b.includes(me)));
  if (!fr.length) return null;
  const [k, f] = fr[0];
  return {
    key, src: `/frame/${k}?n=${f.seq}`,
    tile: k.slice(key.length + 1).replace(/\.c$/, ""),
    cap: f.name + (k.endsWith(".s") ? " · ekran" : "") + " · " + chanName(g.value, key.slice(g.value!.id.length), "voice"),
  };
});

const pos = computed(() => (S.pip ? { left: S.pip[0] + "px", top: S.pip[1] + "px", right: "auto", bottom: "auto" } : undefined));
const box = ref<HTMLElement>();

function down(e: PointerEvent) {
  e.preventDefault(); // a drag, not a text selection across the page
  const el = box.value!, r = el.getBoundingClientRect(), ox = e.clientX - r.left, oy = e.clientY - r.top;
  let moved = false;
  el.setPointerCapture(e.pointerId);
  const move = (ev: PointerEvent) => {
    if (!moved && Math.hypot(ev.clientX - e.clientX, ev.clientY - e.clientY) < 4) return;
    moved = true;
    S.pip = [Math.max(8, Math.min(innerWidth - r.width - 8, ev.clientX - ox)), Math.max(8, Math.min(innerHeight - r.height - 8, ev.clientY - oy))];
  };
  el.addEventListener("pointermove", move);
  el.addEventListener("pointerup", () => {
    el.removeEventListener("pointermove", move);
    const p = pick.value;
    if (!moved && p) openVoice(p.key).then(() => setFocus(p.tile));
  }, { once: true });
}
</script>

<template>
  <div v-if="pick" id="pip" ref="box" :style="pos" @pointerdown="down">
    <FrameCanvas :src="pick.src" /><span class="cap">{{ pick.cap }}</span>
  </div>
</template>
