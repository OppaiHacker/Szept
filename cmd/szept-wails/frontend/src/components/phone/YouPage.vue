<script setup lang="ts">
import { computed } from "vue";
import { tint } from "@/lib/avatar";
import { nameFx } from "@/lib/profileStyle";
import { myCodeDialog, pickPicture } from "@/store/dialogs";
import { styleOf } from "@/store/getters";
import { editStatus } from "@/store/profileEdits";
import { openSettings } from "@/store/settings";
import { S } from "@/store/state";
import type { IconName } from "@/lib/icons";
import Icon from "@/components/base/Icon.vue";
import KeyBlock from "@/components/base/KeyBlock.vue";
import PresenceAvatar from "@/components/base/PresenceAvatar.vue";

// Phone, the Ty tab: who you are on this device and the way into everything else.
const me = computed(() => S.snap!.me);
const conn = computed(() => me.value.connErr ? "brak połączenia z connectorem"
  : me.value.connUp ? "połączony z " + me.value.connector : "łączenie z connectorem…");
const rows = computed<{ label: string; icon: IconName; sub?: string; section?: string; code?: true }[]>(() => [
  { label: "Twój kod", icon: "qr", sub: me.value.code, code: true },
  { label: "Urządzenia", icon: "device", sub: "połącz komputer albo drugi telefon", section: "devices" },
  { label: "Connector", icon: "server", sub: conn.value, section: "connector" },
  { label: "Głos", icon: "volume", sub: "mikrofon, głośnik, powiadomienia", section: "voice" },
  { label: "Wygląd", icon: "palette", sub: "motyw i kolor", section: "look" },
  { label: "Hasło i kopia", icon: "shield", section: "security" },
  { label: "Wszystkie ustawienia", icon: "gear" },
]);
const go = (r: (typeof rows.value)[number]) => (r.code ? myCodeDialog() : openSettings(r.section || "profile", !!r.section));
</script>

<template>
  <div v-appear class="you scroll">
    <div class="card-banner own" :style="{ '--tone': tint(me.pub), backgroundImage: me.banner ? `url('${me.banner}')` : undefined }"
      @click="pickPicture('banner')" />
    <div class="card-id">
      <button @click="pickPicture('avatar')"><PresenceAvatar :url="me.avatar" :name="me.name" :seed="me.pub" :online="me.connUp" size="xl" /></button>
      <div class="nm" v-bind="nameFx(styleOf(me.pub))">{{ me.name }}</div>
      <div class="st">{{ me.status || "bez statusu" }}</div>
      <div class="btn-row">
        <button class="btn" @click="editStatus"><Icon name="smile" />Ustaw status</button>
        <button class="btn ghost" @click="openSettings('profile', true)"><Icon name="edit" />Edytuj profil</button>
      </div>
    </div>
    <div class="you-sec"><KeyBlock :pub="me.pub" /></div>
    <div class="you-list">
      <button v-for="r in rows" :key="r.label" class="you-row" @click="go(r)">
        <Icon :name="r.icon" />
        <span class="nm"><b>{{ r.label }}</b><span v-if="r.sub">{{ r.sub }}</span></span>
        <Icon name="chev" class="go" />
      </button>
    </div>
  </div>
</template>
