<script setup lang="ts">
import { onMounted, ref } from "vue";
import { api } from "@/api/wails";
import type { Crop } from "@/api/types";
import { afterEdit, refresh } from "@/store/actions";
import { closeModal } from "@/ui/modal";
import { fail } from "@/ui/toast";
import ModalFrame from "@/components/overlay/ModalFrame.vue";

// Pan/zoom framing of a picture. The crop rectangle is reported in *source*
// pixels, which is exactly what attach.Avatar wants. kind: avatar | banner,
// or with chat set a group's icon | banner.
const props = defineProps<{ kind: "avatar" | "banner" | "icon"; src: string; chat?: string }>();
const round = props.kind === "avatar" || props.kind === "icon";
const ratio = round ? 1 : 3;
const title = props.chat ? (round ? "Wykadruj ikonę serwera" : "Wykadruj baner serwera") : round ? "Wykadruj awatar" : "Wykadruj baner";

const stage = ref<HTMLElement>(), img = ref<HTMLImageElement>(), mask = ref<HTMLElement>();
const zoom = ref(1);
let fw = 0, fh = 0, base = 1, scale = 1, tx = 0, ty = 0, dragging = false, px = 0, py = 0;

// Dragged past the edge the picture keeps following the pointer, just less
// and less: a hard stop reads as "frozen", progressive resistance reads as
// "still responding, there is simply nothing more here".
const rubber = (over: number, dim: number) => (over * dim * 0.55) / (dim + 0.55 * Math.abs(over));
const give = (v: number, max: number, dim: number) => (Math.abs(v) <= max ? v : Math.sign(v) * (max + rubber(Math.abs(v) - max, dim)));

function apply(settle = false) {
  const i = img.value!, s = stage.value!;
  scale = base * zoom.value;
  const dw = i.naturalWidth * scale, dh = i.naturalHeight * scale;
  const mx = Math.max(0, (dw - fw) / 2), my = Math.max(0, (dh - fh) / 2);
  if (!dragging) { // release and zoom both pull the frame back inside the picture
    tx = Math.min(mx, Math.max(-mx, tx));
    ty = Math.min(my, Math.max(-my, ty));
  }
  i.style.transition = settle ? "left .32s var(--ease), top .32s var(--ease)" : "none";
  Object.assign(i.style, {
    width: `${dw}px`, height: `${dh}px`,
    left: `${(s.clientWidth - dw) / 2 + (dragging ? give(tx, mx, fw) : tx)}px`,
    top: `${(s.clientHeight - dh) / 2 + (dragging ? give(ty, my, fh) : ty)}px`,
  });
}

function layout() {
  const s = stage.value!, i = img.value!;
  const sw = s.clientWidth, sh = s.clientHeight;
  fw = Math.min(sw - 32, (sh - 32) * ratio);
  fh = fw / ratio;
  Object.assign(mask.value!.style, { left: `${(sw - fw) / 2}px`, top: `${(sh - fh) / 2}px`, width: `${fw}px`, height: `${fh}px`, inset: "auto" });
  base = Math.max(fw / i.naturalWidth, fh / i.naturalHeight);
  apply();
}
onMounted(() => { const i = img.value!; if (i.complete && i.naturalWidth) layout(); else i.onload = layout; });

function rect(): Crop {
  const i = img.value!;
  const w = Math.round(fw / scale), h = Math.round(fh / scale);
  const cx = i.naturalWidth / 2 - tx / scale, cy = i.naturalHeight / 2 - ty / scale;
  const clamp = (v: number, hi: number) => Math.max(0, Math.min(hi, Math.round(v)));
  return { X: clamp(cx - w / 2, i.naturalWidth - w), Y: clamp(cy - h / 2, i.naturalHeight - h), W: w, H: h };
}

function down(e: PointerEvent) { dragging = true; px = e.clientX; py = e.clientY; stage.value!.setPointerCapture(e.pointerId); }
function move(e: PointerEvent) {
  if (!dragging) return;
  tx += e.clientX - px; ty += e.clientY - py; px = e.clientX; py = e.clientY;
  apply();
}
function release() { if (!dragging) return; dragging = false; apply(true); }
function setZoom(e: Event) { zoom.value = +(e.target as HTMLInputElement).value; apply(); }
function wheel(e: WheelEvent) { zoom.value = Math.max(1, Math.min(4, zoom.value - e.deltaY / 600)); apply(); }

// save (with a crop) or clear the picture
async function done(r: Crop | "clear") {
  closeModal(true);
  try {
    if (props.chat) {
      await api("SetServerImage", props.chat, props.kind, r === "clear" ? "" : props.src, r === "clear" ? {} : r);
      await refresh();
    } else {
      if (r === "clear") await api(round ? "ClearAvatar" : "ClearBanner");
      else await api(round ? "SetAvatar" : "SetBanner", props.src, r);
      await afterEdit("Zapisane");
    }
  } catch (e) { fail(e); }
}
</script>

<template>
  <ModalFrame :title="title" :wide="!round">
    <div ref="stage" class="crop-stage" @pointerdown="down" @pointermove="move" @pointerup="release" @pointercancel="release"
      @wheel.prevent="wheel">
      <img ref="img" :src="src" alt=""><div ref="mask" class="crop-mask" :class="{ round }" />
    </div>
    <div class="field"><label>Powiększenie</label>
      <input class="slider" type="range" min="1" max="4" step="0.01" :value="zoom" @input="setZoom"></div>
    <p class="hint-text pad">Przeciągnij obraz, żeby przesunąć kadr.</p>
    <template #actions>
      <button class="btn ghost" @click="closeModal(null)">Anuluj</button>
      <button class="btn primary" @click="done(rect())">Zapisz</button>
      <button class="btn danger" @click="done('clear')">Usuń obraz</button>
    </template>
  </ModalFrame>
</template>
