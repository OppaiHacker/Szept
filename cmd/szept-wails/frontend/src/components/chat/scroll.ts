import { ref } from "vue";

// Whether the open chat's message list sits at the bottom. The composer shows
// the way back down when it does not.
export const atBottom = ref(true);

// Only this jump glides: the automatic scroll after a new message must stay
// instant, or the list would still be travelling when the next one lands.
export function jumpDown() {
  const list = document.getElementById("msgs");
  list?.scrollTo({ top: list.scrollHeight, behavior: "smooth" });
}
