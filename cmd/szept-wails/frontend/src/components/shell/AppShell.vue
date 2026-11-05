<script setup lang="ts">
import { computed, shallowRef, watchEffect } from "vue";
import type { Conversation } from "@/api/types";
import { narrow } from "@/lib/env";
import { curTab, goTab } from "@/store/phone";
import { S } from "@/store/state";
import Rail from "./Rail.vue";
import TabBar from "./TabBar.vue";
import SideList from "@/components/sidebar/SideList.vue";
import VoicePanel from "@/components/call/VoicePanel.vue";
import UserBar from "./UserBar.vue";
import ConvView from "@/components/chat/ConvView.vue";
import VoiceView from "@/components/call/VoiceView.vue";
import MembersPane from "@/components/chat/MembersPane.vue";
import SearchResults from "@/components/chat/SearchResults.vue";
import FriendsPage from "@/components/friends/FriendsPage.vue";
import ActiveNow from "@/components/friends/ActiveNow.vue";
import YouPage from "@/components/phone/YouPage.vue";
import Welcome from "@/components/chat/Welcome.vue";

// What the conversation pane shows. On a phone the chat you just left stays
// in place ("keep") so it can slide away instead of vanishing.
type Mode = "chat" | "voice" | "you" | "friends" | "welcome" | "keep";
const mode = computed<Mode>(() => {
  if (S.conv) return S.voice ? "voice" : "chat";
  if (narrow.value && S.tab === "you") return "you";
  const home = S.space === "home";
  if (narrow.value && !(home && S.fpage)) return "keep";
  return home ? "friends" : "welcome";
});

// the last chat on screen, for "keep"
const kept = shallowRef<{ mode: Mode; conv: Conversation | null }>({ mode: "welcome", conv: null });
watchEffect(() => { if (mode.value !== "keep") kept.value = { mode: mode.value, conv: S.conv }; });
const shown = computed(() => (mode.value === "keep" ? kept.value : { mode: mode.value, conv: S.conv }));

const shellClass = computed(() => {
  const phone = narrow.value, c = S.conv, m = mode.value;
  if (c) return {
    "in-chat": true,
    "with-members": !c.direct && (phone ? S.mMembers : S.showMembers) && !S.voice,
    searching: !!S.search,
    "voice-view": !!S.voice,
    "hs-open": S.hsOpen,
  };
  if (m === "you") return { "in-chat": true, page: true };
  const home = S.space === "home";
  return {
    page: phone && home && S.fpage,
    "in-chat": m !== "keep" && home && S.fpage,
    "with-members": m !== "keep" && home && !phone, // "Aktywne teraz" has no room on a phone
  };
});

const closeMembers = () => {
  if (narrow.value) S.mMembers = !S.mMembers; else S.showMembers = !S.showMembers;
};
</script>

<template>
  <div id="shell" class="shell" :class="shellClass" :data-tab="curTab()">
    <Rail />
    <div class="side"><SideList /></div>
    <div class="dock">
      <VoicePanel />
      <UserBar />
    </div>
    <section id="conv" class="conv">
      <ConvView v-if="shown.mode === 'chat' && shown.conv" :key="shown.conv.id + '/' + S.ch" :conv="shown.conv" />
      <VoiceView v-else-if="shown.mode === 'voice' && shown.conv" :conv="shown.conv" />
      <YouPage v-else-if="shown.mode === 'you'" />
      <FriendsPage v-else-if="shown.mode === 'friends'" />
      <Welcome v-else />
    </section>
    <aside id="members" class="members">
      <SearchResults v-if="S.search && shown.conv" />
      <MembersPane v-else-if="shown.mode === 'chat' && shown.conv && !shown.conv.direct" :conv="shown.conv" />
      <ActiveNow v-else-if="shown.mode === 'friends'" />
    </aside>
    <div class="m-scrim" @click="closeMembers" />
    <TabBar @go="goTab" />
  </div>
</template>
