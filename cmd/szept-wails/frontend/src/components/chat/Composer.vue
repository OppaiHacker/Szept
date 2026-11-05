<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from "vue";
import type { Conversation } from "@/api/types";
import { api } from "@/api/wails";
import { autoFocus } from "@/lib/env";
import { attachFiles, runCommand } from "@/store/commands";
import { emojiPicker, gifPicker } from "@/store/menus";
import { send, setReply } from "@/store/messages";
import { S } from "@/store/state";
import Icon from "@/components/base/Icon.vue";
import StagedFiles from "./StagedFiles.vue";
import { applyHint, matchCommands, matchMentions, type Hint } from "./hints";
import { atBottom, jumpDown } from "./scroll";

const props = defineProps<{ conv: Conversation }>();
const input = ref<HTMLTextAreaElement>();
const text = ref("");
const hints = ref<Hint[]>([]);
const sel = ref(0);
const canSend = computed(() => text.value.trim() !== "" || S.files.length > 0);

// field-sizing:content is not in the WebKitGTK several distros ship, and a
// composer that refuses to grow past one line is worse than a few lines of JS.
function grow() {
  const el = input.value!;
  el.style.height = "auto";
  el.style.height = Math.min(el.scrollHeight, 168) + "px";
}

function suggest() {
  const el = input.value!;
  hints.value = matchCommands(text.value.trim());
  if (!hints.value.length) hints.value = matchMentions(text.value, el.selectionStart, props.conv);
  sel.value = Math.min(sel.value, Math.max(0, hints.value.length - 1));
}

let typingTimer = 0;
const typing = (on: boolean) => api("Typing", S.chat, on).catch(() => {});
function onInput() {
  nextTick(grow);
  suggest();
  if (text.value.startsWith("/")) return;
  typing(true);
  clearTimeout(typingTimer);
  typingTimer = window.setTimeout(() => typing(false), 2500);
}

async function pick(h: Hint) {
  const [v, caret] = applyHint(text.value, h);
  text.value = v;
  hints.value = [];
  await nextTick();
  grow();
  input.value!.focus();
  input.value!.setSelectionRange(caret, caret);
}

function onKey(e: KeyboardEvent) {
  const hs = hints.value;
  if (hs.length) {
    if (e.key === "ArrowDown") { e.preventDefault(); sel.value = (sel.value + 1) % hs.length; return; }
    if (e.key === "ArrowUp") { e.preventDefault(); sel.value = (sel.value - 1 + hs.length) % hs.length; return; }
    // Tab always completes; Enter only completes when there is something
    // left to complete, otherwise it runs what you already typed.
    const h = hs[sel.value];
    const done = !h.at && text.value.trim().toLowerCase() === h.n;
    if (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey && !done)) { e.preventDefault(); pick(h); return; }
    if (e.key === "Escape") { hints.value = []; return; }
  }
  if (e.key === "Escape" && S.reply) { e.stopPropagation(); return setReply(null); }
  if (e.key === "Escape" && S.files.length) { e.stopPropagation(); S.files = []; return; }
  if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); submit(); }
}

async function submit() {
  const t = text.value.trim();
  if (!t && !S.files.length) return;
  text.value = "";
  hints.value = [];
  nextTick(grow);
  if (t.startsWith("/")) return runCommand(t);
  clearTimeout(typingTimer);
  if (!(await send(t))) text.value = t;
}

function onPaste(e: ClipboardEvent) {
  const files = [...(e.clipboardData?.files || [])];
  if (!files.length) return;
  e.preventDefault();
  S.files = [...S.files, ...files];
}

function emoji(e: Event) {
  emojiPicker(e.currentTarget as Element, (em) => {
    const el = input.value!;
    const at = el.selectionStart ?? text.value.length;
    text.value = text.value.slice(0, at) + em + text.value.slice(el.selectionEnd ?? at);
    nextTick(() => { el.focus(); el.setSelectionRange(at + em.length, at + em.length); onInput(); });
  });
}

onMounted(() => autoFocus(input.value));
// a reply or staged files bring the focus back to the text
watch(() => S.reply, (r) => { if (r) input.value?.focus(); });
</script>

<template>
  <div class="composer-wrap">
    <button class="jump" :class="{ show: !atBottom }" @click="jumpDown">Przeglądasz starsze wiadomości<b>Wróć na dół <Icon name="down" /></b></button>
    <div class="hints" :hidden="!hints.length">
      <button v-for="(h, i) in hints" :key="h.n" class="hint" :class="{ sel: i === sel }" @click="pick(h)">
        <b>{{ h.n }}</b><i>{{ h.a || "" }}</i><span>{{ h.d }}</span>
      </button>
    </div>
    <StagedFiles />
    <div v-if="S.reply" class="replying">
      <Icon name="reply" /><span>Odpowiadasz <b>{{ S.reply.name }}</b></span>
      <button class="icon-btn" title="Anuluj" @click="setReply(null)"><Icon name="close" /></button>
    </div>
    <div class="composer">
      <button class="add-btn" title="Załącz plik" @click="attachFiles()"><Icon name="plus" /></button>
      <textarea id="input" ref="input" v-model="text" rows="1" placeholder="Napisz wiadomość albo /komendę…"
        @input="onInput" @keydown="onKey" @paste="onPaste" />
      <button class="icon-btn gif-btn" title="Zapisane GIF-y" @click="gifPicker($event.currentTarget as Element)">GIF</button>
      <button class="icon-btn" title="Emoji" @click="emoji"><Icon name="smile" /></button>
      <button class="send" :disabled="!canSend" @click="submit"><Icon name="send" /></button>
    </div>
    <div class="drop-hint">Upuść plik, żeby załączyć</div>
  </div>
</template>
