// Profile card effects: tsParticles (MIT) on a canvas behind the card frame.
// The engine and its plugins load on first use, so a session that never opens
// a decorated card never downloads them.
import type { Container, ISourceOptions } from "@tsparticles/engine";
import { CARDFX, decoSrc } from "./profileStyle";

let ready: Promise<typeof import("@tsparticles/engine").tsParticles> | null = null;

function engine() {
  return (ready ??= (async () => {
    const [{ tsParticles }, { loadSlim }, { loadWobbleUpdater }, { loadRollUpdater }] = await Promise.all([
      import("@tsparticles/engine"), import("@tsparticles/slim"),
      import("@tsparticles/updater-wobble"), import("@tsparticles/updater-roll"),
    ]);
    await loadSlim(tsParticles);
    await loadWobbleUpdater(tsParticles);
    await loadRollUpdater(tsParticles);
    return tsParticles;
  })());
}

// options: effect x scaled to a card of w by h (the presets are tuned for 300x260)
function options(x: number, w: number, h: number): ISourceOptions {
  const [, n = 0, imgs = [], size = [1, 1], dir = "none", speed = [0, 0], o = {}] = CARDFX[x];
  const image = imgs.map((f) => ({ src: decoSrc(f), width: 128, height: 128 }));
  const k = Math.min(1, (w * h) / (300 * 260)), z = Math.max(.6, Math.sqrt(k));
  const range = (v: [number, number], f = 1) => ({ min: v[0] * f, max: v[1] * f });
  return {
    fullScreen: { enable: false }, fpsLimit: 60, detectRetina: true, pauseOnOutsideViewport: true,
    particles: {
      number: { value: Math.max(6, Math.round(n * k)) },
      shape: { type: o.dots ? ["image", "circle", "circle"] : "image", options: { image } },
      color: { value: "#fff" },
      size: { value: range(size, z) },
      ...(o.dots && { shadow: { enable: true, color: "#4a6f99", blur: 3 } }),
      opacity: o.blink
        ? { value: { min: .15, max: 1 }, animation: { enable: true, speed: .8, sync: false } }
        : { value: { min: .7, max: 1 } },
      move: { enable: true, direction: dir, random: dir === "none", straight: false, speed: range(speed), outModes: { default: "out" } },
      ...(o.spin && { rotate: { value: { min: 0, max: 360 }, direction: "random", animation: { enable: true, speed: { min: 3, max: 12 } } } }),
      ...(o.wobble && { wobble: { enable: true, distance: { min: 3, max: 10 }, speed: { min: -6, max: 6 } } }),
      ...(o.roll && { roll: { enable: true, speed: { min: 3, max: 10 }, darken: { enable: true, value: 10 } } }),
    },
  } as ISourceOptions;
}

let seq = 0;
export async function startFx(el: HTMLElement, x: number, w: number, h: number): Promise<Container | undefined> {
  const ts = await engine();
  return ts.load({ id: "fx" + ++seq, element: el, options: options(x, w, h) });
}
