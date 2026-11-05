<script setup lang="ts">
import { computed } from "vue";
import { api } from "@/api/wails";
import { FX, NO_STYLE, PAL, nameFx, parseStyle, styleStr, type Style, type StyleKey } from "@/lib/profileStyle";
import { tint } from "@/lib/avatar";
import { refresh } from "@/store/actions";
import { S } from "@/store/state";
import { fail, toast } from "@/ui/toast";
import Avatar from "@/components/base/Avatar.vue";
import PCard from "@/components/base/PCard.vue";

// Name style, avatar decoration, card frame and effect, with a live card
// preview. Nothing leaves the device until "Zapisz efekty".
const me = computed(() => S.snap!.me);
const cur = computed(() => S.draftStyle ?? me.value.style ?? "");
const st = computed(() => parseStyle(cur.value));
const dirty = computed(() => S.draftStyle !== undefined && S.draftStyle !== (me.value.style || ""));

const pick = (k: StyleKey, v: number) => { S.draftStyle = styleStr({ ...st.value, [k]: v }); };
// one: a preview tile showing a single option on its own
const one = (o: Partial<Style>) => styleStr({ ...st.value, f: 0, e: 0, d: 0, r: 0, x: 0, ...o });

async function save() {
  try {
    await api("SetStyle", S.draftStyle);
    S.snap!.me.style = S.draftStyle!;
    S.draftStyle = undefined;
    toast("Zapisane", "ok");
  } catch (e) { fail(e); }
  refresh();
}
const rows = computed(() => [
  { k: "f" as const, label: "Czcionka nazwy", list: FX.f },
  { k: "e" as const, label: "Efekt nazwy", list: FX.e },
]);
</script>

<template>
  <div class="block fx">
    <h4>Efekty profilu</h4>
    <p class="hint-text">Widzą je wszyscy w twoich czatach. Animacje wyłączają się, gdy system prosi o mniej ruchu.</p>
    <div class="fx-grid">
      <div class="fx-opts">
        <div v-for="r in rows" :key="r.k" class="fx-row"><label>{{ r.label }}</label>
          <div class="fx-tiles">
            <button v-for="(label, v) in r.list" :key="v" class="fx-tile" :class="{ on: st[r.k] === v }" :title="label" @click="pick(r.k, v)">
              <b class="fx-name" v-bind="nameFx(one(r.k === 'f' ? { f: v, e: st.e } : { f: st.f, e: v }))">Aa</b><span>{{ label }}</span>
            </button>
          </div>
        </div>
        <div class="fx-row"><label>Kolory</label>
          <div class="fx-cols">
            <div v-for="k in (['a', 'b'] as const)" :key="k" class="fx-sw">
              <button v-for="(c, v) in PAL" :key="v" :class="{ on: st[k] === v }" :style="{ '--c': c }" :title="`Kolor ${v + 1}`" @click="pick(k, v)" />
            </div>
          </div>
        </div>
        <div class="fx-row"><label>Dekoracja awatara</label>
          <div class="fx-tiles">
            <button v-for="(label, v) in FX.d" :key="v" class="fx-tile" :class="{ on: st.d === v }" :title="label" @click="pick('d', v)">
              <Avatar :url="me.avatar" :name="me.name" :seed="me.pub" :style-str="one({ d: v })" /><span>{{ label }}</span>
            </button>
          </div>
        </div>
        <div v-for="k in (['r', 'x'] as const)" :key="k" class="fx-row"><label>{{ k === "r" ? "Ramka karty" : "Efekt karty" }}</label>
          <div class="fx-tiles">
            <button v-for="(label, v) in FX[k]" :key="v" class="fx-tile" :class="{ on: st[k] === v }" :title="label" @click="pick(k, v)">
              <PCard tag="i" class="fx-mini" :style-str="one({ [k]: v })" /><span>{{ label }}</span>
            </button>
          </div>
        </div>
      </div>
      <div class="fx-preview">
        <PCard :style-str="cur">
          <div class="card-banner" :style="{ '--tone': tint(me.pub), backgroundImage: me.banner ? `url('${me.banner}')` : undefined }" />
          <div class="card-id">
            <div class="av-wrap online"><Avatar :url="me.avatar" :name="me.name" :seed="me.pub" size="xl" :style-str="cur" /><i class="presence" /></div>
            <div class="nm"><span v-bind="nameFx(cur)">{{ me.name }}</span></div>
            <div class="st">{{ me.status || "online" }}</div>
          </div>
        </PCard>
      </div>
    </div>
    <div class="btn-row">
      <button class="btn primary" :disabled="!dirty" @click="save">Zapisz efekty</button>
      <button v-if="dirty" class="btn ghost" @click="S.draftStyle = undefined">Cofnij</button>
      <button class="btn ghost" @click="S.draftStyle = NO_STYLE">Bez efektów</button>
    </div>
  </div>
</template>
