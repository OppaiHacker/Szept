<script setup lang="ts">
import { computed, ref } from "vue";
import { leaveChat, refresh } from "@/store/actions";
import { addFriend, myCodeDialog } from "@/store/dialogs";
import { copy } from "@/store/messages";
import { S } from "@/store/state";
import Icon from "@/components/base/Icon.vue";
import KeyBlock from "@/components/base/KeyBlock.vue";
import FriendRow from "./FriendRow.vue";

// Home without a chat: your friends, and the way to add one.
const me = computed(() => S.snap!.me);
const shown = computed(() => (S.ftab === "online" ? S.snap!.friends.filter((f) => f.online) : S.snap!.friends));
const code = ref("");
const codeEl = ref<HTMLInputElement>();

async function add() {
  if (!code.value.trim()) return codeEl.value?.focus();
  if (await addFriend(code.value.trim())) { code.value = ""; S.ftab = "all"; refresh(); }
}
</script>

<template>
  <header class="conv-head">
    <button class="icon-btn back" title="Wróć" @click="leaveChat({ fpage: false })"><Icon name="chev" /></button>
    <span class="h-ico"><Icon name="people" /></span><b class="h-t">Znajomi</b><i class="h-div" />
    <nav class="tabs">
      <button :class="{ sel: S.ftab === 'online' }" @click="S.ftab = 'online'">Online</button>
      <button :class="{ sel: S.ftab === 'all' }" @click="S.ftab = 'all'">Wszyscy</button>
      <button class="add" :class="{ sel: S.ftab === 'add' }" @click="S.ftab = 'add'">Dodaj<span class="long"> znajomego</span></button>
    </nav>
  </header>
  <div v-appear="S.ftab" class="fr-body scroll">
    <template v-if="S.ftab === 'add'">
      <h3>Dodaj znajomego</h3>
      <p class="lead">Wklej kod SZF-…, który dostaniesz od drugiej osoby. U niej to „Twój kod”.</p>
      <div class="add-line">
        <input ref="codeEl" v-model="code" v-focus class="input" placeholder="SZF-…" autocomplete="off" @keydown.enter="add">
        <button class="btn primary" @click="add">Dodaj</button>
      </div>
      <h3>Twój kod</h3>
      <div class="code-box">{{ me.code }}</div>
      <KeyBlock :pub="me.pub" />
      <div class="btn-row">
        <button class="btn" @click="copy(me.code)"><Icon name="copy" />Kopiuj</button>
        <button class="btn ghost" @click="myCodeDialog()"><Icon name="qr" />Pokaż QR</button>
      </div>
    </template>
    <template v-else-if="shown.length">
      <div class="fr-count">{{ S.ftab === "online" ? "Online" : "Wszyscy znajomi" }} · {{ shown.length }}</div>
      <FriendRow v-for="f in shown" :key="f.pub" :f="f" />
    </template>
    <div v-else class="empty"><i class="empty-ico"><Icon name="people" /></i>
      <strong>{{ S.ftab === "online" ? "Nikt nie jest teraz online" : "Nie masz jeszcze znajomych" }}</strong>
      <button class="btn primary" @click="S.ftab = 'add'">Dodaj znajomego</button>
    </div>
  </div>
</template>
