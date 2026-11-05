<script setup lang="ts">
import { computed } from "vue";
import { keyGroups } from "@/lib/key";
import Icon from "./Icon.vue";

// The public key, typed out like a carbon copy coming off the roller: each
// group fades in in turn, four groups a line.
const props = defineProps<{ pub: string }>();
const lines = computed(() => {
  const g = keyGroups(props.pub), out: { x: string; i: number }[][] = [];
  for (let i = 0; i < g.length; i += 4) out.push(g.slice(i, i + 4).map((x, j) => ({ x, i: i + j })));
  return out;
});
</script>

<template>
  <div v-if="lines.length" class="key-block" title="Porównaj z drugą osobą innym kanałem">
    <div class="key-head"><Icon name="key" /><span>Klucz</span></div>
    <code><template v-for="(line, li) in lines" :key="li"><template v-if="li">{{ "\n" }}</template><template v-for="(g, gi) in line" :key="g.i"><template v-if="gi">{{ " " }}</template><span :style="{ '--i': g.i }">{{ g.x }}</span></template></template></code>
  </div>
</template>
