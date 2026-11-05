<script setup lang="ts">
import type { Member } from "@/api/types";
import { nameFx } from "@/lib/profileStyle";
import { profilePop } from "@/store/dialogs";
import { userMenu } from "@/store/menus";
import { peerVol } from "@/store/state";
import { pointAnchor } from "@/ui/popover";
import Icon from "@/components/base/Icon.vue";
import PresenceAvatar from "@/components/base/PresenceAvatar.vue";
import Speak from "@/components/base/Speak.vue";

// A person in a member list. Right click: the person menu.
defineProps<{ m: Member; sub?: string; fx?: boolean }>();
</script>

<template>
  <button class="member" :class="{ off: !m.online, 'in-call': m.inCall, muted: peerVol.get(m.pub)?.mute }"
    @click="profilePop(m.pub, $event.currentTarget as Element)" @contextmenu.prevent="userMenu(m.pub, pointAnchor($event))">
    <Speak :pub="m.pub"><PresenceAvatar :url="m.avatar" :name="m.name" :seed="m.pub" :online="m.online" size="s" /></Speak>
    <span class="nm"><b v-bind="fx ? nameFx(m.style) : {}">{{ m.name }}</b><span>{{ sub ?? (m.status || (m.owner ? "właściciel" : "")) }}</span></span>
    <span v-if="m.inCall" class="live"><Icon name="phone" /></span>
    <span v-if="m.owner" class="tag" title="właściciel"><Icon name="crown" /></span>
  </button>
</template>
