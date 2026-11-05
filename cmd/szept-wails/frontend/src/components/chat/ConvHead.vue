<script setup lang="ts">
import { computed, nextTick, watchEffect } from "vue";
import type { Conversation } from "@/api/types";
import { narrow } from "@/lib/env";
import { people } from "@/lib/format";
import { leaveChat } from "@/store/actions";
import { joinVoice } from "@/store/calls";
import { chanName, chatById } from "@/store/getters";
import { inviteDialog, profilePop } from "@/store/dialogs";
import { chatMenu, pinsPop } from "@/store/menus";
import { warnRelayed } from "@/store/notify";
import { S } from "@/store/state";
import Icon from "@/components/base/Icon.vue";
import PresenceAvatar from "@/components/base/PresenceAvatar.vue";
import HeadFacts from "./HeadFacts.vue";

// The chat header: who or which channel, link facts, and the chat's tools.
const props = defineProps<{ conv: Conversation }>();

const relayed = computed(() => props.conv.members.filter((m) => !m.me && m.relay));
watchEffect(() => warnRelayed(relayed.value));
const peer = computed(() => props.conv.members.find((m) => !m.me));

const toggleMembers = () => {
  if (narrow.value) S.mMembers = !S.mMembers; else S.showMembers = !S.showMembers;
};
const openSearch = () => { S.hsOpen = true; nextTick(() => document.getElementById("hs")?.focus()); };
const el = (e: Event) => e.currentTarget as Element;
</script>

<template>
  <div class="head-main">
    <button class="icon-btn back" title="Wróć" @click="leaveChat({ fpage: false })"><Icon name="chev" /></button>
    <template v-if="conv.direct">
      <!-- a direct chat is about the person -->
      <button class="head-id" title="Profil" @click="profilePop(conv.peer, el($event))">
        <PresenceAvatar :url="conv.avatar" :name="conv.name" :seed="conv.peer" :online="peer?.online" /><span class="t">{{ conv.name }}</span>
      </button>
      <span class="s">{{ peer?.online ? "online" : "offline" }}<HeadFacts :relayed="relayed" :hidden="conv.hidden" /></span>
    </template>
    <template v-else>
      <!-- a group header names the channel -->
      <span class="h-ico"><Icon name="hash" /></span><b class="t">{{ chanName(chatById(conv.id), S.ch) }}</b><i class="h-div" />
      <span class="s">{{ conv.name }} · {{ people(conv.members.length) }}, {{ conv.online }} online<HeadFacts :relayed="relayed" :hidden="conv.hidden" /></span>
    </template>
    <span class="spacer" />
    <button v-if="!conv.call.joined" class="icon-btn" title="Zadzwoń" @click="joinVoice(conv.id, '')"><Icon name="phone" /></button>
    <button class="icon-btn hs-btn" title="Szukaj" @click="openSearch"><Icon name="search" /></button>
    <button class="icon-btn" title="Przypięte wiadomości" @click="pinsPop(el($event))"><Icon name="pin" /></button>
    <template v-if="!conv.direct">
      <button class="icon-btn" title="Zaproś" @click="inviteDialog()"><Icon name="key" /></button>
      <button class="icon-btn" :class="{ on: S.showMembers }" title="Lista członków" @click="toggleMembers"><Icon name="people" /></button>
    </template>
    <button class="icon-btn" title="Więcej" @click="chatMenu(el($event))"><Icon name="gear" /></button>
  </div>
</template>
