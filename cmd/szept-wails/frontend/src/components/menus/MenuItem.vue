<script setup lang="ts">
import type { IconName } from "@/lib/icons";
import { closePopover } from "@/ui/popover";
import Icon from "@/components/base/Icon.vue";

// A menu row. Picking it closes the menu, then runs it.
const props = defineProps<{ label: string; icon?: IconName; danger?: boolean; run: () => unknown }>();
const go = () => { closePopover(); props.run(); };
</script>

<template>
  <button class="menu-item" :class="{ danger }" @click="go">{{ label }}<Icon v-if="icon" :name="icon" /></button>
</template>
