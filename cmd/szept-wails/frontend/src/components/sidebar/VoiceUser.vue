<script setup lang="ts">
import type { Member } from "@/api/types";
import { peerVol } from "@/store/state";
import { userMenu } from "@/store/menus";
import { pointAnchor } from "@/ui/popover";
import Avatar from "@/components/base/Avatar.vue";
import Speak from "@/components/base/Speak.vue";
import CallFlags from "@/components/call/CallFlags.vue";

// One person under a voice channel. The call list has names only; m is their
// member entry when the open chat knows them.
const props = defineProps<{ name: string; flags?: string; m?: Member | { pub: string; avatar: string } | null }>();
const menu = (e: MouseEvent) => props.m && userMenu(props.m.pub, pointAnchor(e));
</script>

<template>
  <button class="vc-user" :class="{ muted: m && peerVol.get(m.pub)?.mute }" :disabled="!m"
    @click="m && userMenu(m.pub, $event.currentTarget as Element)" @contextmenu.prevent="menu">
    <Speak :pub="m?.pub"><Avatar :url="m?.avatar" :name="name" :seed="m?.pub || name" size="xs" /></Speak>
    <span class="vn">{{ name }}</span><CallFlags :flags="flags" />
  </button>
</template>
