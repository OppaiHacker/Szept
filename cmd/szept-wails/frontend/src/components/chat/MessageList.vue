<script setup lang="ts">
import { computed, onBeforeUpdate, onMounted, onUpdated, ref, shallowRef, watch } from "vue";
import type { Conversation, Msg } from "@/api/types";
import { dayLabel } from "@/lib/format";
import { refreshConv } from "@/store/actions";
import { S } from "@/store/state";
import Icon from "@/components/base/Icon.vue";
import MessageItem from "./MessageItem.vue";
import { atBottom } from "./scroll";

// The log of one chat. It is mounted per chat, so "first" is the chat opening.
const props = defineProps<{ conv: Conversation }>();
const list = ref<HTMLElement>();

// Which messages already were on screen: only one that lands while you watch
// the bottom slides in; older pages pulled in by scrolling up stay still.
let seen = new Set<string>(), first = true;
const fresh = shallowRef(new Set<string>());
watch(() => props.conv.msgs, (msgs) => {
  const lastKnown = msgs.findLastIndex((m) => seen.has(m.id));
  const f = new Set<string>();
  if (!first && atBottom.value) msgs.forEach((m, i) => { if (i > lastKnown && !seen.has(m.id)) f.add(m.id); });
  fresh.value = f;
  seen = new Set(msgs.map((m) => m.id));
  first = false;
  // the first unread message is pinned by id once, so the "nowe" line stays
  // put while newer messages keep arriving under it
  const mark = S.newMark;
  if (mark?.chat === props.conv.id && !mark.id) mark.id = msgs[Math.max(0, msgs.length - mark.n)]?.id;
}, { immediate: true });

type Row = { k: string; day?: string; nl?: true; m?: Msg; grouped?: boolean };
const rows = computed(() => {
  const out: Row[] = [];
  const mark = S.newMark?.chat === props.conv.id ? S.newMark : null;
  let lastDay = "", prev: Msg | null = null;
  props.conv.msgs.forEach((m, i) => {
    const d = dayLabel(m.ts);
    if (d !== lastDay) { out.push({ k: "d" + m.id, day: d }); lastDay = d; prev = null; }
    if (mark && m.id === mark.id && i > 0) { out.push({ k: "new", nl: true }); prev = null; }
    const grouped = !!prev && prev.kind !== "info" && prev.author === m.author && m.ts - prev.ts < 300;
    out.push({ k: m.id, m, grouped });
    prev = m;
  });
  return out;
});

// Growing the list keeps what you read in place: at the bottom it follows new
// messages, higher up an older page landing above must not move the view.
let keep = true, prevTop = 0, prevHeight = 0, prevFirst = "";
onBeforeUpdate(() => {
  const l = list.value!;
  keep = l.scrollTop + l.clientHeight >= l.scrollHeight - 60 || !l.scrollHeight;
  prevTop = l.scrollTop; prevHeight = l.scrollHeight;
});
onUpdated(() => {
  const l = list.value!, firstId = props.conv.msgs[0]?.id || "";
  if (keep) l.scrollTop = l.scrollHeight;
  else if (firstId !== prevFirst) l.scrollTop = prevTop + (l.scrollHeight - prevHeight);
  prevFirst = firstId;
});
onMounted(() => {
  list.value!.scrollTop = list.value!.scrollHeight;
  prevFirst = props.conv.msgs[0]?.id || "";
  onScroll();
});

// reaching the top pulls another page of history
let loading = false;
function onScroll() {
  const l = list.value!;
  atBottom.value = l.scrollTop + l.clientHeight >= l.scrollHeight - 60;
  if (l.scrollTop < 40 && !loading && props.conv.msgs.length >= S.limit) {
    loading = true;
    S.limit += 60;
    refreshConv().finally(() => { loading = false; });
  }
}
</script>

<template>
  <div id="msgs" ref="list" class="msgs" @scroll.passive="onScroll">
    <template v-for="r in rows" :key="r.k">
      <div v-if="r.day" class="day">{{ r.day }}</div>
      <div v-else-if="r.nl" class="new-line"><span>nowe</span></div>
      <MessageItem v-else :m="r.m!" :grouped="r.grouped!" :fresh="fresh.has(r.k)" />
    </template>
    <div v-if="!rows.length" class="empty auto"><i class="empty-ico"><Icon name="chat" /></i><strong>Cisza</strong>Napisz pierwszą wiadomość.</div>
  </div>
</template>
