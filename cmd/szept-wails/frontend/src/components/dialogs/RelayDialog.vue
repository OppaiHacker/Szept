<script setup lang="ts">
import { closeModal } from "@/ui/modal";
import ModalFrame from "@/components/overlay/ModalFrame.vue";

// What a relay operator can and cannot do. Shown when you turn relay-only on
// (ask) and when a link, yours or the other side's choice, runs through a relay.
defineProps<{ ask?: boolean }>();
</script>

<template>
  <ModalFrame :title="ask ? 'Ukryć Twój adres IP?' : 'Rozmowa przez przekaźnik'">
    <p v-if="ask">Rozmówcy zobaczą adres connectora zamiast Twojego. W zamian wszystko, co robisz, przechodzi przez connector.</p>
    <p>Połączenie idzie przez przekaźnik (TURN) connectora, a nie bezpośrednio między wami.</p>
    <ul class="relay-facts">
      <li><b>Operator connectora widzi</b>, kto z kim rozmawia, kiedy, jak długo i ile danych przesyłacie. Może to zapisywać.</li>
      <li><b>Może zablokować albo zepsuć połączenie</b> w dowolnej chwili.</li>
      <li>Connector może prowadzić każdy. Najlepiej używaj connectora, którego operatora znasz albo któremu ufasz.</li>
      <li><b>Nie widzi treści</b>: wiadomości, pliki i rozmowy głosowe są szyfrowane end-to-end między wami. Nagrany ruch to dla niego szum, a klucze nie przechodzą przez connector.</li>
    </ul>
    <p v-if="ask">Każdy Twój rozmówca dostanie informację, że łączycie się przez przekaźnik. Rozmowy będą wolniejsze, a gdy connector nie ma przekaźnika, nie połączysz się z nikim.</p>
    <template #actions>
      <template v-if="ask">
        <button class="btn ghost" @click="closeModal(false)">Anuluj</button>
        <button class="btn danger" @click="closeModal(true)">Rozumiem, włącz</button>
      </template>
      <button v-else class="btn primary" @click="closeModal(true)">Rozumiem</button>
    </template>
  </ModalFrame>
</template>
