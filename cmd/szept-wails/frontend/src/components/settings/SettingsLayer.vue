<script setup lang="ts">
import { computed, ref, watch, type Component } from "vue";
import { closeSettings, dirty, shakes } from "@/store/settings";
import { S } from "@/store/state";
import Icon from "@/components/base/Icon.vue";
import SettingsNav from "./SettingsNav.vue";
import ProfileSection from "./sections/ProfileSection.vue";
import DevicesSection from "./sections/DevicesSection.vue";
import AccountsSection from "./sections/AccountsSection.vue";
import SecuritySection from "./sections/SecuritySection.vue";
import ConnectorSection from "./sections/ConnectorSection.vue";
import VoiceSection from "./sections/VoiceSection.vue";
import LookSection from "./sections/LookSection.vue";
import LangSection from "./sections/LangSection.vue";
import BotsSection from "./sections/BotsSection.vue";
import DonateSection from "./sections/DonateSection.vue";
import AboutSection from "./sections/AboutSection.vue";

// Settings float over the app as one big sheet.
const VIEWS: Record<string, Component> = {
  profile: ProfileSection, devices: DevicesSection, accounts: AccountsSection, security: SecuritySection,
  connector: ConnectorSection, voice: VoiceSection, look: LookSection, lang: LangSection, bots: BotsSection,
  donate: DonateSection, about: AboutSection,
};
const view = computed(() => VIEWS[S.section] || AboutSection);

// Unsaved edits: typing into a field marks its block; the bar offers to save
// (the block's own primary button) or reset (the section mounts afresh).
const resets = ref(0);
const body = ref<HTMLElement>();
function onInput(e: Event) {
  const t = e.target as HTMLElement;
  if (t.matches(".input")) dirty.value = t.closest<HTMLElement>(".block");
}
// the block's own save button counts as saving, same as the bar's
function onClick(e: MouseEvent) {
  const b = (e.target as Element).closest(".btn.primary");
  if (b && dirty.value?.contains(b)) dirty.value = null;
}
function saveDirty() {
  const b = dirty.value?.querySelector<HTMLElement>(".btn.primary") || dirty.value?.querySelector<HTMLElement>(".btn-row .btn");
  dirty.value = null;
  b?.click();
}
function reset() { dirty.value = null; resets.value++; }
watch(() => S.section, () => { dirty.value = null; body.value?.scrollTo(0, 0); });

// the bar shakes when something tries to leave with edits unsaved
const bar = ref<HTMLElement>();
watch(shakes, () => {
  const el = bar.value!;
  el.classList.remove("shake"); void el.offsetWidth; el.classList.add("shake");
});
</script>

<template>
  <div class="set-layer">
    <div class="settings" :class="{ paged: S.setPaged }" role="dialog" aria-modal="true" aria-label="Ustawienia">
      <SettingsNav />
      <div ref="body" class="set-body">
        <button class="icon-btn set-back" title="Wróć" @click="S.setPaged = false"><Icon name="chev" /></button>
        <div v-appear="S.section" class="wrap" @input="onInput" @click="onClick">
          <component :is="view" :key="S.section + resets" />
        </div>
        <div class="set-close"><button class="round-btn" title="Zamknij" @click="closeSettings"><Icon name="x" /></button><span>Esc</span></div>
      </div>
      <div ref="bar" class="unsaved" :class="{ show: !!dirty }"><span>Masz niezapisane zmiany</span>
        <button class="btn link" @click="reset">Cofnij</button>
        <button class="btn save" @click="saveDirty">Zapisz zmiany</button>
      </div>
    </div>
  </div>
</template>
