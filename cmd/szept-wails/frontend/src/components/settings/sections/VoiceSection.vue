<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { api, onEvent } from "@/api/wails";
import type { AudioDevice, VoiceOpts } from "@/api/types";
import { camTest, micLevel, micTest, saveSettings } from "@/store/settings";
import { S } from "@/store/state";
import { fail } from "@/ui/toast";
import FrameCanvas from "@/components/call/FrameCanvas.vue";
import Toggle from "./Toggle.vue";

const s = computed(() => S.settings!);
const v = computed<VoiceOpts>(() => s.value.voice || {});
const mic = ref(s.value.micDevice), spk = ref(s.value.speakerDevice), cam = ref(s.value.camDevice);
const gain = ref(s.value.micGain || 1), vol = ref(s.value.outputVol || 1);

onMounted(() => {
  if (!S.devices) api<{ mics: AudioDevice[]; speakers: AudioDevice[] }>("AudioDevices").then((d) => (S.devices = d)).catch(fail);
});
// leaving the section stops the tests
onBeforeUnmount(() => { if (S.micTest) micTest(false); if (S.camTest) camTest(false); });

const mics = computed(() => (S.devices?.mics || []).filter((d) => !d.name.startsWith("Monitor of ")));
const saveDevices = () => saveSettings({ micDevice: mic.value, speakerDevice: spk.value, camDevice: cam.value });

// voice options are stored as "no_*" flags: a switch is on when its flag is off
const setVoice = (patch: VoiceOpts) => saveSettings({ voice: { ...v.value, ...patch } });
const OPTS: [keyof VoiceOpts, string, string][] = [
  ["no_denoise", "Odszumianie", "usuwa wentylator, klawiaturę i szum ulicy (RNNoise 48 kHz)"],
  ["no_agc", "Automatyczna głośność (AGC)", "łagodne wyrównywanie poziomu mowy w tle (+4 dB/s)"],
  ["no_comp", "Kompresor dynamiki", "wygładza nagłe okrzyki i podbija cichsze sylaby (transparentny lookahead)"],
];
const OPTS2: [keyof VoiceOpts, string, string][] = [
  ["no_gate", "Wysyłaj tylko mowę", "w ciszy nic nie idzie do sieci, a pierścień świeci tylko gdy mówisz"],
  ["no_rx_protect", "Ochrona słuchu (odbiornik)", "kompresor i limiter wygładzający zbyt głośnych rozmówców"],
  ["no_monitor", "Odsłuch w teście mikrofonu", "słyszysz siebie podczas testu"],
];
const PRESETS = [["natural", "Naturalny (łagodny)"], ["gentle", "Delikatny (minimalny)"], ["radio", "Radio / Podcast (wyrazisty)"], ["custom", "Własny (zaawansowane)"]];
const BITRATES = [[24000, "Oszczędna, 24 kbit/s"], [32000, "Niska, 32 kbit/s"], [48000, "Standardowa, 48 kbit/s (zalecana)"], [64000, "Wysoka, 64 kbit/s"], [96000, "Bardzo wysoka, 96 kbit/s"], [128000, "Studyjna / Muzyka, 128 kbit/s"]] as const;

// the custom compressor: the label follows the drag, saving waits for release
const COMP: { k: keyof VoiceOpts; label: string; min: number; max: number; step: number; def: number; fmt: (x: number) => string }[] = [
  { k: "comp_thresh", label: "Próg (Threshold)", min: -36, max: -6, step: 1, def: -18, fmt: (x) => x + " dB" },
  { k: "comp_ratio", label: "Współczynnik (Ratio)", min: 1.5, max: 8, step: 0.5, def: 3, fmt: (x) => x + ":1" },
  { k: "comp_attack", label: "Czas ataku (Attack)", min: 2, max: 50, step: 1, def: 12, fmt: (x) => x + " ms" },
  { k: "comp_release", label: "Czas powrotu (Release)", min: 20, max: 300, step: 10, def: 100, fmt: (x) => x + " ms" },
  { k: "comp_makeup", label: "Wzmocnienie (Makeup)", min: 0, max: 12, step: 0.5, def: 2, fmt: (x) => "+" + x + " dB" },
];
const comp = ref(Object.fromEntries(COMP.map((c) => [c.k, (v.value[c.k] as number) || c.def])));

function setGain(e: Event) {
  gain.value = +(e.target as HTMLInputElement).value;
  S.settings!.micGain = gain.value;
  api("SetMicGain", gain.value).catch(() => {});
}
function setVol(e: Event) {
  vol.value = +(e.target as HTMLInputElement).value;
  api("SetOutputVolume", vol.value).catch(() => {});
}

// the camera test streams frames like a call does
const camSeq = ref(0);
const off = onEvent<number>("camtest", (n) => (camSeq.value = n));
onBeforeUnmount(off);
const num = (e: Event) => +(e.target as HTMLInputElement).value;
</script>

