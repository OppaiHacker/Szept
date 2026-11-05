import { ref, watch } from "vue";
import { load, save } from "@/lib/env";

// the volume of every media player in the chats, remembered across restarts
export const volume = ref(load("szept.vol", 1));
watch(volume, (v) => save("szept.vol", v));
