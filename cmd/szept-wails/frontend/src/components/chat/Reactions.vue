<script setup lang="ts">
import { computed } from "vue";
import type { Msg } from "@/api/types";
import { react } from "@/store/messages";
import { S } from "@/store/state";

// Reaction chips under a message: one per emoji, with a count past one.
const props = defineProps<{ m: Msg }>();
const chips = computed(() => {
  const by = new Map<string, string[]>();
  for (const [pub, emoji] of Object.entries(props.m.reactions || {})) by.set(emoji, [...(by.get(emoji) || []), pub]);
  return [...by].map(([emoji, pubs]) => ({ emoji, n: pubs.length, mine: pubs.includes(S.snap!.me.pub) }));
});
</script>

<template>
  <div v-if="chips.length" class="reactions">
    <button v-for="c in chips" :key="c.emoji" class="reaction" :class="{ mine: c.mine }" @click="react(m.id, c.emoji)">
      {{ c.emoji }}<span v-if="c.n > 1" class="n">{{ c.n }}</span>
    </button>
  </div>
</template>
