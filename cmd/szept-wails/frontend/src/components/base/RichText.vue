<script setup lang="ts">
import { computed } from "vue";
import { pieces } from "@/lib/text";
import { S } from "@/store/state";

// Message text with clickable links and highlighted @mentions. Links open
// outside the app: the webview never navigates away from it.
const props = defineProps<{ text: string }>();
const parts = computed(() => pieces(props.text, (S.conv?.members || []).map((m) => m.name)));
const open = (url: string) => window.open(url, "_blank");
</script>

<template><template v-for="(p, i) in parts" :key="i"><a v-if="p.t === 'link'" :href="p.v" @click.prevent="open(p.v)">{{ p.v }}</a><span v-else-if="p.t === 'ping'" class="ping">{{ p.v }}</span><template v-else>{{ p.v }}</template></template></template>
