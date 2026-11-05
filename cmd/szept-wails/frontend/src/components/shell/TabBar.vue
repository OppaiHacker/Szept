<script setup lang="ts">
import { computed } from "vue";
import { count } from "@/lib/format";
import { curTab, type Tab } from "@/store/phone";
import { S } from "@/store/state";
import Icon from "@/components/base/Icon.vue";
import Logo from "@/components/base/Logo.vue";
import PresenceAvatar from "@/components/base/PresenceAvatar.vue";

// Phone: the four tabs at the bottom.
defineEmits<{ go: [t: Tab] }>();
const me = computed(() => S.snap!.me);
const dms = computed(() => S.snap!.chats.reduce((n, c) => n + (c.direct ? c.unread : 0), 0));
const groups = computed(() => S.snap!.chats.reduce((n, c) => n + (c.direct ? 0 : c.unread), 0));
const tabs = computed(() => [
  { id: "servers" as Tab, label: "Serwery", n: groups.value },
  { id: "dms" as Tab, label: "Wiadomości", n: dms.value },
  { id: "friends" as Tab, label: "Znajomi", n: 0 },
  { id: "you" as Tab, label: "Ty", n: 0 },
]);
</script>

<template>
  <nav class="tabbar">
    <button v-for="t in tabs" :key="t.id" class="tab" :class="{ on: curTab() === t.id }"
      :aria-current="curTab() === t.id ? 'page' : 'false'" @click="$emit('go', t.id)">
      <span class="tab-ico">
        <Logo v-if="t.id === 'servers'" />
        <Icon v-else-if="t.id === 'dms'" name="chat" />
        <Icon v-else-if="t.id === 'friends'" name="people" />
        <PresenceAvatar v-else :url="me.avatar" :name="me.name" :seed="me.pub" :online="me.connUp" size="xs" />
        <span v-if="t.n" class="pip">{{ count(t.n) }}</span>
      </span>
      <span class="tab-t">{{ t.label }}</span>
    </button>
  </nav>
</template>
