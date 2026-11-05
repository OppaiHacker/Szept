import { reactive } from "vue";
import { errText } from "@/lib/format";

export type ToastKind = "" | "ok" | "err" | "warn";
export const toasts = reactive<{ id: number; text: string; kind: ToastKind }[]>([]);

let seq = 0;
export function toast(text: string, kind: ToastKind = "") {
  const id = ++seq;
  toasts.push({ id, text, kind });
  setTimeout(() => {
    const i = toasts.findIndex((t) => t.id === id);
    if (i >= 0) toasts.splice(i, 1);
  }, kind === "err" ? 4200 : 2400);
}

// fail: a rejected Go call as an error toast
export const fail = (e: unknown) => toast(errText(e), "err");
