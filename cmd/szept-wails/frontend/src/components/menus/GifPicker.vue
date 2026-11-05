<script setup lang="ts">
import { gifs, unsaveGif } from "@/lib/gifs";
import { sendFile } from "@/store/messages";
import { closePopover } from "@/ui/popover";
import { fail } from "@/ui/toast";
import Icon from "@/components/base/Icon.vue";

// Saved GIFs; a click sends the local copy again as a new file.
async function send(g: { blob: string; file?: string }) {
  closePopover();
  try {
    const data = await (await fetch("/blob/" + g.blob)).blob();
    if (!data.size) throw new Error("Tego GIF-a nie ma już na tym urządzeniu.");
    await sendFile(new File([data], g.file || "gif.gif", { type: "image/gif" }));
  } catch (e) { fail(e); }
}
</script>

<template>
  <div class="gifs">
    <div v-if="gifs.length" class="gif-grid">
      <div v-for="g in gifs" :key="g.blob" class="gif-cell">
        <button class="gif-send" :title="g.file || 'GIF'" @click="send(g)"><img :src="'/blob/' + g.blob" alt="" loading="lazy"></button>
        <button class="gif-x" title="Usuń z zapisanych" @click="unsaveGif(g.blob)"><Icon name="close" /></button>
      </div>
    </div>
    <div v-else class="quiet"><i class="empty-ico"><Icon name="star" /></i><b>Brak zapisanych GIF-ów</b>Kliknij GIF na czacie prawym przyciskiem i wybierz „Zapisz GIF”.</div>
  </div>
</template>
