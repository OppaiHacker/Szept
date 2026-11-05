<script setup lang="ts">
import { computed } from "vue";
import type { Msg } from "@/api/types";
import { humanSize } from "@/lib/format";
import { isSaved, saveGif, unsaveGif } from "@/lib/gifs";
import { fetchFile, saveFile } from "@/store/messages";
import { openLightbox } from "@/ui/lightbox";
import { toast } from "@/ui/toast";
import Icon from "@/components/base/Icon.vue";
import MediaPlayer from "./MediaPlayer.vue";

// A file in a message: a player for audio and video, the picture for images,
// otherwise a tile that fetches or saves it.
const props = defineProps<{ m: Msg }>();
const av = computed(() => {
  const t = props.m.mime || "";
  return t.startsWith("video/") ? "video" : t.startsWith("audio/") || t === "application/ogg" ? "audio" : "";
});
const state = computed(() => props.m.have ? humanSize(props.m.size)
  : props.m.getting ? "pobieranie…" : `${humanSize(props.m.size)} · kliknij, aby pobrać`);

function star() {
  const on = !isSaved(props.m.blob);
  if (on) saveGif(props.m.blob!, props.m.file); else unsaveGif(props.m.blob!);
  toast(on ? "GIF zapisany" : "Usunięto z zapisanych GIF-ów", "ok");
}
</script>

<template>
  <MediaPlayer v-if="av && m.have" :m="m" :kind="av" />
  <span v-else-if="m.mime?.startsWith('image/') && m.src" class="attach" :class="{ pending: !m.have }">
    <img :src="m.src" :alt="m.file" loading="lazy" @click="openLightbox(m.src!)">
    <button v-if="m.mime === 'image/gif' && m.have" class="gif-star" :class="{ on: isSaved(m.blob) }" title="Zapisz GIF" @click="star">
      <Icon name="star" />
    </button>
  </span>
  <button v-else class="file" @click="m.have ? saveFile(m) : fetchFile(m)">
    <span class="ico"><Icon :name="m.have ? 'save' : 'file'" /></span>
    <span class="nm"><b>{{ m.file }}</b><span>{{ state }}</span></span>
  </button>
</template>
