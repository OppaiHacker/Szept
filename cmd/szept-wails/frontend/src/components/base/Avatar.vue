<script setup lang="ts">
import { computed } from "vue";
import { initials, tint } from "@/lib/avatar";
import { decoVars, parseStyle } from "@/lib/profileStyle";
import { styleOf } from "@/store/getters";
import Deco from "./Deco.vue";

// A face: the picture, or initials on a tint picked from the seed. A profile
// decoration wraps it unless the avatar is extra small. style overrides what
// the snapshot knows (previews in the effects editor).
const props = defineProps<{ url?: string; name?: string; seed?: string; size?: "" | "xs" | "s" | "m" | "xl"; styleStr?: string }>();

const st = computed(() => parseStyle(props.styleStr ?? styleOf(props.seed)));
const decorated = computed(() => !!st.value.d && props.size !== "xs");
</script>

<template>
  <span v-if="decorated" class="avd" :style="decoVars(st)">
    <div class="avatar" :class="size" :style="url ? undefined : { background: tint(seed || name || '') }">
      <img v-if="url" :src="url" alt=""><template v-else>{{ initials(name || "") }}</template>
    </div>
    <span class="deco" aria-hidden="true"><Deco :d="st.d" /></span>
  </span>
  <div v-else class="avatar" :class="size" :style="url ? undefined : { background: tint(seed || name || '') }">
    <img v-if="url" :src="url" alt=""><template v-else>{{ initials(name || "") }}</template>
  </div>
</template>
