<script setup lang="ts">
import { computed, ref } from "vue";
import { closeSettings, goSection } from "@/store/settings";
import { S } from "@/store/state";
import Avatar from "@/components/base/Avatar.vue";
import Icon from "@/components/base/Icon.vue";
import { PHONE_OFF, SECTIONS } from "./sections";

// The left column: you, a search box, the sections in their groups. A group
// header shows only while one of its sections matches the search.
const q = ref("");
const me = computed(() => S.snap!.me);
const items = computed(() => {
  const s = q.value.toLowerCase();
  const hit = (x: (typeof SECTIONS)[number]) => "id" in x && (!s || (x.t + " " + x.d).toLowerCase().includes(s)) && !(S.boot?.mobile && PHONE_OFF.has(x.id));
  return SECTIONS.filter((x, i) => {
    if (!("g" in x)) return hit(x);
    const next = SECTIONS.slice(i + 1).find((y) => "g" in y || hit(y));
    return !!next && !("g" in next);
  });
});
</script>

<template>
  <nav class="set-nav">
    <button class="icon-btn set-nav-close" title="Zamknij" @click="closeSettings"><Icon name="x" /></button>
    <button class="set-me" @click="goSection('profile')">
      <Avatar :url="me.avatar" :name="me.name" :seed="me.pub" />
      <span><b>{{ me.name }}</b><span>Edytuj profil</span></span>
    </button>
    <div class="search"><Icon name="search" /><input v-model="q" placeholder="Szukaj"></div>
    <div>
      <template v-for="(x, i) in items" :key="i">
        <template v-if="'g' in x">
          <div v-if="x.g" class="set-group">{{ x.g }}</div>
          <div v-else class="set-sep" />
        </template>
        <button v-else class="set-row" :class="{ active: x.id === S.section }" @click="goSection(x.id)">
          <Icon :name="x.i" /><span>{{ x.t }}</span>
        </button>
      </template>
    </div>
  </nav>
</template>
