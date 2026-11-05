<script setup lang="ts">
import type { Friend } from "@/api/types";
import { keyShort } from "@/lib/key";
import { profilePop } from "@/store/dialogs";
import { dmWith } from "@/store/people";
import Icon from "@/components/base/Icon.vue";
import PresenceAvatar from "@/components/base/PresenceAvatar.vue";

defineProps<{ f: Friend }>();
const el = (e: Event) => e.currentTarget as Element;
</script>

<template>
  <div class="friend" @click="profilePop(f.pub, el($event))">
    <PresenceAvatar :url="f.avatar" :name="f.name" :seed="f.pub" :online="f.online" />
    <div class="row-body">
      <div class="row-name">{{ f.name }}<template v-if="f.alias"> <span class="alias">„{{ f.alias }}”</span></template> <span class="key-chip">{{ keyShort(f.pub) }}</span></div>
      <div class="row-sub">{{ f.status || (f.online ? "online" : "offline") }}</div>
    </div>
    <button class="round-btn" title="Napisz" @click.stop="dmWith(f.pub)"><Icon name="chat" /></button>
    <button class="round-btn" title="Profil" @click.stop="profilePop(f.pub, el($event))"><Icon name="user" /></button>
  </div>
</template>
