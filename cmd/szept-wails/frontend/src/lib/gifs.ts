import { ref, watch } from "vue";
import { load, save } from "./env";

// Saved GIFs: only ones that came through a chat, kept as the blob hash on
// this device. Sending one again re-reads the local copy and posts it as a new file.
export const gifs = ref(load<{ blob: string; file?: string }[]>("szept.gifs", []));
watch(gifs, (v) => save("szept.gifs", v.slice(0, 200)), { deep: true });

export const isSaved = (blob?: string) => !!blob && gifs.value.some((g) => g.blob === blob);
export function saveGif(blob: string, file?: string) {
  gifs.value = [{ blob, file }, ...gifs.value.filter((g) => g.blob !== blob)];
}
export function unsaveGif(blob: string) {
  gifs.value = gifs.value.filter((g) => g.blob !== blob);
}
