<script setup lang="ts">
import { computed, ref } from "vue";
import { REACT_SET } from "@/lib/emoji";
import { isSaved, saveGif, unsaveGif } from "@/lib/gifs";
import { emojiPicker } from "@/store/menus";
import { copy, hide, pin, react, remove, setReply } from "@/store/messages";
import { S } from "@/store/state";
import { closePopover } from "@/ui/popover";
import { toast } from "@/ui/toast";
import MenuItem from "./MenuItem.vue";

// A right click on a message: the hover tools plus what does not fit there.
const props = defineProps<{ id: string }>();
const m = computed(() => S.conv?.msgs.find((x) => x.id === props.id));
const quick = (e: string) => { closePopover(); react(props.id, e); };
// the picker opens where the menu was (it is still on screen when this runs)
const root = ref<HTMLElement>();
const more = () => {
  const r = root.value!.getBoundingClientRect();
  emojiPicker({ getBoundingClientRect: () => r, contains: () => false }, (e) => react(props.id, e));
};
const gifSave = () => { saveGif(m.value!.blob!, m.value!.file); toast("GIF zapisany", "ok"); };
</script>

<template>
  <div v-if="m" ref="root" class="menu">
    <template v-if="!m.deleted">
      <div class="menu-reacts"><button v-for="e in REACT_SET.slice(0, 4)" :key="e" @click="quick(e)">{{ e }}</button></div>
      <MenuItem label="Dodaj reakcję" icon="smile" :run="more" />
      <i class="menu-sep" />
      <MenuItem label="Odpowiedz" icon="reply" :run="() => setReply(m!)" />
      <MenuItem :label="m.pinned ? 'Odepnij wiadomość' : 'Przypnij wiadomość'" icon="pin" :run="() => pin(m!.id, !!m!.pinned)" />
      <MenuItem v-if="m.text" label="Kopiuj tekst" icon="copy" :run="() => copy(m!.text)" />
      <template v-if="m.mime === 'image/gif' && m.have">
        <MenuItem v-if="isSaved(m.blob)" label="Usuń z zapisanych GIF-ów" icon="trash" :run="() => unsaveGif(m!.blob!)" />
        <MenuItem v-else label="Zapisz GIF" icon="star" :run="gifSave" />
      </template>
      <i class="menu-sep" />
    </template>
    <MenuItem label="Schowaj u mnie" icon="eye" :run="() => hide(m!.id)" />
    <MenuItem v-if="!m.deleted && (m.mine || S.conv?.isOwner)" label="Usuń wiadomość" icon="trash" danger :run="() => remove(m!.id)" />
    <template v-if="S.snap?.me.dev">
      <i class="menu-sep" />
      <MenuItem label="Kopiuj ID wiadomości" icon="hash" :run="() => copy(m!.id)" />
    </template>
  </div>
</template>
