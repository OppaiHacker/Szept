<script setup lang="ts">
import type { Channel, Chat } from "@/api/types";
import { channelDialog } from "@/store/dialogs";
import { S } from "@/store/state";
import Icon from "@/components/base/Icon.vue";
import ChannelRow from "./ChannelRow.vue";
import { drag, dropOn, takes } from "./drag";

// A foldable section of the channel list. catId: a category's id, "" for the
// default section of `kind`, null for the archive (no drop, no add).
const props = defineProps<{ g: Chat; id: string; label: string; catId: string | null; kind: string; rows: Channel[] }>();

const toggle = () => (S.collapsed.has(props.id) ? S.collapsed.delete(props.id) : S.collapsed.add(props.id));
function over(e: DragEvent) {
  if (!drag.ch) return;
  const ok = props.catId !== null && takes(props.catId, props.kind);
  drag.over = ok ? props.id : null;
  e.dataTransfer!.dropEffect = ok ? "move" : "none";
}
const drop = () => { if (props.catId !== null && drag.ch) dropOn(props.catId, props.kind); };
</script>

<template>
  <div class="cat" :class="{ shut: S.collapsed.has(id), 'drop-to': drag.over === id }" :data-cat="catId ?? undefined" :data-kind="kind"
    @dragover="over" @drop="drop">
    <div class="cat-top">
      <button class="cat-head" @click="toggle"><Icon name="chev" /><span>{{ label }}</span></button>
      <button v-if="g.isOwner && catId !== null" class="cat-add" title="Utwórz kanał"
        @click="channelDialog(catId, kind === 'voice' ? 'voice' : 'text')"><Icon name="plus" /></button>
    </div>
    <div class="cat-body"><div><ChannelRow v-for="x in rows" :key="x.kind + x.id" :g="g" :x="x" /></div></div>
  </div>
</template>
