<script setup lang="ts">
import { computed } from "vue";
import { count } from "@/lib/format";
import { parseStyle } from "@/lib/profileStyle";
import { leaveChat, openSpace } from "@/store/actions";
import { S } from "@/store/state";
import { joinDialog, newGroupDialog } from "@/store/dialogs";
import Icon from "@/components/base/Icon.vue";
import Logo from "@/components/base/Logo.vue";
import ServerIcon from "@/components/base/ServerIcon.vue";

// The rail: home (direct chats and friends), then every group as a server
// tile. The pill on the left edge says unread (short), hover (mid) or open (tall).
const groups = computed(() => S.snap!.chats.filter((c) => !c.direct));
const dmUnread = computed(() => S.snap!.chats.reduce((n, c) => n + (c.direct ? c.unread : 0), 0));
</script>

<template>
  <nav class="rail">
    <button class="rail-btn home" :class="{ active: S.space === 'home', unread: dmUnread }" data-tip="Wiadomości prywatne"
      @click="leaveChat({ space: 'home', fpage: false })">
      <Logo /><span v-if="dmUnread" class="pip">{{ count(dmUnread) }}</span>
    </button>
    <i class="rail-sep" />
    <button v-for="g in groups" :key="g.id" class="rail-btn"
      :class="{ active: S.space === g.id, unread: g.unread, decorated: parseStyle(g.style).d }" :data-tip="g.name"
      @click="openSpace(g.id)">
      <ServerIcon :avatar="g.avatar" :name="g.name" :style-str="g.style" />
      <span v-if="g.unread" class="pip">{{ count(g.unread) }}</span>
      <span v-if="g.inCall.length || g.call" class="live"><Icon name="volume" /></span>
    </button>
    <button class="rail-btn add" data-tip="Nowa grupa" @click="newGroupDialog()"><Icon name="plus" /></button>
    <button class="rail-btn add" data-tip="Wejdź z zaproszenia" @click="joinDialog()"><Icon name="key" /></button>
  </nav>
</template>
