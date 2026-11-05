<script setup lang="ts">
import type { Chat } from "@/api/types";
import { count } from "@/lib/format";
import { openChat } from "@/store/actions";
import { S } from "@/store/state";
import PresenceAvatar from "@/components/base/PresenceAvatar.vue";

defineProps<{ c: Chat }>();
</script>

<template>
  <button class="row" :class="{ active: c.id === S.chat, unread: c.unread }" :data-chat="c.id" @click="openChat(c.id)">
    <PresenceAvatar :url="c.avatar" :name="c.name" :seed="c.peer || c.id" :online="c.online > 0" />
    <div class="row-body">
      <div class="row-name">{{ c.name }}</div>
      <div class="row-sub">{{ c.inCall.length ? "w rozmowie" : c.preview || (c.online > 0 ? "online" : "offline") }}</div>
    </div>
    <span v-if="c.unread" class="badge">{{ count(c.unread) }}</span>
  </button>
</template>
