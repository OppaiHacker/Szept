<script setup lang="ts">
defineProps<{ typing: string[] }>();
</script>

<template>
  <div class="typing">
    <template v-if="typing.length">
      <span class="dots"><i /><i /><i /></span>{{ typing.length === 1 ? `${typing[0]} pisze` : `${typing.join(", ")} piszą` }}…
    </template>
  </div>
</template>
