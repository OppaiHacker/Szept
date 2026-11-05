<script setup lang="ts">
import { computed } from "vue";
import { initials } from "@/lib/avatar";
import { decoVars, parseStyle } from "@/lib/profileStyle";
import Deco from "./Deco.vue";

// A group's icon: its picture or initials, under the same sprites an avatar
// decoration uses.
const props = defineProps<{ avatar?: string; name: string; styleStr?: string }>();
const st = computed(() => parseStyle(props.styleStr));
</script>

<template>
  <img v-if="avatar" :src="avatar" alt=""><span v-else class="ini">{{ initials(name) }}</span>
  <span v-if="st.d" class="deco" aria-hidden="true" :style="decoVars(st)"><Deco :d="st.d" /></span>
</template>
