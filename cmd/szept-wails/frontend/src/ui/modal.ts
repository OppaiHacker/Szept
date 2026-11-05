import { markRaw, shallowRef, type Component } from "vue";

// One dialog at a time. openModal resolves with what the dialog emitted with
// "close" (null when dismissed). Opening another closes the current one; the
// old sheet leaves on its own, so the next one never waits for its exit.
interface Open { id: number; comp: Component; props: Record<string, unknown>; resolve: (v: unknown) => void }

export const current = shallowRef<Open | null>(null);
let seq = 0;

export function openModal<R = unknown>(comp: Component, props: Record<string, unknown> = {}): Promise<R | null> {
  closeModal(null);
  return new Promise((resolve) => {
    current.value = { id: ++seq, comp: markRaw(comp), props, resolve: resolve as (v: unknown) => void };
  });
}

export function closeModal(result: unknown = null) {
  const m = current.value;
  if (!m) return;
  current.value = null;
  m.resolve(result);
}

export const modalOpen = () => !!current.value;
