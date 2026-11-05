<script setup lang="ts">
import { computed } from "vue";
import { curGroup } from "@/store/getters";
import { S } from "@/store/state";
import GroupSide from "./GroupSide.vue";
import HomeSide from "./HomeSide.vue";
import NoServersSide from "./NoServersSide.vue";

const g = computed(() => (S.space !== "home" ? curGroup() : undefined));
</script>

<template>
  <section class="list">
    <div v-if="!S.snap" class="skel"><i v-for="n in 7" :key="n" /></div>
    <GroupSide v-else-if="g" :g="g" />
    <NoServersSide v-else-if="S.space === 'servers'" />
    <HomeSide v-else />
  </section>
</template>
