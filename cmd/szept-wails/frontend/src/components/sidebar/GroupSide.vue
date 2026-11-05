<script setup lang="ts">
import { computed } from "vue";
import type { Chat } from "@/api/types";
import { openChat } from "@/store/actions";
import { chanOf } from "@/store/getters";
import { channelMenu, chatMenu } from "@/store/menus";
import { S } from "@/store/state";
import { pointAnchor } from "@/ui/popover";
import Icon from "@/components/base/Icon.vue";
import ChannelSection from "./ChannelSection.vue";

// A group's channel list: the default text and voice sections, the owner's
// categories, and the owner's archive.
const props = defineProps<{ g: Chat }>();

const live = computed(() => props.g.channels.filter((x) => !x.arch));
const cats = computed(() => live.value.filter((x) => x.kind === "cat"));
const chans = computed(() => live.value.filter((x) => x.kind !== "cat"));
// a channel whose category was deleted falls back to the default section
const loose = (kind: string) => chans.value.filter((x) => x.kind === kind && (!x.cat || !cats.value.some((c) => c.id === x.cat)));
const sections = computed(() => [
  { id: "text", label: "Kanały tekstowe", kind: "text", rows: loose("text") },
  { id: "voice", label: "Głosowe", kind: "voice", rows: loose("voice") },
].filter((s) => s.rows.length || props.g.isOwner));
const arch = computed(() => (props.g.isOwner ? props.g.channels.filter((x) => x.arch && x.kind !== "cat") : []));

async function headMenu(e: MouseEvent) {
  const el = e.currentTarget as Element;
  if (S.chat !== S.space) await openChat(S.space);
  chatMenu(el);
}

// right click, owner only: the channel menu for the row or section under it
function ctx(e: MouseEvent) {
  if (!props.g.isOwner) return;
  e.preventDefault();
  const t = e.target as Element;
  const row = t.closest<HTMLElement>("[data-ch]"), box = t.closest<HTMLElement>(".cat");
  const x = row ? chanOf(props.g, row.dataset.ch!, row.dataset.kind as "text") : box?.dataset.cat ? chanOf(props.g, box.dataset.cat, "cat") : undefined;
  channelMenu(pointAnchor(e), x, box?.dataset.cat || "", box?.dataset.kind || "text");
}
</script>

<template>
  <button class="pane-head g-head" :class="{ 'has-banner': g.banner }" :style="g.banner ? { backgroundImage: `url('${g.banner}')` } : undefined"
    @click="headMenu"><h2>{{ g.name }}</h2><Icon name="chev" class="dd" /></button>
  <div v-appear="g.id" class="scroll" @contextmenu="ctx">
    <div class="rows">
      <ChannelSection v-for="s in sections" :key="s.id" :g="g" :id="s.id" :label="s.label" cat-id="" :kind="s.kind" :rows="s.rows" />
      <ChannelSection v-for="c in cats" :key="c.id" :g="g" :id="c.id" :label="c.name" :cat-id="c.id" kind="text"
        :rows="chans.filter((x) => x.cat === c.id)" />
      <ChannelSection v-if="arch.length" :g="g" id="arch" label="Archiwum" :cat-id="null" kind="" :rows="arch" />
    </div>
  </div>
</template>
