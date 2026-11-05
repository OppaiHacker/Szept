<script setup lang="ts">
import { ref } from "vue";
import { closeModal } from "@/ui/modal";
import Icon from "@/components/base/Icon.vue";
import ModalFrame from "@/components/overlay/ModalFrame.vue";

const props = defineProps<{ kind: "text" | "voice" }>();
const kind = ref(props.kind), name = ref("");
const KINDS = [
  { k: "text" as const, icon: "hash" as const, t: "Tekstowy", d: "Wiadomości, obrazy, pliki" },
  { k: "voice" as const, icon: "volume" as const, t: "Głosowy", d: "Rozmowa, kamera i ekran" },
];
const ok = () => closeModal({ name: name.value.trim(), kind: kind.value });
</script>

<template>
  <ModalFrame title="Utwórz kanał">
    <div class="field"><label>Typ kanału</label>
      <div class="kinds">
        <label v-for="x in KINDS" :key="x.k" class="kind">
          <input v-model="kind" type="radio" name="ck" :value="x.k"><Icon :name="x.icon" /><span><b>{{ x.t }}</b><small>{{ x.d }}</small></span>
        </label>
      </div>
    </div>
    <div class="field"><label>Nazwa kanału</label><input v-model="name" class="input" placeholder="nowy-kanał" maxlength="64" @keydown.enter="ok"></div>
    <template #actions>
      <button class="btn ghost" @click="closeModal(null)">Anuluj</button>
      <button class="btn primary" @click="ok">Utwórz kanał</button>
    </template>
  </ModalFrame>
</template>
