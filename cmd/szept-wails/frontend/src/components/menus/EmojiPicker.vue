<script setup lang="ts">
import { computed, ref } from "vue";
import { EMOJI } from "@/lib/emoji";
import { load, save } from "@/lib/env";
import { closePopover } from "@/ui/popover";
import Icon from "@/components/base/Icon.vue";

// Emoji with a search by Polish or English words, recent ones first.
const props = defineProps<{ pick: (e: string) => void }>();
const q = ref("");
const recent = ref(load<string[]>("szept.emoji", []));
const hits = computed(() => {
  const s = q.value.trim().toLowerCase();
  return s ? EMOJI.filter(([, w]) => w.includes(s)) : EMOJI;
});

function choose(e: string) {
  recent.value = [e, ...recent.value.filter((x) => x !== e)].slice(0, 14);
  save("szept.emoji", recent.value);
  props.pick(e);
  closePopover();
}
const first = () => hits.value[0] && choose(hits.value[0][0]);
</script>

<template>
  <div class="emoji">
    <div class="search emoji-q"><input v-model="q" v-focus placeholder="Szukaj emoji" autocomplete="off" @keydown.enter="first"><Icon name="search" /></div>
    <div class="emoji-body">
      <template v-if="!q.trim() && recent.length">
        <h4>Ostatnie</h4>
        <div class="emoji-grid"><button v-for="e in recent" :key="e" @click="choose(e)">{{ e }}</button></div>
        <h4>Wszystkie</h4>
      </template>
      <div v-if="hits.length" class="emoji-grid">
        <button v-for="[e, w] in hits" :key="e" :title="w.split(' ')[0]" @click="choose(e)">{{ e }}</button>
      </div>
      <p v-else class="emoji-none">Brak emoji dla „{{ q.trim() }}”</p>
    </div>
  </div>
</template>
