<script setup lang="ts">
import { closeModal, current } from "@/ui/modal";
</script>

<template>
  <Transition leave-active-class="out" type="animation">
    <div v-if="current" :key="current.id" class="backdrop" @click.self="closeModal(null)">
      <component :is="current.comp" v-bind="current.props" />
    </div>
  </Transition>
</template>
