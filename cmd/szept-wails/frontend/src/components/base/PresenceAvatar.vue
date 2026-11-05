<script setup lang="ts">
import Avatar from "./Avatar.vue";

// an avatar with the online dot cut into its corner
defineProps<{ url?: string; name?: string; seed?: string; online?: boolean; size?: "" | "xs" | "s" | "m" | "xl"; styleStr?: string }>();
</script>

<template>
  <div class="av-wrap" :class="{ online }">
    <Avatar :url="url" :name="name" :seed="seed" :size="size" :style-str="styleStr" /><i class="presence" />
  </div>
</template>
