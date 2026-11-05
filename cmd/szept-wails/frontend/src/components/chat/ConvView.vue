<script setup lang="ts">
import type { Conversation } from "@/api/types";
import ConvHead from "./ConvHead.vue";
import MessageList from "./MessageList.vue";
import TypingLine from "./TypingLine.vue";
import Composer from "./Composer.vue";
import HeadSearch from "./HeadSearch.vue";

// An open text chat. It is mounted once per chat and channel, so a sync
// burst only patches what changed and never yanks focus out of the composer.
defineProps<{ conv: Conversation }>();
</script>

<template>
  <header class="conv-head">
    <ConvHead :conv="conv" />
    <HeadSearch />
  </header>
  <div v-appear class="conv-body" data-file-drop-target>
    <MessageList :conv="conv" />
    <TypingLine :typing="conv.typing" />
    <Composer :conv="conv" />
  </div>
</template>
