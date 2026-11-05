<script setup lang="ts">
import { computed } from "vue";
import { hhmm } from "@/lib/format";
import { jumpTo } from "@/store/actions";
import { pin } from "@/store/messages";
import { S } from "@/store/state";
import { closePopover } from "@/ui/popover";
import Icon from "@/components/base/Icon.vue";

// The chat's pinned messages, newest first; a click jumps to one.
const pins = computed(() => (S.conv?.pinned || []).slice().reverse());
const go = (id: string) => { closePopover(); jumpTo(id); };
const unpin = (id: string) => { closePopover(); pin(id, true); };
</script>

<template>
  <div class="pins">
    <h3>Przypięte wiadomości</h3>
    <div v-for="m in pins" :key="m.id" class="pin-row">
      <button class="pin-go" @click="go(m.id)">
        <span class="pin-who"><b>{{ m.name }}</b><span class="t">{{ hhmm(m.ts) }}</span></span>
        <span class="pin-tx">{{ m.text || m.file || "" }}</span>
      </button>
      <button class="icon-btn" title="Odepnij" @click="unpin(m.id)"><Icon name="close" /></button>
    </div>
    <div v-if="!pins.length" class="quiet"><i class="empty-ico"><Icon name="pin" /></i><b>Nic nie przypięto</b>Kliknij wiadomość prawym przyciskiem i wybierz „Przypnij”.</div>
  </div>
</template>
