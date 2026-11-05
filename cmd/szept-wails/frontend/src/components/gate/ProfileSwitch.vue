<script setup lang="ts">
import { newProfile, useProfile } from "@/store/profiles";
import { S } from "@/store/state";
import Icon from "@/components/base/Icon.vue";
</script>

<template>
  <div v-if="S.boot && !S.boot.pinned" class="profile-switch">
    <div class="select-wrap">
      <select class="input" :value="S.boot.profile" @change="useProfile(($event.target as HTMLSelectElement).value)">
        <option v-for="p in S.boot.profiles" :key="p" :value="p">{{ p || "domyślne" }}</option>
      </select>
    </div>
    <button class="btn ghost" @click="newProfile"><Icon name="plus" />Nowe konto</button>
  </div>
</template>
