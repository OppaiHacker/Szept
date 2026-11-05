<script setup lang="ts">
import { computed } from "vue";
import { S } from "@/store/state";

// Android has no title bar: a strip over the top says the same while the
// connector is not carrying you.
const me = computed(() => S.snap?.me);
const state = computed(() => (me.value?.connErr ? "err" : me.value?.connUp ? "up" : "wait"));
</script>

<template>
  <div class="conn-strip" role="status" :data-state="state" :hidden="!me || state === 'up'">
    <span class="dot" /><span>{{ me?.connErr ? "Brak połączenia z connectorem" : "Łączenie z connectorem…" }}</span>
  </div>
</template>
