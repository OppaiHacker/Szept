<script setup lang="ts">
import { computed } from "vue";
import { leaveChat } from "@/store/actions";
import { addFriendDialog } from "@/store/dialogs";
import { S } from "@/store/state";
import Icon from "@/components/base/Icon.vue";
import ChatRow from "./ChatRow.vue";

// Home: the friends page and every direct chat, newest first (Go sorts).
const q = computed(() => S.filter.toLowerCase());
const dms = computed(() => S.snap!.chats.filter((c) => c.direct && (!q.value || c.name.toLowerCase().includes(q.value))));
const online = computed(() => S.snap!.friends.filter((f) => f.online).length);
</script>

<template>
  <div class="pane-head">
    <div class="search"><Icon name="search" /><input id="q" v-model="S.filter" placeholder="Znajdź rozmowę"></div>
  </div>
  <div v-appear="'home'" class="scroll">
    <!-- a chat that jumped up the list slides there instead of teleporting -->
    <TransitionGroup tag="div" class="rows" move-class="row-move">
      <button key="friends" class="row nav" :class="{ active: !S.chat }" @click="leaveChat({ space: 'home', fpage: true })">
        <span class="nav-ico"><Icon name="people" /></span><span class="row-name">Znajomi</span>
        <span v-if="online" class="badge soft">{{ online }}</span>
      </button>
      <div key="head" class="cat-head"><span>Wiadomości prywatne</span>
        <button class="icon-btn" title="Dodaj znajomego" @click="addFriendDialog()"><Icon name="plus" /></button></div>
      <ChatRow v-for="c in dms" :key="c.id" :c="c" />
      <p v-if="!dms.length" key="empty" class="side-empty">{{ q ? "Nic nie pasuje." : "Napisz do znajomego, a rozmowa pojawi się tutaj." }}</p>
    </TransitionGroup>
  </div>
</template>
