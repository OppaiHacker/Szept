<script setup lang="ts">
import { computed } from "vue";
import type { Msg } from "@/api/types";
import { REACT_SET } from "@/lib/emoji";
import { hhmm } from "@/lib/format";
import { nameFx } from "@/lib/profileStyle";
import { pingsMe } from "@/lib/text";
import { jumpTo } from "@/store/actions";
import { profilePop } from "@/store/dialogs";
import { styleOf } from "@/store/getters";
import { emojiPicker, msgMenu } from "@/store/menus";
import { hide, react, remove, setReply } from "@/store/messages";
import { S } from "@/store/state";
import { pointAnchor } from "@/ui/popover";
import Avatar from "@/components/base/Avatar.vue";
import Icon from "@/components/base/Icon.vue";
import RichText from "@/components/base/RichText.vue";
import Attachment from "./Attachment.vue";
import Reactions from "./Reactions.vue";

// One message. A log, not a chat of bubbles: one header per block of messages
// from the same person; follow-ups show their clock in the gutter on hover,
// and only own messages carry a delivery mark.
const props = defineProps<{ m: Msg; grouped: boolean; fresh: boolean }>();

const mention = computed(() => !props.m.mine && pingsMe(props.m.text, S.snap!.me.name));
const cls = computed(() => ({
  mine: props.m.mine, grouped: props.grouped, mention: mention.value, fresh: props.fresh,
  pending: props.m.mine && !props.m.ack && !props.m.deleted, deleted: props.m.deleted,
}));
const who = (e: Event) => profilePop(props.m.author, e.currentTarget as Element);
const el = (e: Event) => e.currentTarget as Element;

// a selection or a link keeps the native menu (copy, inspect in dev mode)
function ctx(e: MouseEvent) {
  if (getSelection()?.toString() || (e.target as Element).closest("a")) return;
  e.preventDefault();
  msgMenu(props.m.id, pointAnchor(e));
}
</script>

<template>
  <div v-if="m.kind === 'info'" class="sys"><RichText :text="m.text" /></div>
  <div v-else class="msg" :class="cls" :data-id="m.id" @contextmenu="ctx">
    <div class="gutter">
      <span v-if="grouped" class="gt">{{ hhmm(m.ts) }}</span>
      <button v-else @click="who"><Avatar :url="m.avatar" :name="m.name" :seed="m.author" /></button>
    </div>
    <div class="msg-col">
      <button v-if="m.reply" class="quote" @click="jumpTo(m.reply)">
        <Icon name="reply" />
        <template v-if="m.replyName"><b>{{ m.replyName }}</b><span>{{ m.replyText || "załącznik" }}</span></template>
        <span v-else>starsza wiadomość</span>
      </button>
      <div v-if="!grouped" class="msg-who">
        <button class="n" @click="who"><span v-bind="nameFx(styleOf(m.author))">{{ m.name }}</span></button><span class="t">{{ hhmm(m.ts) }}</span>
      </div>
      <div class="bubble"><Attachment v-if="!m.deleted && m.kind === 'file'" :m="m" /><RichText v-if="m.text" :text="m.text" /><span v-if="m.mine && !m.deleted" class="meta"><Icon :name="m.ack === 'read' || m.ack === 'delivered' ? 'checks' : 'check'" :class="{ read: m.ack === 'read' }" /></span></div>
      <Reactions :m="m" />
    </div>
    <div v-if="!m.deleted" class="msg-tools">
      <button v-for="e in REACT_SET.slice(0, 3)" :key="e" @click="react(m.id, e)">{{ e }}</button>
      <button title="Dodaj reakcję" @click="emojiPicker(el($event), (e) => react(m.id, e))"><Icon name="smile" /></button>
      <button title="Odpowiedz" @click="setReply(m)"><Icon name="reply" /></button>
      <button title="Schowaj u mnie" @click="hide(m.id)"><Icon name="eye" /></button>
      <button v-if="m.mine" title="Usuń dla wszystkich" @click="remove(m.id)"><Icon name="trash" /></button>
    </div>
  </div>
</template>
