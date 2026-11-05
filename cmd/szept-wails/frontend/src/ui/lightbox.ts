import { ref } from "vue";

// a picture blown up over the whole window; a click anywhere closes it
export const lightboxSrc = ref<string | null>(null);
export const openLightbox = (src: string) => (lightboxSrc.value = src);
export const closeLightbox = () => (lightboxSrc.value = null);
