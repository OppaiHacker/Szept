<script setup lang="ts">
import { errText } from "@/lib/format";
import { boot } from "@/store/actions";
import { S } from "@/store/state";
import "@/store/global";
import "@/store/live";
import "@/store/nav";
import "@/store/phone";
import TitleBar from "@/components/shell/TitleBar.vue";
import ConnStrip from "@/components/shell/ConnStrip.vue";
import GateView from "@/components/gate/GateView.vue";
import AppShell from "@/components/shell/AppShell.vue";
import SettingsLayer from "@/components/settings/SettingsLayer.vue";
import ToastHost from "@/components/overlay/ToastHost.vue";
import ModalHost from "@/components/overlay/ModalHost.vue";
import PopoverHost from "@/components/overlay/PopoverHost.vue";
import LightboxHost from "@/components/overlay/LightboxHost.vue";
import RailTip from "@/components/overlay/RailTip.vue";
import CallPip from "@/components/call/CallPip.vue";
import GateCard from "@/components/gate/GateCard.vue";

boot().catch((e) => { S.bootErr = errText(e); S.stage = "dead"; });
</script>

<template>
  <TitleBar />
  <main class="view">
    <GateCard v-if="S.stage === 'dead'">
      <h3>szept nie wstał</h3>
      <p class="hint-text err">{{ S.bootErr }}</p>
    </GateCard>
    <GateView v-else-if="S.stage === 'gate'" />
    <AppShell v-else-if="S.stage === 'ready'" />
  </main>
  <ConnStrip />
  <Transition leave-active-class="out" type="animation">
    <SettingsLayer v-if="S.settingsOpen && S.settings && S.snap" />
  </Transition>
  <CallPip v-if="S.stage === 'ready'" />
  <ToastHost />
  <ModalHost />
  <PopoverHost />
  <LightboxHost />
  <RailTip />
</template>
