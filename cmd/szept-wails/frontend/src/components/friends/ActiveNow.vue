<script setup lang="ts">
import { computed } from "vue";
import { people } from "@/lib/format";
import { openChat } from "@/store/actions";
import { S } from "@/store/state";
import Icon from "@/components/base/Icon.vue";

// "Aktywne teraz": calls going on right now, next to the friends page.
const live = computed(() => S.snap!.chats.filter((c) => c.inCall.length));
</script>

<template>
  <div class="scroll active-now">
    <h3>Aktywne teraz</h3>
    <button v-for="c in live" :key="c.id" class="live-card" @click="openChat(c.id)">
      <b>{{ c.name }}</b><span>{{ c.inCall.join(", ") }}</span>
      <span class="vc-tag"><Icon name="volume" />{{ people(c.inCall.length) }} w rozmowie</span>
    </button>
    <div v-if="!live.length" class="quiet"><i class="empty-ico"><Icon name="volume" /></i><b>Na razie cisza</b>Gdy ktoś zacznie rozmowę głosową, zobaczysz ją tutaj.</div>
  </div>
</template>
