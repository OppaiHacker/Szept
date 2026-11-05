<script setup lang="ts">
import { computed } from "vue";
import { tint } from "@/lib/avatar";
import { myCodeDialog, pickPicture } from "@/store/dialogs";
import { copy } from "@/store/messages";
import { editBio, editNick, editStatus } from "@/store/profileEdits";
import { S } from "@/store/state";
import Avatar from "@/components/base/Avatar.vue";
import Icon from "@/components/base/Icon.vue";
import KeyBlock from "@/components/base/KeyBlock.vue";
import FxEditor from "./FxEditor.vue";

const me = computed(() => S.snap!.me);
</script>

<template>
  <h2>Profil</h2>
  <p class="lead">Wszystko tutaj jest podpisane twoim kluczem i trafia do każdego czatu, w którym jesteś.</p>
  <div class="block">
    <div class="card-banner own" title="Zmień baner" :style="{ '--tone': tint(me.pub), backgroundImage: me.banner ? `url('${me.banner}')` : undefined }"
      @click="pickPicture('banner')">
      <span class="icon-btn edit"><Icon name="edit" /></span>
    </div>
    <div class="card-id">
      <button @click="pickPicture('avatar')"><Avatar :url="me.avatar" :name="me.name" :seed="me.pub" size="xl" /></button>
      <div class="nm">{{ me.name }}</div>
      <div class="st">{{ me.status || "bez statusu" }}</div>
    </div>
    <div class="btn-row">
      <button class="btn" @click="editNick"><Icon name="edit" />Nazwa</button>
      <button class="btn" @click="editStatus"><Icon name="smile" />Status</button>
      <button class="btn" @click="editBio"><Icon name="edit" />Opis</button>
    </div>
  </div>
  <FxEditor />
  <div class="block">
    <h4>Twój kod</h4>
    <p class="hint-text">Wizytówka: klucz publiczny plus podpowiedź, przez który connector cię szukać.</p>
    <div class="code-box wide-pad">{{ me.code }}</div>
    <KeyBlock :pub="me.pub" />
    <div class="btn-row">
      <button class="btn" @click="copy(me.code)"><Icon name="copy" />Kopiuj</button>
      <button class="btn ghost" @click="myCodeDialog()"><Icon name="qr" />QR</button>
    </div>
  </div>
</template>
