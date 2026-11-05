<script setup lang="ts">
import { setFocus } from "@/store/actions";
import { userMenu } from "@/store/menus";
import { S } from "@/store/state";
import { pointAnchor } from "@/ui/popover";
import Avatar from "@/components/base/Avatar.vue";
import Icon from "@/components/base/Icon.vue";
import Speak from "@/components/base/Speak.vue";
import CallFlags from "./CallFlags.vue";
import FrameCanvas from "./FrameCanvas.vue";
import type { Tile } from "./tile";

// One tile of the voice view: a camera or a shared screen, or the avatar with
// its speaking ring when there is no picture. A click on a picture blows it
// up over the stage, a double click makes it full screen.
const props = defineProps<{ t: Tile }>();

function click(e: MouseEvent) {
  if (!props.t.src || (e.target as Element).closest(".vfull")) return;
  setFocus(S.focus === props.t.key && !S.full ? null : props.t.key, S.full);
}
const dbl = () => props.t.src && setFocus(props.t.key, !S.full);
const full = () => setFocus(props.t.key, !(S.full && S.focus === props.t.key));
</script>

<template>
  <div class="vtile" :class="{ cam: !!t.src, screen: t.screen, muted: t.muted, focus: S.focus === t.key, full: S.focus === t.key && S.full }"
    :data-key="t.key" @click="click" @dblclick="dbl" @contextmenu.prevent="userMenu(t.pub, pointAnchor($event))">
    <template v-if="t.src">
      <FrameCanvas :src="t.src" />
      <span class="cap">{{ t.name }}<template v-if="t.screen"> · ekran</template><CallFlags v-else :flags="t.flags" /></span>
      <button class="vfull" title="Pełny ekran" @click.stop="full"><Icon name="expand" /></button>
    </template>
    <template v-else>
      <Speak :pub="t.pub"><Avatar :url="t.avatar" :name="t.name" :seed="t.pub" size="xl" /></Speak>
      <span class="cap">{{ t.name }}<CallFlags :flags="t.flags" /></span>
    </template>
  </div>
</template>
