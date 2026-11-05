import { shallowReactive } from "vue";
import type { Channel } from "@/api/types";
import { putChannel } from "@/store/actions";

// Owner only: a channel row dragged onto a category moves there; the default
// sections take only their own kind.
export const drag = shallowReactive({ ch: null as Channel | null, over: null as string | null });

// takes: may section (catId "" = a default section of that kind) take the dragged row
export const takes = (catId: string, kind: string) => !!drag.ch && (!!catId || kind === drag.ch.kind);

export function dropOn(catId: string, kind: string) {
  const x = drag.ch, ok = takes(catId, kind);
  drag.ch = null; drag.over = null;
  if (x && ok && (x.cat || "") !== catId) putChannel(x.id, x.kind, x.name, catId, x.arch);
}
