<script setup lang="ts">
import Logo from "@/components/base/Logo.vue";
</script>

<template>
  <div class="gate">
    <div class="gate-card">
      <div class="gate-logo"><Logo /><h1>szept</h1><p>Szyfrowane rozmowy prosto między urządzeniami</p></div>
      <div class="panel"><slot /></div>
      <slot name="after" />
    </div>
  </div>
</template>
