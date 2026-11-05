<script setup lang="ts">
import type { Channel } from "@/api/types";
import { dropChannel, putChannel } from "@/store/actions";
import { categoryDialog, channelDialog } from "@/store/dialogs";
import { curGroup } from "@/store/getters";
import { confirmBox, promptBox } from "@/ui/dialogs";
import MenuItem from "./MenuItem.vue";

// The owner's menu on the channel list. A channel goes to the archive; only
// a category is deleted outright.
const props = defineProps<{ x?: Channel; cat: string; kind: string }>();

async function rename() {
  const x = props.x!;
  const name = await promptBox({ title: x.kind === "cat" ? "Zmień nazwę kategorii" : "Zmień nazwę kanału", label: "Nazwa", value: x.name });
  if (name?.trim()) putChannel(x.id, x.kind, name.trim(), x.cat, x.arch);
}
async function del() {
  const x = props.x!;
  if (x.kind === "cat") {
    if (await confirmBox(`Usunąć kategorię „${x.name}”?`, "Kanały z tej kategorii przejdą do domyślnych.")) putChannel(x.id, "cat", "", "");
    return;
  }
  await dropChannel(curGroup()!, x, x.name, true);
}
const restore = () => putChannel(props.x!.id, props.x!.kind, props.x!.name, props.x!.cat, false);
async function purge() {
  const x = props.x!;
  if (await confirmBox(`Usunąć „${x.name}” na stałe?`, "Kanału nie da się potem przywrócić, a jego wiadomości znikną z listy u wszystkich.")) {
    await dropChannel(curGroup()!, x, "", false);
  }
}
</script>

<template>
  <div class="menu">
    <MenuItem label="Utwórz kanał" icon="plus" :run="() => channelDialog(cat, kind === 'voice' ? 'voice' : 'text')" />
    <MenuItem label="Utwórz kategorię" icon="plus" :run="categoryDialog" />
    <template v-if="x?.arch">
      <i class="menu-sep" />
      <MenuItem label="Przywróć kanał" icon="plus" :run="restore" />
      <MenuItem label="Usuń na stałe" icon="trash" danger :run="purge" />
    </template>
    <template v-else-if="x">
      <i class="menu-sep" />
      <MenuItem :label="x.kind === 'cat' ? 'Zmień nazwę kategorii' : 'Zmień nazwę kanału'" icon="edit" :run="rename" />
      <MenuItem :label="x.kind === 'cat' ? 'Usuń kategorię' : 'Przenieś do archiwum'" icon="trash" danger :run="del" />
    </template>
  </div>
</template>
