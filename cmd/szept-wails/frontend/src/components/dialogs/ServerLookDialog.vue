<script setup lang="ts">
import { computed, ref } from "vue";
import { api } from "@/api/wails";
import { tint } from "@/lib/avatar";
import { FX, NO_STYLE, PAL, parseStyle, styleStr, type StyleKey } from "@/lib/profileStyle";
import { afterEdit, refresh } from "@/store/actions";
import { pickPicture, serverLook } from "@/store/dialogs";
import { chatById } from "@/store/getters";
import { confirmBox } from "@/ui/dialogs";
import { closeModal } from "@/ui/modal";
import { fail } from "@/ui/toast";
import ModalFrame from "@/components/overlay/ModalFrame.vue";
import ServerIcon from "@/components/base/ServerIcon.vue";

// The owner's editor for a group's icon, banner and decoration. The
// decoration is a draft until "Zapisz"; pictures save straight from the crop.
const props = defineProps<{ chat: string }>();
const g = computed(() => chatById(props.chat)!);
const st = ref(parseStyle(g.value?.style));
const set = (k: StyleKey, v: number) => (st.value = { ...st.value, [k]: v });

async function clear(kind: "icon" | "banner") {
  try { await api("SetServerImage", props.chat, kind, "", {}); await refresh(); } catch (e) { fail(e); }
}
async function history() {
  const on = !g.value.history;
  if (on && !await confirmBox("Odsłonić historię?", "Wszyscy obecni i przyszli członkowie dostaną klucze do wcześniejszych wiadomości. Tego nie da się cofnąć dla osób, które już je dostaną.", false)) {
    return serverLook(props.chat); // the confirm replaced this sheet
  }
  try { await api("SetHistory", props.chat, on); await refresh(); } catch (e) { fail(e); }
  if (on) serverLook(props.chat);
}
async function save() {
  const v = styleStr(st.value);
  closeModal(null);
  if (v === (g.value.style || NO_STYLE)) return;
  try { await api("SetServerStyle", props.chat, v); await afterEdit("Zapisane"); } catch (e) { fail(e); }
}
</script>

<template>
  <ModalFrame v-if="g" title="Ustawienia serwera" wide>
    <div class="sl">
      <div class="sl-banner" :style="{ '--tone': tint(chat), backgroundImage: g.banner ? `url('${g.banner}')` : undefined }">
        <span class="sl-icon big"><ServerIcon :avatar="g.avatar" :name="g.name" :style-str="styleStr(st)" /></span>
      </div>
      <div class="btn-row">
        <button class="btn ghost" @click="pickPicture('icon', chat)">Zmień ikonę</button>
        <button v-if="g.avatar" class="btn ghost" @click="clear('icon')">Usuń ikonę</button>
        <button class="btn ghost" @click="pickPicture('banner', chat)">Zmień baner</button>
        <button v-if="g.banner" class="btn ghost" @click="clear('banner')">Usuń baner</button>
      </div>
      <div class="toggle">
        <div class="tx"><b>Nowi członkowie widzą wcześniejszą historię</b><span>{{ g.history
          ? "Każda dodana osoba czyta wszystko, co napisano przed jej dołączeniem."
          : "Nowa osoba widzi tylko to, co napisano po jej dołączeniu. Włączenie odsłoni historię także osobom, które już dołączyły." }}</span></div>
        <button class="switch" :class="{ on: g.history }" @click="history" />
      </div>
      <div class="fx-row"><label>Dekoracja ikony</label>
        <div class="fx-tiles">
          <button v-for="(label, v) in FX.d" :key="v" class="fx-tile" :class="{ on: st.d === v }" :title="label" @click="set('d', v)">
            <span class="sl-icon"><ServerIcon :avatar="g.avatar" :name="g.name" :style-str="styleStr({ ...st, d: v })" /></span><span>{{ label }}</span>
          </button>
        </div>
      </div>
      <div class="fx-row"><label>Kolory obwódki</label>
        <div class="fx-cols">
          <div v-for="k in (['a', 'b'] as const)" :key="k" class="fx-sw">
            <button v-for="(c, v) in PAL" :key="v" :class="{ on: st[k] === v }" :style="{ '--c': c }" :title="`Kolor ${v + 1}`" @click="set(k, v)" />
          </div>
        </div>
      </div>
    </div>
    <template #actions>
      <button class="btn ghost" @click="closeModal(null)">Anuluj</button>
      <button class="btn primary" @click="save">Zapisz dekorację</button>
    </template>
  </ModalFrame>
</template>
