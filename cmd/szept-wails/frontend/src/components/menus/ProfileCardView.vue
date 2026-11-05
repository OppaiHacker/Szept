<script setup lang="ts">
import { computed } from "vue";
import type { ProfileCard } from "@/api/types";
import { tint } from "@/lib/avatar";
import { dateOf } from "@/lib/format";
import { nameFx } from "@/lib/profileStyle";
import { pickPicture } from "@/store/dialogs";
import { styleOf } from "@/store/getters";
import { dmWith, kick, unfriend } from "@/store/people";
import { openSettings } from "@/store/settings";
import { S } from "@/store/state";
import { closePopover } from "@/ui/popover";
import Avatar from "@/components/base/Avatar.vue";
import Icon from "@/components/base/Icon.vue";
import KeyBlock from "@/components/base/KeyBlock.vue";
import PCard from "@/components/base/PCard.vue";
import ModalFrame from "@/components/overlay/ModalFrame.vue";

// A profile card, as a popover next to what you clicked or (sheet) as a dialog.
const props = defineProps<{ p: ProfileCard; sheet?: boolean }>();
const style = computed(() => (props.p.isMe ? styleOf(props.p.pub) : props.p.style));
const canKick = computed(() => S.conv && !S.conv.direct && S.conv.isOwner && !props.p.isMe);
const go = (fn: () => unknown) => { closePopover(); fn(); };
</script>

<template>
  <component :is="sheet ? ModalFrame : 'div'" v-bind="sheet ? { title: 'Profil', bare: true } : {}">
    <PCard :style-str="style">
      <div class="card-banner" :class="{ own: p.isMe }" :title="p.isMe ? 'Zmień baner' : undefined"
        :style="{ '--tone': tint(p.pub), backgroundImage: p.banner ? `url('${p.banner}')` : undefined }"
        @click="p.isMe && go(() => pickPicture('banner'))">
        <span v-if="p.isMe" class="icon-btn edit"><Icon name="edit" /></span>
      </div>
      <div class="card-id">
        <div class="av-wrap" :class="{ online: p.online }">
          <button v-if="p.isMe" title="Zmień awatar" @click="go(() => pickPicture('avatar'))">
            <Avatar :url="p.avatar" :name="p.name" :seed="p.pub" size="xl" :style-str="style" />
          </button>
          <Avatar v-else :url="p.avatar" :name="p.name" :seed="p.pub" size="xl" :style-str="style" />
          <i class="presence" />
        </div>
        <div class="nm"><span v-bind="nameFx(style)">{{ p.name || "nieznany" }}</span></div>
        <div class="st">{{ p.status || (p.online ? "online" : "offline") }}</div>
        <div v-if="p.bio" class="bio">{{ p.bio }}</div>
        <div v-if="p.version" class="st">szept <span class="mono">{{ p.version }}</span></div>
      </div>
      <KeyBlock :pub="p.pub" />
      <div class="btn-row">
        <button v-if="p.isMe" class="btn" @click="go(() => openSettings('profile', true))"><Icon name="edit" />Edytuj profil</button>
        <template v-else>
          <button class="btn primary" @click="go(() => dmWith(p.pub))"><Icon name="chat" />Napisz</button>
          <button v-if="p.isFriend" class="btn danger" @click="go(() => unfriend(p.pub))"><Icon name="trash" />Usuń ze znajomych</button>
          <button v-if="canKick" class="btn danger" @click="go(() => kick(p.pub))"><Icon name="logout" />Wyrzuć z czatu</button>
        </template>
      </div>
      <template v-if="p.history.length > 1">
        <div class="sec-label">Historia nazw</div>
        <div v-for="(h, i) in p.history" :key="i" class="list-line">
          <span class="mono">{{ h.name }}</span>
          <span v-if="i === p.history.length - 1" class="tag">teraz</span>
          <span class="when">{{ dateOf(h.ts) }}</span>
        </div>
      </template>
    </PCard>
  </component>
</template>
