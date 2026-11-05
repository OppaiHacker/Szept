<script setup lang="ts">
import { joinDialog, newGroupDialog } from "@/store/dialogs";
import Icon from "@/components/base/Icon.vue";
</script>

<template>
  <div class="pane-head"><h2>Serwery</h2></div>
  <div v-appear class="scroll">
    <div class="empty"><i class="empty-ico"><Icon name="server" /></i><strong>Nie jesteś jeszcze w żadnej grupie</strong>
      Załóż własną albo wejdź z zaproszenia, które ktoś ci wysłał.
      <div class="btn-row">
        <button class="btn primary" @click="newGroupDialog()">Nowa grupa</button>
        <button class="btn ghost" @click="joinDialog()">Mam zaproszenie</button>
      </div>
    </div>
  </div>
</template>