<template>
  <h2>Głos i wideo</h2>
  <p class="lead">Rozmowy idą pełną siatką P2P: każdy wysyła do każdego. Wygodnie do ok. ośmiu osób.</p>
  <div class="block">
    <div class="dev-row">
      <div class="field"><label>Mikrofon</label><div class="select-wrap"><select v-model="mic" class="input">
        <option value="">Domyślne systemowe</option>
        <option v-for="d in mics" :key="d.id" :value="d.id">{{ d.name }}</option>
      </select></div></div>
      <div class="field"><label>Głośnik</label><div class="select-wrap"><select v-model="spk" class="input">
        <option value="">Domyślne systemowe</option>
        <option v-for="d in S.devices?.speakers || []" :key="d.id" :value="d.id">{{ d.name }}</option>
      </select></div></div>
    </div>
    <p class="hint-text pad">Zmiana urządzenia działa od następnej rozmowy.</p>
    <div class="btn-row"><button class="btn primary" @click="saveDevices">Zapisz</button></div>
  </div>
  <div class="block">
    <h4>Głośność mikrofonu</h4>
    <div class="field"><input class="slider" type="range" min="0" max="2" step="0.05" :value="gain" @input="setGain"></div>
    <p class="hint-text pad">{{ Math.round(gain * 100) }}%</p>
    <div class="mic-test">
      <button class="btn" :class="{ primary: !S.micTest }" @click="micTest(!S.micTest)">{{ S.micTest ? "Zatrzymaj test" : "Test mikrofonu" }}</button>
      <div class="meter" :style="{ '--lvl': micLevel.toFixed(3) }"><i /></div>
    </div>
    <p class="hint-text pad-b">{{ v.no_monitor ? "Powiedz coś: pasek pokazuje, jak głośno inni Cię słyszą." : "Powiedz coś: usłyszysz siebie tak, jak słyszą Cię inni. Najlepiej w słuchawkach." }} Nic nie jest wysyłane.</p>
  </div>
  <div class="block">
    <h4>Przetwarzanie dźwięku</h4>
    <p class="hint-text pad">Działa od razu, także w trwającej rozmowie i w teście mikrofonu.</p>
    <Toggle v-for="[k, t, d] in OPTS" :key="k" :title="t" :on="!v[k]" @flip="setVoice({ [k]: !v[k] })">{{ d }}</Toggle>
    <div class="field inset"><label>Profil kompresora</label><div class="select-wrap">
      <select class="input" :disabled="v.no_comp" :value="v.comp_preset || 'natural'"
        @change="setVoice({ comp_preset: ($event.target as HTMLSelectElement).value })">
        <option v-for="[p, l] in PRESETS" :key="p" :value="p">{{ l }}</option>
      </select></div></div>
    <div v-if="!v.no_comp && v.comp_preset === 'custom'" class="comp-custom">
      <template v-for="c in COMP" :key="c.k">
        <div class="lbl">{{ c.label }}: <span>{{ c.fmt(comp[c.k]) }}</span></div>
        <input class="slider" type="range" :min="c.min" :max="c.max" :step="c.step" :value="comp[c.k]"
          @input="comp[c.k] = num($event)" @change="setVoice({ [c.k]: num($event) })">
      </template>
    </div>
    <Toggle v-for="[k, t, d] in OPTS2" :key="k" :title="t" :on="!v[k]" @flip="setVoice({ [k]: !v[k] })">{{ d }}</Toggle>
    <div class="field"><label>Jakość dźwięku (Opus)</label><div class="select-wrap">
      <select class="input" :value="v.bitrate || 48000" @change="setVoice({ bitrate: +($event.target as HTMLSelectElement).value })">
        <option v-for="[b, l] in BITRATES" :key="b" :value="b">{{ l }}</option>
      </select></div></div>
    <p class="hint-text pad-b">Tyle wysyłasz do każdej osoby w rozmowie. 48 kbit/s zapewnia pełne pasmo mowy (Fullband 48 kHz) z minimalnym narzutem.</p>
  </div>
  <div class="block">
    <h4>Głośność rozmowy</h4>
    <div class="field"><input class="slider" type="range" min="0" max="2" step="0.05" :value="vol" @input="setVol"></div>
    <p class="hint-text pad">{{ Math.round(vol * 100) }}%</p>
  </div>
  <div class="block">
    <h4>Kamera</h4>
    <div class="field"><label>Urządzenie</label><input v-model="cam" class="input" placeholder="/dev/video0"></div>
    <p class="hint-text pad">Puste = pierwsza znaleziona kamera.</p>
    <div class="btn-row">
      <button class="btn primary" @click="saveDevices">Zapisz</button>
      <button class="btn" @click="camTest(!S.camTest)">{{ S.camTest ? "Zatrzymaj podgląd" : "Test kamery" }}</button>
    </div>
    <FrameCanvas v-if="S.camTest && camSeq" class="cam-prev" :src="`/frame/camtest?n=${camSeq}`" />
  </div>
  <div class="block">
    <Toggle title="Powiadomienia systemowe" :on="!s.noNotify" @flip="saveSettings({ noNotify: !s.noNotify })">wiadomości prywatne i wzmianki (@ty, @everyone, @here), gdy nie patrzysz na ten czat</Toggle>
    <Toggle v-if="!S.boot?.mobile" title="Automatyczne aktualizacje" :on="!s.noUpdate" @flip="saveSettings({ noUpdate: !s.noUpdate })">co kilka godzin pyta GitHuba o nową wersję podpisaną kluczem szepta i ją instaluje. Wyłączone: szept nie łączy się z GitHubem sam, sprawdzasz przyciskiem w zakładce O szepcie.</Toggle>
  </div>
</template>
