<script setup lang="ts">
import { computed } from "vue";
import { dateOf, hhmm, plural } from "@/lib/format";
import { jumpTo } from "@/store/actions";
import { closeSearch } from "@/store/phone";
import { S } from "@/store/state";
import Avatar from "@/components/base/Avatar.vue";
import Icon from "@/components/base/Icon.vue";

// Hits of a chat search, newest first; each jumps back to its place.
const r = computed(() => S.search!);

// the text cut into plain and matching pieces, for <mark>
function parts(text: string) {
  const q = r.value.q.toLowerCase(), out: { t: string; hit: boolean }[] = [];
  let at = 0;
  const low = text.toLowerCase();
  for (let i = low.indexOf(q); q && i >= 0; i = low.indexOf(q, i + q.length)) {
    if (i > at) out.push({ t: text.slice(at, i), hit: false });
    out.push({ t: text.slice(i, i + q.length), hit: true });
    at = i + q.length;
  }
  if (at < text.length) out.push({ t: text.slice(at), hit: false });
  return out;
}
</script>

<template>
  <div class="res-head">
    <b>{{ r.hits.length ? plural(r.hits.length, "wynik", "wyniki", "wyników") : "Brak wyników" }}</b>
    <button class="icon-btn" title="Zamknij" @click="closeSearch"><Icon name="x" /></button>
  </div>
  <div class="scroll">
    <div class="res-list">
      <button v-for="m in r.hits" :key="m.id" class="res" @click="jumpTo(m.id)">
        <div class="gutter"><Avatar :url="m.avatar" :name="m.name" :seed="m.author" /></div>
        <div class="msg-col">
          <div class="msg-who"><span class="n">{{ m.name }}</span><span class="t">{{ dateOf(m.ts) }} {{ hhmm(m.ts) }}</span></div>
          <div class="bubble"><template v-for="(p, i) in parts(m.text)" :key="i"><mark v-if="p.hit">{{ p.t }}</mark><template v-else>{{ p.t }}</template></template></div>
        </div>
        <span class="go">Przejdź</span>
      </button>
      <div v-if="!r.hits.length" class="empty"><i class="empty-ico"><Icon name="search" /></i><strong>Nic nie znalazłem</strong>Spróbuj innego słowa.</div>
    </div>
  </div>
</template>
