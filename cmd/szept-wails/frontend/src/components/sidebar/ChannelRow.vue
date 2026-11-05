<script setup lang="ts">
import { computed } from "vue";
import type { Channel, Chat } from "@/api/types";
import { count } from "@/lib/format";
import { openChat } from "@/store/actions";
import { joinVoice } from "@/store/calls";
import { myFlags } from "@/store/getters";
import { S } from "@/store/state";
import Icon from "@/components/base/Icon.vue";
import { drag } from "./drag";
import VoiceUser from "./VoiceUser.vue";

// A channel of group g: text opens its messages, voice joins its call and
// lists who is in it. Archived voice channels are not joinable, archived text
// ones stay readable.
const props = defineProps<{ g: Chat; x: Channel }>();

const key = computed(() => props.g.id + props.x.id);
const unread = computed(() => props.g.chanUnread?.[props.x.id] || 0);

// people in the voice channel, you first while you are in it
const live = computed(() => {
  const me = S.snap!.me.name;
  let names = props.g.voice[props.x.id] || [], flags = props.g.vflags?.[props.x.id] || [];
  if (props.g.callKey === key.value && !names.includes(me)) {
    names = [me, ...names];
    flags = [myFlags(props.g.call), ...flags];
  }
  // the call list has names only; the open chat's member list adds faces
  const members = S.conv?.id === props.g.id ? S.conv.members : [];
  return names.map((n, i) => ({
    name: n, flags: flags[i],
    m: members.find((x) => x.name === n) || (n === me ? S.snap!.me : null),
  }));
});

const draggable = computed(() => props.g.isOwner && !props.x.arch);
function start(e: DragEvent) {
  drag.ch = props.x;
  e.dataTransfer!.effectAllowed = "move";
  e.dataTransfer!.setData("text/plain", props.x.name);
}
const end = () => { drag.ch = null; drag.over = null; };
</script>

<template>
  <button v-if="x.kind === 'text'" class="row chan" :data-ch="x.id" :data-kind="x.kind"
    :class="{ active: S.chat === g.id && S.ch === x.id && !S.voice, unread }" :draggable="draggable || undefined"
    @click="openChat(g.id, x.id)" @dragstart="start" @dragend="end">
    <Icon name="hash" /><span class="row-name">{{ x.name }}</span>
    <span v-if="unread" class="badge">{{ count(unread) }}</span>
  </button>
  <button v-else-if="x.arch" class="row chan" :data-ch="x.id" :data-kind="x.kind"><Icon name="volume" /><span class="row-name">{{ x.name }}</span></button>
  <template v-else>
    <button class="row chan" :data-ch="x.id" :data-kind="x.kind" :class="{ joined: g.callKey === key, active: S.voice === key }" :draggable="draggable || undefined"
      @click="joinVoice(g.id, x.id)" @dragstart="start" @dragend="end">
      <Icon name="volume" /><span class="row-name">{{ x.name }}</span>
    </button>
    <div v-if="live.length" class="vc-list">
      <VoiceUser v-for="u in live" :key="u.name" :name="u.name" :flags="u.flags" :m="u.m" />
    </div>
  </template>
</template>
