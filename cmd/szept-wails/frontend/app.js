// szept front end. No framework, no build step: the whole UI is DOM built from
// template strings plus one delegated click handler. Go pushes a "sync" event
// whenever the DAG changes; everything below repaints from a fresh snapshot.

import { Call, Events } from "/wails/runtime.js";

const api = (method, ...args) => Call.ByName("main.App." + method, ...args);

/* ============================ theme ============================ */

// Dark unless the user picks light or "auto" (follow the desktop). Resolving here and
// stamping the answer on <html> keeps the stylesheet down to one palette per
// theme — no media query duplicated for every token.
const LIGHT_Q = matchMedia("(prefers-color-scheme: light)");
const NARROW_Q = matchMedia("(max-width: 640px)"); // keep in step with app.css
const REDUCED_Q = matchMedia("(prefers-reduced-motion: reduce)");
let themePref = "dark";
try { themePref = localStorage.getItem("theme") || "dark"; } catch { /* opaque origin */ }

function applyTheme() {
  document.documentElement.dataset.theme =
    themePref === "auto" ? (LIGHT_Q.matches ? "light" : "dark") : themePref;
}
function setTheme(pref) {
  // the whole palette crossfades instead of flashing to the other theme
  const html = document.documentElement;
  if (!REDUCED_Q.matches) { html.classList.add("theming"); setTimeout(() => html.classList.remove("theming"), 320); }
  themePref = pref;
  try { localStorage.setItem("theme", pref); } catch { /* not persisted, still applied */ }
  applyTheme();
}
LIGHT_Q.addEventListener("change", applyTheme);
applyTheme();

/* ============================ tiny helpers ============================ */

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];
const view = $("#view");

function esc(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

// linkify: URLs stay clickable but never navigate the webview away from the app.
function body(text) {
  return esc(text).replace(/\bhttps?:\/\/[^\s<]+/g, (u) =>
    `<a href="${u}" data-act="ext" data-url="${u}">${u}</a>`);
}

// Avatar grounds: eight inks at one lightness and chroma, so no face shouts
// louder than another and white initials read on all of them.
const AVATAR_HUES = [15, 45, 145, 185, 220, 255, 290, 330];
function tint(seed) {
  let h = 0;
  for (const ch of String(seed)) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return `oklch(.54 .09 ${AVATAR_HUES[h % AVATAR_HUES.length]})`;
}

function initials(name) {
  const parts = String(name || "?").trim().split(/\s+/).slice(0, 2);
  return parts.map((p) => [...p][0] || "").join("").toUpperCase() || "?";
}

function avatar(url, name, seed, cls = "", style) {
  const a = url ? `<div class="avatar ${cls}"><img src="${esc(url)}" alt=""></div>`
    : `<div class="avatar ${cls}" style="background:${tint(seed || name)}">${esc(initials(name))}</div>`;
  const st = parseStyle(style ?? styleOf(seed));
  return st.d && !cls.includes("xs") ? `<span class="avd" style="--da:${PAL[st.a]};--db:${PAL[st.b]}">${a}<span class="deco" aria-hidden="true">${decoHTML(st.d)}</span></span>` : a;
}

/* ---------------------------- profile effects ---------------------------- */

// A profile style is "font.effect.hue.hue.deco.frame.fx", seven small numbers
// that travel inside the signed profile card (core SetStyle). Unknown values
// render as nothing, so an older client and a newer preset never break.
const PAL = ["oklch(.74 .15 25)", "oklch(.8 .14 70)", "oklch(.86 .15 100)", "oklch(.76 .15 150)", "oklch(.78 .11 195)",
  "oklch(.72 .13 240)", "oklch(.7 .15 290)", "oklch(.74 .15 340)", "oklch(.97 0 0)", "oklch(.82 .12 85)",
  "oklch(.6 .21 22)", "oklch(.55 .12 200)", "oklch(.58 .2 300)", "oklch(.3 .02 260)"];
// Avatar decorations, anime themed and deliberately lopsided like a sticker
// stuck on a photo. Each sprite is [file, left%, top%, width%, tilt, motion,
// delay] in the avatar's own box, so it scales with every avatar size.
// Art: Microsoft Fluent Emoji 3D (MIT), see fx/LICENSE.txt.
const DECO = [
  ["Brak", []],
  ["Obwódka", [], true],
  ["Hanami", [["cherry_blossom", -26, -24, 60, -18, "sway", 0], ["cherry_blossom", 24, -30, 32, 24, "sway", -1.2], ["cherry_blossom", -30, 26, 26, 40, "sway", -2.1],
    ["petal.svg", 86, 6, 15, 30, "fall", 0], ["petal.svg", 96, 36, 11, -20, "fall", -2.4], ["petal.svg", 70, 70, 10, 60, "fall", -1.1]], 225],
  ["Neko", [["cat_face", 54, 52, 60, 14, "bob", 0], ["ribbon", -24, -22, 54, -28, "sway", -.5], ["bell", -6, 78, 28, -8, "swing", -1]], 45],
  ["Kitsune", [["fox", 50, -28, 64, 16, "bob", 0], ["red_paper_lantern", -28, 28, 36, 0, "swing", -.8], ["sparkles", 82, 68, 26, 0, "tw", -1.4], ["cherry_blossom", 40, -24, 22, 0, "sway", -.4]], 315],
  ["Oni", [["ogre", -28, -26, 66, -22, "", 0], ["fire", 80, -12, 32, 10, "flick", 0], ["fire", 94, 16, 20, 25, "flick", -.3], ["fire", -22, 74, 22, -30, "flick", -.15]], 225],
  ["Tengu", [["goblin", 48, -28, 64, 18, "", 0], ["maple_leaf", -26, 62, 36, -30, "sway", -.6], ["fallen_leaf", -8, 88, 24, 20, "sway", -1.6], ["maple_leaf", 90, 56, 18, 40, "sway", -1]], 315],
  ["Mahō shōjo", [["magic_wand", 56, -30, 58, 0, "sway", 0], ["sparkling_heart", -24, -18, 42, -14, "pulse", 0], ["glowing_star", -18, 74, 28, 10, "tw", -.7], ["sparkles", 90, 58, 24, 0, "tw", -1.5], ["ribbon", 26, -26, 24, 12, "sway", -.9]], 270],
  ["Tsukimi", [["full_moon", 46, -32, 68, 0, "glow", 0], ["rabbit_face", -26, 54, 52, -12, "bob", -.4], ["sparkles", -10, -14, 20, 0, "tw", -1]], 315],
  ["Ramen", [["steaming_bowl", -28, 50, 62, -8, "bob", 0], ["fish_cake_with_swirl", 70, -18, 36, 0, "spin", 0], ["fish_cake_with_swirl", 92, 16, 20, 0, "spin", -3]], 135],
  ["Dango", [["dango", 48, -28, 64, 24, "sway", 0], ["teacup_without_handle", -26, 64, 42, 0, "", 0], ["cherry_blossom", -16, -16, 24, 0, "sway", -1.3]], 315],
  ["Matsuri", [["carp_streamer", 54, -36, 64, 10, "sway", 0], ["red_paper_lantern", -26, -14, 38, 0, "swing", -.5], ["red_paper_lantern", -14, 22, 26, 0, "swing", -1.1], ["fire", 88, 70, 20, 0, "flick", 0]], 270],
  ["Natsu", [["wind_chime", -18, -34, 44, 0, "swing", 0], ["water_wave", 54, 56, 56, 0, "bob", -.6], ["water_wave", 86, 76, 28, 0, "bob", -1.6]], 45],
  ["Shinobi", [["ninja", 52, 46, 64, 10, "bob", 0], ["dizzy", -24, -22, 40, 0, "", 0], ["collision", 88, 8, 22, 0, "pop", -.5]], 45],
  ["Yōkai", [["ghost", 58, -24, 56, 12, "drift", 0], ["red_paper_lantern", -26, 54, 38, 0, "swing", -.7], ["crescent_moon", -20, -18, 32, -10, "", 0], ["ghost", 92, 50, 20, -12, "drift", -2]], 315],
  ["Ryū", [["dragon", 44, -24, 78, 6, "bob", 0], ["sparkles", -20, 76, 26, 0, "tw", 0], ["fire", -20, -10, 22, -20, "flick", 0]], 315],
  ["Doki doki", [["two_hearts", 60, -24, 46, 10, "rise", 0], ["two_hearts", 80, -4, 28, -10, "rise", -1.6], ["sparkling_heart", -20, 64, 38, -12, "pulse", -.3], ["sparkling_heart", -12, -12, 22, 12, "pulse", -.7]], 315],
  ["Wkurzony", [["anger_symbol", 62, -16, 46, 0, "pulse", 0], ["collision", -20, 72, 26, -10, "pop", -.4]], 315],
  ["Speszony", [["sweat_droplets", 66, -18, 42, 0, "drip", 0], ["sweat_droplets", -16, 10, 22, -40, "drip", -1.1]], 315],
  ["Śpiący", [["zzz", 58, -30, 46, 0, "rise", 0], ["zzz", 76, -12, 30, 0, "rise", -1.5], ["crescent_moon", -22, 66, 32, -20, "", 0]], 315],
  ["Zaskoczony", [["red_exclamation_mark", 62, -28, 40, 14, "pop", 0], ["red_question_mark", 84, -10, 30, 24, "pop", -.25]], 315],
  ["Lofi", [["headphone", -14, -32, 128, 0, "", 0], ["musical_notes", 80, -14, 34, 0, "rise", 0], ["crescent_moon", -24, 70, 30, -10, "", 0]]],
  ["Chō", [["butterfly", 60, -22, 46, 18, "flap", 0], ["cherry_blossom", -24, 64, 36, 0, "sway", -1], ["butterfly", -16, -8, 22, -20, "flap", -.3]], 315],
  ["Hoshi", [["glowing_star", -24, -24, 46, -12, "tw", 0], ["sparkles", 74, 66, 32, 0, "tw", -.8], ["dizzy", 72, -22, 38, 0, "", 0]], 225],
];
const decoSrc = (f) => `/fx/${f.includes(".") ? f : f + ".png"}`;
function decoHTML(d) {
  const [, sprites, ring] = DECO[d];
  // ring: true is a full double ring, a number is an arc whose gap is
  // centred at that angle (clockwise from 3 o'clock), under the sprites.
  const arc = (r, w, c, dash) => `<circle cx="50" cy="50" r="${r}" fill="none" stroke="var(${c})" stroke-width="${w}" stroke-linecap="round"${dash ? ` stroke-dasharray="${dash}" transform="rotate(${ring + 39} 50 50)"` : ""}/>`;
  return (ring === undefined ? "" : `<svg class="ring" viewBox="-12 -12 124 124">${ring === true
    ? arc(53.5, 4, "--da") + arc(58.5, 1.5, "--db")
    : arc(53.5, 3.5, "--da", "265 75") + arc(58.5, 1.2, "--db", "120 248")}</svg>`)
    + sprites.map(([f, x, y, w, r, m, dl]) =>
      `<img src="${decoSrc(f)}" alt="" draggable="false" class="${m ? "m-" + m : ""}" style="left:calc(${x}% + ${w / 2}% * (1 - var(--ds, 1)));top:calc(${y}% + ${w / 2}% * (1 - var(--ds, 1)));width:calc(${w}% * var(--ds, 1));rotate:${r}deg;animation-delay:${dl}s">`).join("");
}
// Profile card effects: tsParticles (MIT) on a canvas behind the frame.
// [name, count per 300x260, images, size, direction, speed, options]
const CARDFX = [
  ["Brak"],
  ["Hanami", 18, ["petal.svg", "petal.svg", "petal.svg", "cherry_blossom"], [4, 9], "bottom-right", [.6, 1.5], { wobble: 1, roll: 1, spin: 1 }],
  ["Momiji", 11, ["maple_leaf", "maple_leaf", "fallen_leaf"], [6, 11], "bottom", [.5, 1.2], { wobble: 1, roll: 1, spin: 1 }],
  ["Yuki", 34, ["snowflake"], [2.5, 6], "bottom", [.3, .9], { wobble: 1, spin: 1, dots: 1 }],
  ["Hoshi", 14, ["sparkles", "glowing_star"], [4, 9], "none", [.1, .3], { blink: 1 }],
  ["Hotaru", 16, ["glow.svg"], [4, 10], "none", [.3, .8], { blink: 1 }],
  ["Tōrō", 7, ["red_paper_lantern"], [8, 13], "top", [.25, .55], { wobble: 1 }],
  ["Doki doki", 10, ["sparkling_heart", "two_hearts"], [5, 10], "top", [.4, 1], { wobble: 1 }],
];
const FX = {
  f: ["Zwykła", "Mono", "Szeryf", "Kapitaliki", "Wąska"],
  e: ["Bez efektu", "Kolor", "Gradient", "Neon", "Komiks", "Połysk"],
  d: DECO.map((x) => x[0]),
  r: ["Brak", "Linia", "Podwójna", "Gradient", "Narożniki"],
  x: CARDFX.map((x) => x[0]),
};
function parseStyle(s) {
  const [f, e, a, b, d, r, x] = String(s || "").split(".").map((n) => +n || 0);
  const ok = (v, list) => (v >= 0 && v < list.length ? v : 0);
  const c = (v) => (v >= 0 && v < PAL.length ? v : 0);
  return { f: ok(f, FX.f), e: ok(e, FX.e), a: c(a), b: c(b), d: ok(d, FX.d), r: ok(r, FX.r), x: ok(x, FX.x) };
}
const styleStr = (st) => [st.f, st.e, st.a, st.b, st.d, st.r, st.x].join(".");
// styleOf: what we know about someone's effects from the last snapshot.
function styleOf(pub) {
  if (!pub || !S.snap) return "";
  if (pub === S.snap.me.pub) return S.draftStyle ?? S.snap.me.style ?? "";
  return S.conv?.members.find((m) => m.pub === pub)?.style
    ?? S.snap.friends.find((f) => f.pub === pub)?.style ?? "";
}
// nameFx: class and colors for a display name.
function nameFx(pub, style) {
  const st = parseStyle(style ?? styleOf(pub));
  if (!st.f && !st.e) return "";
  return ` data-nf="${st.f}" data-ne="${st.e}" style="--na:${PAL[st.a]};--nb:${PAL[st.b]}"`;
}
// cardFx: frame and animated effect of a profile card.
function cardFx(style) {
  const st = parseStyle(style);
  return `data-fr="${st.r}" data-fx="${st.x}" style="--fa:${PAL[st.a]};--fb:${PAL[st.b]}"`;
}

// mountFx starts the particle canvas of every card on screen and drops the
// ones whose card left the page. A MutationObserver calls it, so no render
// path has to remember it.
const fxLive = new Map();
const calm = matchMedia("(prefers-reduced-motion: reduce)");
function fxOptions(x, w, h) {
  const [, n, imgs, size, dir, speed, o] = CARDFX[x];
  const image = imgs.map((f) => ({ src: decoSrc(f), width: 128, height: 128 }));
  const k = Math.min(1, (w * h) / (300 * 260)), z = Math.max(.6, Math.sqrt(k));
  const range = (v, f = 1) => ({ min: v[0] * f, max: v[1] * f });
  return {
    fullScreen: { enable: false }, fpsLimit: 60, detectRetina: true, pauseOnOutsideViewport: true,
    particles: {
      number: { value: Math.max(6, Math.round(n * k)) },
      shape: { type: o.dots ? ["image", "circle", "circle"] : "image", options: { image } },
      color: { value: "#fff" },
      size: { value: range(size, z) },
      ...(o.dots && { shadow: { enable: true, color: "#4a6f99", blur: 3 } }),
      opacity: o.blink ? { value: { min: .15, max: 1 }, animation: { enable: true, speed: .8, sync: false } } : { value: { min: .7, max: 1 } },
      move: { enable: true, direction: dir, random: dir === "none", straight: false, speed: range(speed), outModes: { default: "out" } },
      ...(o.spin && { rotate: { value: { min: 0, max: 360 }, direction: "random", animation: { enable: true, speed: { min: 3, max: 12 } } } }),
      ...(o.wobble && { wobble: { enable: true, distance: { min: 3, max: 10 }, speed: { min: -6, max: 6 } } }),
      ...(o.roll && { roll: { enable: true, speed: { min: 3, max: 10 }, darken: { enable: true, value: 10 } } }),
    },
  };
}
let fxReady;
async function mountFx() {
  for (const [el, c] of fxLive) if (!el.isConnected) { c?.destroy(); fxLive.delete(el); }
  if (!window.tsParticles || !document.querySelector(".pcard[data-fx]:not([data-fx='0'])")) return;
  await (fxReady ??= window.loadFull ? loadFull(tsParticles) : Promise.resolve());
  for (const card of document.querySelectorAll(".pcard[data-fx]")) {
    const x = +card.dataset.fx;
    if (!CARDFX[x]?.[1] || card.querySelector(":scope > .fxl")) continue;
    const el = document.createElement("div");
    el.className = "fxl";
    card.prepend(el);
    fxLive.set(el, null);
    tsParticles.load({ id: "fx" + Math.random().toString(36).slice(2), element: el, options: fxOptions(x, card.offsetWidth || 300, card.offsetHeight || 260) })
      .then((c) => { if (!el.isConnected) return c?.destroy(); fxLive.set(el, c); if (calm.matches) setTimeout(() => c?.pause(), 60); });
  }
}
let fxQueued = false;
new MutationObserver(() => { if (!fxQueued) { fxQueued = true; requestAnimationFrame(() => { fxQueued = false; mountFx(); }); } })
  .observe(document.body, { childList: true, subtree: true });

function presenceAvatar(url, name, seed, online, cls = "") {
  return `<div class="av-wrap ${online ? "online" : ""}">${avatar(url, name, seed, cls)}<i class="presence"></i></div>`;
}

// appear: new content settles in instead of snapping. With a key, only a real
// change of view animates (another chat, tab, section); a background repaint
// of the same view stays still.
function appear(el, key) {
  if (!el) return;
  if (key !== undefined) { if (el.dataset.vk === key) return; el.dataset.vk = key; }
  if (REDUCED_Q.matches) return;
  el.animate([{ opacity: 0, transform: "translateY(4px)" }, { opacity: 1, transform: "none" }],
    { duration: 220, easing: "cubic-bezier(.23,1,.32,1)" });
}

const PL_MONTHS = ["sty", "lut", "mar", "kwi", "maj", "cze", "lip", "sie", "wrz", "paź", "lis", "gru"];
const PL_DAYS = ["nd", "pn", "wt", "śr", "cz", "pt", "sb"];

const hhmm = (ts) => new Date(ts * 1000).toLocaleTimeString("pl-PL", { hour: "2-digit", minute: "2-digit" });

function shortTime(ts) {
  if (!ts) return "";
  const d = new Date(ts * 1000), now = new Date();
  if (d.toDateString() === now.toDateString()) return hhmm(ts);
  if (now - d < 7 * 864e5) return PL_DAYS[d.getDay()];
  return `${String(d.getDate()).padStart(2, "0")}.${String(d.getMonth() + 1).padStart(2, "0")}`;
}

function dayLabel(ts) {
  const d = new Date(ts * 1000), now = new Date();
  const midnight = (x) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diff = midnight(now) - midnight(d);
  if (diff === 0) return "Dziś";
  if (diff === 864e5) return "Wczoraj";
  return `${d.getDate()} ${PL_MONTHS[d.getMonth()]} ${d.getFullYear()}`;
}

// Polish counts: 1 osoba, 2-4 osoby, 5+ osób (12-14 too)
function plural(n, one, few, many) {
  if (n === 1) return `${n} ${one}`;
  const d = n % 10, t = n % 100;
  return `${n} ${d >= 2 && d <= 4 && (t < 12 || t > 14) ? few : many}`;
}
const people = (n) => plural(n, "osoba", "osoby", "osób");

function humanSize(n) {
  if (n >= 1 << 30) return (n / (1 << 30)).toFixed(1) + " GB";
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + " MB";
  if (n >= 1 << 10) return Math.round(n / (1 << 10)) + " kB";
  return n + " B";
}

const errText = (e) => (e && (e.message || e.value || e.toString())) || "coś poszło nie tak";

/* ============================ icons ============================ */

const I = {
  crown: '<path d="M3 8l4.5 4L12 5l4.5 7L21 8l-2 11H5Z"/>',
  chat: '<path d="M20 12a8 8 0 0 1-11.6 7.1L4 20l.9-4.4A8 8 0 1 1 20 12Z"/>',
  people: '<path d="M16 19v-1.5a3.5 3.5 0 0 0-3.5-3.5h-5A3.5 3.5 0 0 0 4 17.5V19"/><circle cx="10" cy="8" r="3.2"/><path d="M20 19v-1.5a3.5 3.5 0 0 0-2.6-3.4M15.5 5.2a3.2 3.2 0 0 1 0 5.6"/>',
  user: '<circle cx="12" cy="8" r="3.6"/><path d="M5 20a7 7 0 0 1 14 0"/>',
  gear: '<circle cx="12" cy="12" r="3"/><path d="M19.4 14.5a1.6 1.6 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.6 1.6 0 0 0-2.7 1.1V21a2 2 0 1 1-4 0v-.1a1.6 1.6 0 0 0-2.7-1.2l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.6 1.6 0 0 0-1.1-2.7H3a2 2 0 1 1 0-4h.1a1.6 1.6 0 0 0 1.2-2.7l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.6 1.6 0 0 0 2.7-1.1V3a2 2 0 1 1 4 0v.1a1.6 1.6 0 0 0 2.7 1.2l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.6 1.6 0 0 0 1.1 2.7H21a2 2 0 1 1 0 4h-.1a1.6 1.6 0 0 0-1.5 1.1Z"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  search: '<circle cx="11" cy="11" r="6.5"/><path d="m20 20-3.6-3.6"/>',
  send: '<path d="M4.5 12 20 4.5 15 20l-3.4-5.6L4.5 12Z"/>',
  clip: '<path d="M20 11.5 12.3 19a4.6 4.6 0 0 1-6.5-6.5l8-8a3 3 0 1 1 4.3 4.3l-8 8a1.5 1.5 0 0 1-2.1-2.1l7.3-7.3"/>',
  phone: '<path d="M6.5 3.5 9 4l1 3.5-2 1.5a11 11 0 0 0 5.5 5.5l1.5-2 3.5 1 .5 2.5c0 1.1-.9 2-2 2A14.5 14.5 0 0 1 4.5 5.5c0-1.1.9-2 2-2Z"/>',
  hangup: '<path d="M3.4 14.6a1.7 1.7 0 0 1 0-2.4c4.7-4.6 12.5-4.6 17.2 0a1.7 1.7 0 0 1 0 2.4l-1.5 1.5a1.4 1.4 0 0 1-1.8.2l-2-1.3a1.4 1.4 0 0 1-.6-1.2v-1.7a10.5 10.5 0 0 0-5.4 0v1.7c0 .5-.2.9-.6 1.2l-2 1.3a1.4 1.4 0 0 1-1.8-.2Z"/>',
  reply: '<path d="M9 15 4 10l5-5"/><path d="M4 10h10a6 6 0 0 1 6 6v3"/>',
  pin: '<path d="M9 4h6l-1 6 3 3v1H7v-1l3-3-1-6Z"/><path d="M12 14v6"/>',
  close: '<path d="M6 6l12 12M18 6 6 18"/>',
  mic: '<rect x="9" y="3" width="6" height="10" rx="3"/><path d="M5.5 11.5a6.5 6.5 0 0 0 13 0M12 18v3"/>',
  micOff: '<path d="M9 5a3 3 0 0 1 6 0v5m-6 .5V11a3 3 0 0 0 4.4 2.7"/><path d="M5.5 11.5a6.5 6.5 0 0 0 9.7 5.6M18.5 11.5c0 .5 0 1-.2 1.5M12 18v3"/><path d="m4 4 16 16"/>',
  cam: '<rect x="3" y="6.5" width="12" height="11" rx="2.5"/><path d="m15 11 5.5-3v8L15 13"/>',
  screen: '<rect x="3" y="4.5" width="18" height="12" rx="2"/><path d="M9 20h6M12 16.5V20"/>',
  volume: '<path d="M11 5 6.5 8.5H3v7h3.5L11 19V5Z"/><path d="M15 9.5a3.5 3.5 0 0 1 0 5M17.5 7a7 7 0 0 1 0 10"/>',
  deaf: '<path d="M11 5 6.5 8.5H3v7h3.5L11 19V5Z"/><path d="m15.5 9.5 5 5m0-5-5 5"/>',
  down: '<path d="M12 5v14m0 0-6-6m6 6 6-6"/>',
  x: '<path d="M6 6l12 12M18 6 6 18"/>',
  star: '<path d="m12 4 2.4 5 5.4.6-4 3.7 1.1 5.3L12 16l-4.9 2.6 1.1-5.3-4-3.7 5.4-.6L12 4Z"/>',
  heart: '<path d="M12 20s-7.5-4.6-7.5-10.2A4.3 4.3 0 0 1 12 7.4a4.3 4.3 0 0 1 7.5 2.4C19.5 15.4 12 20 12 20Z"/>',
  chev: '<path d="m9 6 6 6-6 6"/>',
  copy: '<rect x="9" y="9" width="11" height="11" rx="2"/><path d="M15 9V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h3"/>',
  trash: '<path d="M4 7h16M9 7V5h6v2M6 7l1 13h10l1-13"/>',
  edit: '<path d="M4 20h4l10-10a2.8 2.8 0 1 0-4-4L4 16v4Z"/>',
  key: '<circle cx="8" cy="12" r="4"/><path d="M12 12h9m-3 0v3.5m-2.5-3.5v2.5"/>',
  file: '<path d="M13 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V9l-6-6Z"/><path d="M13 3v6h6"/>',
  save: '<path d="M12 4v10m0 0 4-4m-4 4-4-4"/><path d="M5 17v2a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-2"/>',
  smile: '<circle cx="12" cy="12" r="8.5"/><path d="M9 14.5a4 4 0 0 0 6 0"/><path d="M9.5 9.5h.01M14.5 9.5h.01"/>',
  eye: '<path d="M2.5 12S6 6 12 6s9.5 6 9.5 6-3.5 6-9.5 6-9.5-6-9.5-6Z"/><circle cx="12" cy="12" r="2.8"/>',
  shield: '<path d="M12 3 5 6v6c0 4.4 3 7.9 7 9 4-1.1 7-4.6 7-9V6l-7-3Z"/>',
  server: '<rect x="3" y="4" width="18" height="7" rx="2"/><rect x="3" y="13" width="18" height="7" rx="2"/><path d="M7 7.5h.01M7 16.5h.01"/>',
  bot: '<rect x="4" y="8" width="16" height="11" rx="3"/><path d="M12 4v4M9 13.5h.01M15 13.5h.01"/>',
  qr: '<rect x="4" y="4" width="6" height="6" rx="1"/><rect x="14" y="4" width="6" height="6" rx="1"/><rect x="4" y="14" width="6" height="6" rx="1"/><path d="M14 14h3v3h-3zM20 14v6h-3"/>',
  check: '<path d="m5 12.5 5 5 9-11"/>',
  checks: '<path d="m3 12.5 4.5 4.5L15 7"/><path d="m11 16 1 1 8-10"/>',
  palette: '<path d="M12 21a9 9 0 1 1 9-9c0 2-2 2.5-3.5 2.5H16a2 2 0 0 0-1.4 3.4A2 2 0 0 1 12 21Z"/><path d="M7.5 12h.01M10 8h.01M15 8h.01"/>',
  globe: '<circle cx="12" cy="12" r="8.5"/><path d="M3.5 12h17M12 3.5c4 4.5 4 12.5 0 17-4-4.5-4-12.5 0-17Z"/>',
  hash: '<path d="M5 9h15M4 15h15M10 3 8 21M16 3l-2 18"/>',
  expand: '<path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5"/>',
  logout: '<path d="M14 8V5a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2v-3"/><path d="M10 12h11m0 0-3-3m3 3-3 3"/>',
};

const icon = (name, cls = "") => `<svg class="${cls}" viewBox="0 0 24 24">${I[name]}</svg>`;

/* ============================ state ============================ */

const S = {
  stage: "loading",
  boot: null,
  snap: null,
  conv: null,
  chat: "",
  space: "home", // "home" (direct chats + friends) or a group's chat id
  ch: "",        // open text channel of the group ("" = the default "czat")
  voice: null,   // call key whose voice view is open instead of the chat
  pip: null,     // [x, y] where the floating call window was dragged
  focus: null,   // voice view: key of the tile blown up over the others
  full: false,   // that tile also covers the whole screen
  selfMute: false, selfDeaf: false, // how the next call starts
  fpage: false,  // phone: the friends page is open instead of the sidebar
  ftab: "online",
  showMembers: true,
  collapsed: new Set(),
  newMark: null, // {chat, n, id}: where the "nowe" line goes in the open chat
  screen: "app", // app | settings
  section: "profile",
  settings: null,
  filter: "",
  limit: 60,
  listSig: "",
  msgSig: "",
  builtChat: undefined, // chat whose DOM structure is currently mounted
  version: "dev",
  atBottom: true,
  frames: new Map(), // key -> { seq, kind, name, at }
  hintIdx: 0,
  hints: [],
  busy: false,
};

/* ============================ toast / modal ============================ */

function toast(text, kind = "") {
  const el = document.createElement("div");
  el.className = "toast " + kind;
  el.textContent = text;
  $("#toasts").append(el);
  setTimeout(() => {
    el.classList.add("out");
    setTimeout(() => el.remove(), 180);
  }, kind === "err" ? 4200 : 2400);
}

const fail = (e) => toast(errText(e), "err");

// A rejected call that nobody caught is still a broken button — say so out
// loud instead of leaving the UI silently doing nothing.
window.addEventListener("unhandledrejection", (e) => fail(e.reason));
window.addEventListener("error", (e) => fail(e.message));

// The webview's own menu (reload, inspect element) is the browser leaking
// through; it shows only in developer mode. Our menus prevent it themselves.
document.addEventListener("contextmenu", (e) => { if (!S.snap?.me.dev) e.preventDefault(); });

let modalClose = null;

// Where the last press landed. A sheet that grows out of the control you
// pressed keeps the spatial relationship between trigger and content obvious;
// one that always blooms from the middle of the screen throws it away.
let lastPress = null;
document.addEventListener("pointerdown", (e) => { lastPress = [e.clientX, e.clientY]; }, true);

function anchor(box) {
  if (!lastPress) return;
  const r = box.getBoundingClientRect();
  const pct = (v, size) => Math.max(-15, Math.min(115, (v / size) * 100)).toFixed(1) + "%";
  box.style.setProperty("--ox", pct(lastPress[0] - r.left, r.width));
  box.style.setProperty("--oy", pct(lastPress[1] - r.top, r.height));
}

// modal({title, body, actions, wide, onMount}) -> Promise resolved with the
// value passed to the action that closed it (null when dismissed).
// bare: no title bar and no buttons, the body is the whole sheet (a profile
// card whose frame has to go around all of it); Esc or a click outside closes.
function modal({ title, body: bodyHTML, actions = [], wide = false, bare = false, onMount }) {
  closeModal(null);
  return new Promise((resolve) => {
    const root = $("#modal-root");
    root.innerHTML = `
      <div class="backdrop" data-act="modal-dismiss">
        <div class="modal ${wide ? "wide" : ""} ${bare ? "bare" : ""}" role="dialog" aria-modal="true" ${bare ? `aria-label="${esc(title)}"` : ""}>
          ${bare ? "" : `<header><h3>${esc(title)}</h3>
            <button class="icon-btn" data-act="modal-dismiss">${icon("x")}</button>
          </header>`}
          <div class="body">${bodyHTML}</div>
          ${actions.length ? `<footer>${actions.map((a, i) =>
            `<button class="btn ${a.kind || "ghost"}" data-act="modal-action" data-i="${i}">${esc(a.label)}</button>`).join("")}</footer>` : ""}
        </div>
      </div>`;
    const box = $(".modal", root);
    anchor(box);
    modalClose = (result) => {
      // the old sheet leaves on its own, so a dialog that opens the next one
      // never waits for the exit to finish
      const old = root.firstElementChild;
      if (old) { old.classList.add("out"); document.body.append(old); setTimeout(() => old.remove(), 170); }
      root.innerHTML = ""; modalClose = null; resolve(result);
    };
    root._actions = actions;
    onMount?.(box);
    const first = $("input:not([type=radio]):not([type=checkbox]), textarea, select", box);
    first?.focus();
    first?.select?.();
  });
}

function closeModal(result) { modalClose?.(result); }

// popover: a small floating layer next to the control that opened it. It grows
// out of that control and closes on a click outside, Esc or a picked item.
// Pressing the same control again closes it.
let popClose = null, popAnchor = null;
function popover(anchor, html, side = "bottom", cls = "") {
  if (popAnchor === anchor && popClose) { popClose(); return null; }
  popClose?.();
  const el = document.createElement("div");
  el.className = "pop " + cls;
  el.innerHTML = html;
  document.body.append(el);
  const a = anchor.getBoundingClientRect(), r = el.getBoundingClientRect();
  let x = a.left, y = a.bottom + 6;
  if (side === "right") { x = a.right + 8; y = a.top; }
  if (side === "left") { x = a.left - r.width - 8; y = a.top; }
  if (side === "top") { x = a.right - r.width; y = a.top - r.height - 8; }
  x = Math.max(8, Math.min(innerWidth - r.width - 8, x));
  y = Math.max(8, Math.min(innerHeight - r.height - 8, y));
  Object.assign(el.style, { left: x + "px", top: y + "px",
    transformOrigin: `${a.left + a.width / 2 - x}px ${a.top + a.height / 2 - y}px` });
  const off = (e) => { if (!el.contains(e.target) && !anchor.contains(e.target)) close(); };
  const close = () => {
    el.classList.add("out");
    setTimeout(() => el.remove(), 110);
    document.removeEventListener("pointerdown", off, true);
    popClose = popAnchor = null;
  };
  document.addEventListener("pointerdown", off, true);
  popClose = close; popAnchor = anchor;
  return el;
}

// Rail tooltips: the rail scrolls, so a CSS ::after would be clipped by it.
const tip = document.createElement("div");
tip.className = "tip";
document.body.append(tip);
document.addEventListener("pointerover", (e) => {
  const el = e.target.closest?.("[data-tip]");
  // a click repaints the rail under a still pointer: that is not a new hover
  const still = lastPress && e.clientX === lastPress[0] && e.clientY === lastPress[1];
  if (!el || still) return tip.classList.remove("on");
  const r = el.getBoundingClientRect();
  tip.textContent = el.dataset.tip;
  tip.style.left = r.right + 14 + "px";
  tip.style.top = r.top + r.height / 2 + "px";
  tip.classList.add("on");
});
document.addEventListener("pointerdown", () => tip.classList.remove("on"));

function confirmBox(title, text, danger = true) {
  return modal({
    title,
    body: `<p>${esc(text)}</p>`,
    actions: [
      { label: "Anuluj", kind: "ghost", value: false },
      { label: "Tak", kind: danger ? "danger" : "primary", value: true },
    ],
  });
}

function promptBox({ title, label, value = "", placeholder = "", hint = "", multiline = false, ok = "Zapisz" }) {
  return modal({
    title,
    body: `<div class="field"><label>${esc(label)}</label>${
      multiline
        ? `<textarea class="input" id="pv" placeholder="${esc(placeholder)}">${esc(value)}</textarea>`
        : `<input class="input" id="pv" value="${esc(value)}" placeholder="${esc(placeholder)}">`
    }${hint ? `<p class="hint-text">${esc(hint)}</p>` : ""}</div>`,
    actions: [
      { label: "Anuluj", kind: "ghost", value: null },
      { label: ok, kind: "primary", get value() { return $("#pv").value; } },
    ],
    onMount: (box) => {
      if (multiline) return;
      $("#pv", box).addEventListener("keydown", (e) => {
        if (e.key === "Enter") { e.preventDefault(); closeModal($("#pv", box).value); }
      });
    },
  });
}

function lightbox(src) {
  const el = document.createElement("div");
  el.className = "lightbox";
  el.innerHTML = `<img src="${esc(src)}" alt="">`;
  el.addEventListener("click", () => closeLightbox());
  document.body.append(el);
}

function closeLightbox() {
  const el = $(".lightbox");
  if (!el) return;
  el.classList.add("out");
  setTimeout(() => el.remove(), 170);
}

/* ============================ gate (login) ============================ */

// the app icon's mark: three voice bars
const LOGO = `<svg viewBox="0 0 24 24"><rect x="7" y="9.4" width="1.9" height="5.2" rx=".95"/><rect x="11.05" y="7" width="1.9" height="10" rx=".95"/><rect x="15.1" y="9.4" width="1.9" height="5.2" rx=".95"/></svg>`;

function profileSwitch(b) {
  if (b.pinned) return "";
  const opts = b.profiles.map((p) =>
    `<option value="${esc(p)}" ${p === b.profile ? "selected" : ""}>${esc(p || "domyślne")}</option>`).join("");
  return `<div class="profile-switch">
      <div class="select-wrap"><select class="input" id="gate-profile">${opts}</select></div>
      <button class="btn ghost" data-act="new-profile">${icon("plus")}Nowe konto</button>
    </div>`;
}

function renderGate() {
  const b = S.boot;
  const setup = b.stage === "setup";
  const title = setup
    ? (b.profile ? `Nowe konto „${esc(b.profile)}”` : "Pierwsze uruchomienie")
    : "Klucz tożsamości jest zaszyfrowany";

  view.innerHTML = `
    <div class="gate">
      <div class="gate-card">
        <div class="gate-logo">${LOGO}<h1>szept</h1>
          <p>Szyfrowane rozmowy prosto między urządzeniami</p></div>
        <div class="panel">
          <h3>${title}</h3>
          ${setup ? `
            <div class="field"><label>Nazwa</label>
              <input class="input" id="g-name" value="${esc(b.profile)}" placeholder="np. ala"></div>
            <div class="field"><label>Connector</label>
              <input class="input" id="g-conn" value="${esc(b.connector)}" placeholder="ws://twoj-vps:7777"></div>
            <div class="field"><label>Hasło klucza <span style="color:var(--muted);font-weight:400">(zalecane)</span></label>
              <input class="input" id="g-pass" type="password" placeholder="•••••"></div>
            <p class="hint-text" style="padding:0 16px">Connector tylko kojarzy urządzenia. Treści rozmów nie widzi.</p>
            <p class="hint-text" style="padding:0 16px">Hasło szyfruje klucz i historię rozmów na tym urządzeniu. Możesz je ustawić także później w ustawieniach.</p>
          ` : `
            <div class="field"><label>Hasło</label>
              <input class="input" id="g-pass" type="password" placeholder="hasło klucza" autofocus></div>
          `}
          <p class="hint-text err" id="g-err" hidden></p>
          <div class="btn-row">
            <button class="btn primary wide" data-act="${setup ? "do-setup" : "do-unlock"}">
              ${setup ? "Zaczynamy" : "Odblokuj"}
            </button>
          </div>
        </div>
        ${profileSwitch(b)}
      </div>
    </div>`;

  const submit = () => $(`[data-act="${setup ? "do-setup" : "do-unlock"}"]`).click();
  $$("#view input").forEach((i) =>
    i.addEventListener("keydown", (e) => { if (e.key === "Enter") submit(); }));
  $("#gate-profile")?.addEventListener("change", async (e) => {
    try { await api("UseProfile", e.target.value); await boot(); } catch (err) { fail(err); }
  });
  $("#g-name, #g-pass")?.focus();
}

function gateError(msg) {
  const el = $("#g-err");
  if (!el) return toast(msg, "err");
  el.textContent = msg;
  el.hidden = false;
}

/* ============================ shell ============================ */

const SKELETON = `<div class="skel">${"<i></i>".repeat(7)}</div>`;

function renderShell() {
  view.innerHTML = `
    <div class="shell" id="shell">
      <nav class="rail" id="rail"></nav>
      <div class="side">
        <section class="list" id="list">${SKELETON}</section>
      </div>
      <div class="dock">
        <div class="voice" id="voice"><div class="voice-in" id="voice-in"></div></div>
        <div class="userbar" id="userbar"></div>
      </div>
      <section class="conv" id="conv"></section>
      <aside class="members" id="members"></aside>
    </div>`;
}

const count = (n) => (n > 99 ? "99+" : n);

// The rail: home (direct chats and friends), then every group as a server
// tile. The pill on the left edge says unread (short), hover (mid) or open (tall).
function paintRail() {
  const rail = $("#rail");
  const groups = S.snap.chats.filter((c) => !c.direct);
  const dmUnread = S.snap.chats.reduce((n, c) => n + (c.direct ? c.unread : 0), 0);
  const sig = JSON.stringify([S.space, dmUnread, groups.map((g) => [g.id, g.name, g.avatar, g.style, g.unread, g.inCall.length, !!g.call])]);
  if (rail.dataset.sig === sig) return;
  rail.dataset.sig = sig;
  const pip = (n) => (n ? `<span class="pip">${count(n)}</span>` : "");
  rail.innerHTML = `
    <button class="rail-btn home ${S.space === "home" ? "active" : ""} ${dmUnread ? "unread" : ""}" data-act="home" data-tip="Wiadomości prywatne">${LOGO}${pip(dmUnread)}</button>
    <i class="rail-sep"></i>
    ${groups.map((g) => `<button class="rail-btn ${S.space === g.id ? "active" : ""} ${g.unread ? "unread" : ""} ${parseStyle(g.style).d ? "decorated" : ""}" data-act="space" data-chat="${g.id}" data-tip="${esc(g.name)}">
        ${g.avatar ? `<img src="${esc(g.avatar)}" alt="">` : `<span class="ini">${esc(initials(g.name))}</span>`}${serverDeco(g.style)}${pip(g.unread)}
        ${g.inCall.length || g.call ? `<span class="live">${icon("volume")}</span>` : ""}</button>`).join("")}
    <button class="rail-btn add" data-act="new-group" data-tip="Nowa grupa">${icon("plus")}</button>
    <button class="rail-btn add" data-act="join" data-tip="Wejdź z zaproszenia">${icon("key")}</button>`;
}

// serverDeco: a group's decoration, the same sprites as an avatar's.
function serverDeco(style) {
  const st = parseStyle(style);
  return st.d ? `<span class="deco" aria-hidden="true" style="--da:${PAL[st.a]};--db:${PAL[st.b]}">${decoHTML(st.d)}</span>` : "";
}

/* ---------------------------- sidebar ---------------------------- */

function chatRow(c) {
  const on = c.online > 0;
  return `<button class="row ${c.id === S.chat ? "active" : ""} ${c.unread ? "unread" : ""}" data-act="open" data-chat="${c.id}">
      ${presenceAvatar(c.avatar, c.name, c.peer || c.id, on)}
      <div class="row-body">
        <div class="row-name">${esc(c.name)}</div>
        <div class="row-sub">${esc(c.inCall.length ? "w rozmowie" : c.preview || (on ? "online" : "offline"))}</div>
      </div>
      ${c.unread ? `<span class="badge">${count(c.unread)}</span>` : ""}
    </button>`;
}

function homeSide() {
  const q = S.filter.toLowerCase();
  const dms = S.snap.chats.filter((c) => c.direct && (!q || c.name.toLowerCase().includes(q)));
  const online = S.snap.friends.filter((f) => f.online).length;
  return `
    <div class="pane-head"><div class="search">${icon("search")}
      <input id="q" placeholder="Znajdź rozmowę" value="${esc(S.filter)}"></div></div>
    <div class="scroll"><div class="rows">
      <button class="row nav ${!S.chat ? "active" : ""}" data-act="friends">
        <span class="nav-ico">${icon("people")}</span><span class="row-name">Znajomi</span>
        ${online ? `<span class="badge soft">${online}</span>` : ""}</button>
      <div class="cat-head"><span>Wiadomości prywatne</span>
        <button class="icon-btn" data-act="add-friend" title="Dodaj znajomego">${icon("plus")}</button></div>
      ${dms.map(chatRow).join("") || `<p class="side-empty">${q ? "Nic nie pasuje." : "Napisz do znajomego, a rozmowa pojawi się tutaj."}</p>`}
    </div></div>`;
}

// chanOf: a channel of group g. Both defaults have the ID "", so the kind
// tells them apart.
const chanOf = (g, id, kind) => g?.channels.find((x) => x.id === id && (id || x.kind === kind));

function chanName(g, id, kind = "text") {
  return chanOf(g, id, kind)?.name || (id ? "?" : kind === "voice" ? "Rozmowa" : "czat");
}

// callFlags: what a call member says about themselves (letters from core's
// Call.State: m mic off, d deafened, c camera, s screen) as small icons.
function callFlags(f = "") {
  const ic = (name, t, cls = "") => `<i class="${cls}" title="${t}">${icon(name)}</i>`;
  const out = (f.includes("m") ? ic("micOff", "Wyciszony mikrofon", "off") : "")
    + (f.includes("d") ? ic("deaf", "Wyciszone dźwięki", "off") : "")
    + (f.includes("c") ? ic("cam", "Kamera włączona") : "")
    + (f.includes("s") ? `<b class="live" title="Udostępnia ekran">Na żywo</b>` : "");
  return out && `<span class="vflags">${out}</span>`;
}
const myFlags = (k) => k ? (k.mic ? "" : "m") + (k.deaf ? "d" : "") + (k.cam ? "c" : "") + (k.screen ? "s" : "") : "";

function groupSide(g) {
  // the call list has names only; the open chat's member list adds faces
  const members = S.conv?.id === g.id ? S.conv.members : [];
  const me = S.snap.me.name;
  const who = (name, flags) => {
    const m = members.find((x) => x.name === name) || (name === me ? S.snap.me : null);
    return `<button class="vc-user ${m && peerVol.get(m.pub)?.mute ? "muted" : ""}" ${m ? `data-act="user-menu" data-pub="${m.pub}"` : "disabled"}><span class="spk" data-speak="${m?.pub || ""}">${avatar(m?.avatar, name, m?.pub || name, "xs")}</span><span class="vn">${esc(name)}</span>${callFlags(flags)}</button>`;
  };
  const attrs = (x) => `data-chat="${g.id}" data-ch="${x.id}" data-kind="${x.kind}" data-ctx
    ${g.isOwner && !x.arch ? `draggable="true"` : ""}`;
  const text = (x) => {
    const n = g.chanUnread?.[x.id] || 0;
    return `<button class="row chan ${S.chat === g.id && S.ch === x.id && !S.voice ? "active" : ""} ${n ? "unread" : ""}"
      data-act="chan" ${attrs(x)}>
      ${icon("hash")}<span class="row-name">${esc(x.name)}</span>${n ? `<span class="badge">${count(n)}</span>` : ""}</button>`;
  };
  const voice = (x) => {
    const key = g.id + x.id;
    let live = g.voice[x.id] || [], flags = g.vflags?.[x.id] || [];
    if (g.callKey === key && !live.includes(me)) { live = [me, ...live]; flags = [myFlags(g.call), ...flags]; }
    return `<button class="row chan ${g.callKey === key ? "joined" : ""} ${S.voice === key ? "active" : ""}"
        data-act="voice-chan" ${attrs(x)}>
        ${icon("volume")}<span class="row-name">${esc(x.name)}</span></button>
      ${live.length ? `<div class="vc-list">${live.map((n, i) => who(n, flags[i])).join("")}</div>` : ""}`;
  };
  // archived voice channels are not joinable, archived text ones stay readable
  const row = (x) => x.kind === "text" ? text(x)
    : x.arch ? `<button class="row chan" ${attrs(x)}>${icon("volume")}<span class="row-name">${esc(x.name)}</span></button>` : voice(x);
  const live = g.channels.filter((x) => !x.arch);
  const cats = live.filter((x) => x.kind === "cat");
  // a channel whose category was deleted falls back to the default section
  const home = (x) => !x.cat || !cats.some((c) => c.id === x.cat);
  const cat = (key, label, catId, kind, rows) => `<div class="cat ${S.collapsed.has(key) ? "shut" : ""}" ${catId === null ? "" : `data-cat="${catId}"`} data-kind="${kind}">
      <div class="cat-top"><button class="cat-head" data-act="cat" data-id="${key}">${icon("chev")}<span>${esc(label)}</span></button>
        ${g.isOwner && catId !== null ? `<button class="cat-add" data-act="chan-new" data-cat="${catId}" data-kind="${kind}" title="Utwórz kanał">${icon("plus")}</button>` : ""}</div>
      <div class="cat-body"><div>${rows}</div></div></div>`;
  const chans = live.filter((x) => x.kind !== "cat");
  // the uncategorized sections: always there for the owner to drop into
  const loose = (kind, label) => {
    const rows = chans.filter((x) => x.kind === kind && home(x));
    return rows.length || g.isOwner ? cat(kind, label, "", kind, rows.map(row).join("")) : "";
  };
  const arch = g.isOwner ? g.channels.filter((x) => x.arch && x.kind !== "cat") : [];
  return `
    <button class="pane-head g-head ${g.banner ? "has-banner" : ""}" data-act="g-menu" ${g.banner ? `style="background-image:url('${esc(g.banner)}')"` : ""}><h2>${esc(g.name)}</h2>${icon("chev", "dd")}</button>
    <div class="scroll"><div class="rows">
      ${loose("text", "Kanały tekstowe")}
      ${loose("voice", "Głosowe")}
      ${cats.map((c) => cat(c.id, c.name, c.id, "text", chans.filter((x) => x.cat === c.id).map(row).join(""))).join("")}
      ${arch.length ? cat("arch", "Archiwum", null, "", arch.map(row).join("")) : ""}
    </div></div>`;
}

function paintList() {
  const list = $("#list");
  if (!list) return;
  // the search box is rebuilt with the pane, so put the caret back afterwards
  const old = $("#q");
  const focused = old && document.activeElement === old;
  const caret = old?.selectionStart ?? 0;
  // FLIP: a chat that jumped up the list slides there instead of teleporting
  const before = new Map($$(".row[data-chat]", list).map((r) => [r.dataset.chat, r.getBoundingClientRect().top]));

  const g = S.space !== "home" && S.snap.chats.find((c) => c.id === S.space);
  list.innerHTML = g ? groupSide(g) : homeSide();
  if (list.dataset.vk !== S.space) appear($(".scroll", list), undefined), list.dataset.vk = S.space;

  // home list only: a server's channel rows all share the server's id
  if (!g && !REDUCED_Q.matches) {
    for (const r of $$(".row[data-chat]", list)) {
      const dy = (before.get(r.dataset.chat) ?? 0) - r.getBoundingClientRect().top;
      if (before.has(r.dataset.chat) && dy > 4) {
        r.animate([{ transform: `translateY(${dy}px)` }, { transform: "none" }], { duration: 200, easing: "cubic-bezier(.23,1,.32,1)" });
      }
    }
  }
  const q$ = $("#q", list);
  if (!q$) return;
  q$.addEventListener("input", () => { S.filter = q$.value; S.listSig = null; paintList(); });
  if (focused) { q$.focus(); q$.setSelectionRange(caret, caret); }
}

// The voice panel sits above the account strip while you are in a call. It
// keeps its old content while it folds away, so the collapse has something to show.
function paintVoice() {
  const host = $("#voice");
  if (!host) return;
  const c = S.snap.chats.find((x) => x.call);
  host.classList.toggle("open", !!c);
  if (!c) return;
  const k = c.call, id = `data-chat="${c.callKey}"`;
  $("#voice-in").innerHTML = `
    <div class="vc-top">
      <button class="vc-tx" data-act="voice-open" ${id}><b>Połączono z głosem</b><span>${esc(c.direct ? c.name : `${chanName(c, c.callKey.slice(c.id.length), "voice")} / ${c.name}`)}</span></button>
      <button class="icon-btn hang" data-act="hangup" ${id} title="Rozłącz">${icon("hangup")}</button>
    </div>
    <div class="vc-ctl">
      <button class="${k.cam ? "on" : ""}" data-act="cam" ${id} title="Kamera">${icon("cam")}</button>
      <button class="${k.screen ? "on" : ""}" data-act="screen" ${id} title="Ekran">${icon("screen")}</button>
      <button data-act="spatial" ${id} title="Przestrzeń dźwięku">${icon("people")}</button>
    </div>`;
}

/* ---------------------------- identity key ---------------------------- */

// The public key, typed out like a carbon copy. Two people read it to each
// other over another channel; if it matches, no connector sits in between.
const keyGroups = (pub) => (String(pub || "").toLowerCase().match(/[0-9a-f]{4}/g) || []);
const keyShort = (pub) => keyGroups(pub).slice(0, 2).join(" ");
function keyBlock(pub) {
  const g = keyGroups(pub);
  if (!g.length) return "";
  // each group is typed in turn, like a carbon copy coming off the roller
  const span = (x, i) => `<span style="--i:${i}">${esc(x)}</span>`;
  const lines = [];
  for (let i = 0; i < g.length; i += 4) lines.push(g.slice(i, i + 4).map((x, j) => span(x, i + j)).join(" "));
  return `<div class="key-block" title="Porównaj z drugą osobą innym kanałem">
      <div class="key-head">${icon("key")}<span>Klucz</span></div>
      <code>${lines.join("\n")}</code></div>`;
}

/* ---------------------------- friends page ---------------------------- */

function friendRow(f) {
  return `<div class="friend" data-act="profile" data-pub="${f.pub}">
      ${presenceAvatar(f.avatar, f.name, f.pub, f.online)}
      <div class="row-body">
        <div class="row-name">${esc(f.name)}${f.alias ? ` <span class="alias">„${esc(f.alias)}”</span>` : ""} <span class="key-chip">${keyShort(f.pub)}</span></div>
        <div class="row-sub">${esc(f.status || (f.online ? "online" : "offline"))}</div>
      </div>
      <button class="round-btn" data-act="dm" data-pub="${f.pub}" title="Napisz">${icon("chat")}</button>
      <button class="round-btn" data-act="profile" data-pub="${f.pub}" title="Profil">${icon("user")}</button>
    </div>`;
}

function paintFriends() {
  const host = $("#conv");
  const me = S.snap.me;
  const sig = JSON.stringify(S.ftab === "add" ? ["add", me.code] : [S.ftab, S.snap.friends]);
  if (S.builtChat === "friends" && host.dataset.fsig === sig) return;
  host.dataset.fsig = sig;
  const fr = S.snap.friends;
  const shown = S.ftab === "online" ? fr.filter((f) => f.online) : fr;
  const tabs = [["online", "Online"], ["all", "Wszyscy"]].map(([v, t]) =>
    `<button class="${S.ftab === v ? "sel" : ""}" data-act="ftab" data-v="${v}">${t}</button>`).join("");
  host.innerHTML = `
    <header class="conv-head">
      <button class="icon-btn back" data-act="back" title="Wróć">${icon("chev")}</button>
      <span class="h-ico">${icon("people")}</span><b class="h-t">Znajomi</b><i class="h-div"></i>
      <nav class="tabs">${tabs}<button class="add ${S.ftab === "add" ? "sel" : ""}" data-act="ftab" data-v="add">Dodaj<span class="long"> znajomego</span></button></nav>
    </header>
    <div class="fr-body scroll">${S.ftab === "add" ? `
      <h3>Dodaj znajomego</h3>
      <p class="lead">Wklej kod SZF-…, który dostaniesz od drugiej osoby. U niej to „Twój kod”.</p>
      <div class="add-line"><input class="input" id="fr-code" placeholder="SZF-…" autocomplete="off">
        <button class="btn primary" data-act="fr-add">Dodaj</button></div>
      <h3>Twój kod</h3>
      <div class="code-box">${esc(me.code)}</div>
      ${keyBlock(me.pub)}
      <div class="btn-row"><button class="btn" data-act="copy" data-text="${esc(me.code)}">${icon("copy")}Kopiuj</button>
        <button class="btn ghost" data-act="my-code">${icon("qr")}Pokaż QR</button></div>`
    : shown.length
      ? `<div class="fr-count">${S.ftab === "online" ? "Online" : "Wszyscy znajomi"} · ${shown.length}</div>${shown.map(friendRow).join("")}`
      : `<div class="empty"><i class="empty-ico">${icon("people")}</i><strong>${S.ftab === "online" ? "Nikt nie jest teraz online" : "Nie masz jeszcze znajomych"}</strong>
          <button class="btn primary" data-act="ftab" data-v="add">Dodaj znajomego</button></div>`}
    </div>`;
  if (host.dataset.vk !== "f:" + S.ftab) { host.dataset.vk = "f:" + S.ftab; appear($(".fr-body", host)); }
  $("#fr-code")?.addEventListener("keydown", (e) => { if (e.key === "Enter") $("[data-act=fr-add]").click(); });
}

// "Aktywne teraz": calls going on right now, next to the friends page.
function paintActive() {
  const live = S.snap.chats.filter((c) => c.inCall.length);
  $("#members").innerHTML = `<div class="scroll active-now"><h3>Aktywne teraz</h3>
    ${live.map((c) => `<button class="live-card" data-act="open" data-chat="${c.id}">
        <b>${esc(c.name)}</b><span>${esc(c.inCall.join(", "))}</span>
        <span class="vc-tag">${icon("volume")}${people(c.inCall.length)} w rozmowie</span></button>`).join("")
      || `<div class="quiet"><i class="empty-ico">${icon("volume")}</i><b>Na razie cisza</b>Gdy ktoś zacznie rozmowę głosową, zobaczysz ją tutaj.</div>`}</div>`;
}

/* ---------------------------- conversation ---------------------------- */

// Repaint guards. The conversation is rebuilt only when the user switches
// chats; everything else patches the pieces that actually changed, so a sync
// burst never yanks focus out of the composer or drops half-typed text.
function msgSignature(c) {
  if (!c) return "";
  return c.members.map((m) => m.style).join() + "|" + c.msgs.map((m) =>
    [m.id, m.ack, m.deleted, m.pinned, m.replyName, m.have, m.havePrev, m.getting, JSON.stringify(m.reactions || {})].join(":")
  ).join(",");
}

const welcomeHTML = `<div class="empty" style="margin:auto;max-width:420px"><i class="empty-ico">${icon("hash")}</i>
    <strong>Wybierz kanał z lewej</strong>
    Tekst jest w <b># czat</b>, rozmowa głosowa pod nim.
  </div>`;

function ackIcon(ack) {
  if (ack === "read") return `<svg viewBox="0 0 24 24" class="read">${I.checks}</svg>`;
  if (ack === "delivered") return `<svg viewBox="0 0 24 24">${I.checks}</svg>`;
  return `<svg viewBox="0 0 24 24">${I.check}</svg>`;
}

function attachHTML(m) {
  if (m.kind !== "file") return "";
  const img = m.mime?.startsWith("image/") || m.mime?.startsWith("video/");
  if (img && m.src) {
    return `<span class="attach ${m.have ? "" : "pending"}">
        <img src="${esc(m.src)}" alt="${esc(m.file)}" data-act="zoom" data-src="${esc(m.src)}" loading="lazy">
      </span>`;
  }
  const state = m.have ? humanSize(m.size)
    : m.getting ? "pobieranie…"
      : `${humanSize(m.size)} · kliknij, aby pobrać`;
  return `<button class="file" data-act="${m.have ? "save" : "fetch"}" data-blob="${m.blob}"
        data-name="${esc(m.file)}" data-mime="${esc(m.mime)}" data-size="${m.size}" data-author="${m.author}">
      <span class="ico">${icon(m.have ? "save" : "file")}</span>
      <span class="nm"><b>${esc(m.file)}</b><span>${esc(state)}</span></span>
    </button>`;
}

const REACT_SET = ["👍", "❤️", "😂", "🎉", "😮", "😢"];

function reactionsHTML(m, mePub) {
  if (!m.reactions) return "";
  const byEmoji = new Map();
  for (const [pub, emoji] of Object.entries(m.reactions)) {
    if (!byEmoji.has(emoji)) byEmoji.set(emoji, []);
    byEmoji.get(emoji).push(pub);
  }
  const chips = [...byEmoji].map(([emoji, pubs]) =>
    `<button class="reaction ${pubs.includes(mePub) ? "mine" : ""}" data-act="react" data-id="${m.id}" data-emoji="${esc(emoji)}">
        ${esc(emoji)}${pubs.length > 1 ? `<span class="n">${pubs.length}</span>` : ""}
      </button>`).join("");
  return `<div class="reactions">${chips}</div>`;
}

function msgHTML(m, prev, me, fresh) {
  const mePub = me.pub;
  if (m.kind === "info") {
    return `<div class="sys">${body(m.text)}</div>`;
  }
  const grouped = prev && prev.kind !== "info" && prev.author === m.author && m.ts - prev.ts < 300;
  const mention = !m.mine && me.name && m.text?.toLowerCase().includes("@" + me.name.toLowerCase());
  const cls = ["msg", m.mine ? "mine" : "", grouped ? "grouped" : "", mention ? "mention" : "",
    m.mine && !m.ack && !m.deleted ? "pending" : "", m.deleted ? "deleted" : "", fresh ? "fresh" : ""].join(" ");
  // one header per block: name and clock; follow-ups show their clock in the
  // gutter on hover, and only own messages carry a delivery mark
  const who = `data-act="profile" data-pub="${m.author}"`;
  const head = grouped ? "" : `<div class="msg-who">
      <button class="n" ${who}><span${nameFx(m.author)}>${esc(m.name)}</span></button><span class="t">${hhmm(m.ts)}</span></div>`;

  const meta = m.deleted || !m.mine ? "" : `<span class="meta">${ackIcon(m.ack)}</span>`;
  const text = m.text ? body(m.text) : "";
  const quote = m.reply ? `<button class="quote" data-act="goto" data-id="${m.reply}">${icon("reply")}${m.replyName
    ? `<b>${esc(m.replyName)}</b><span>${esc(m.replyText || "załącznik")}</span>` : `<span>starsza wiadomość</span>`}</button>` : "";

  return `<div class="${cls}" data-id="${m.id}">
      <div class="gutter">${grouped ? `<span class="gt">${hhmm(m.ts)}</span>` : `<button ${who}>${avatar(m.avatar, m.name, m.author)}</button>`}</div>
      <div class="msg-col">
        ${quote}${head}
        <div class="bubble">${attachHTML(m)}${text}${meta}</div>
        ${reactionsHTML(m, mePub)}
      </div>
      ${m.deleted ? "" : `<div class="msg-tools">
        ${REACT_SET.slice(0, 3).map((e) => `<button data-act="react" data-id="${m.id}" data-emoji="${e}">${e}</button>`).join("")}
        <button data-act="react-more" data-id="${m.id}" title="Dodaj reakcję">${icon("smile")}</button>
        <button data-act="reply" data-id="${m.id}" title="Odpowiedz">${icon("reply")}</button>
        <button data-act="hide" data-id="${m.id}" title="Schowaj u mnie">${icon("eye")}</button>
        ${m.mine ? `<button data-act="delete" data-id="${m.id}" title="Usuń dla wszystkich">${icon("trash")}</button>` : ""}
      </div>`}
    </div>`;
}

function typingHTML(c) {
  if (!c.typing.length) return "";
  const who = c.typing.length === 1 ? `${c.typing[0]} pisze` : `${c.typing.join(", ")} piszą`;
  return `<span class="dots"><i></i><i></i><i></i></span>${esc(who)}…`;
}

// buildConv lays down the structure once per chat. Everything after this point
// patches in place, which is what keeps the textarea alive across repaints.
function buildConv() {
  const host = $("#conv");
  host.dataset.fsig = "";
  host.innerHTML = `
    <header class="conv-head">
      <div class="head-main" id="conv-head"></div>
      <div class="search head-search">
        <input id="hs" placeholder="Szukaj" value="${esc(S.search?.q || "")}">${icon("search")}
      </div>
    </header>
    <div class="conv-body">
      <div class="msgs" id="msgs"></div>
      <div class="typing" id="typing"></div>
      <div class="composer-wrap" id="composer-wrap">
        <button class="jump" data-act="jump">Przeglądasz starsze wiadomości<b>Wróć na dół ${icon("down")}</b></button>
        <div class="hints" id="hints" hidden></div>
        <div class="replying" id="replying" hidden></div>
        <div class="composer">
          <button class="add-btn" data-act="attach" title="Załącz plik">${icon("plus")}</button>
          <textarea id="input" rows="1" placeholder="Napisz wiadomość albo /komendę…"></textarea>
          <button class="icon-btn gif-btn" data-act="gif" title="Zapisane GIF-y">GIF</button>
          <button class="icon-btn" data-act="emoji" title="Emoji">${icon("smile")}</button>
          <button class="send" data-act="send" disabled>${icon("send")}</button>
        </div>
        <div class="drop-hint" id="drop" hidden>Upuść plik, żeby wysłać</div>
      </div>
    </div>`;
  host.dataset.vk = "";
  appear($(".conv-body", host));
  wireComposer();
  wireMsgScroll($("#msgs"));
  const hs = $("#hs");
  hs.addEventListener("keydown", (e) => {
    if (e.key === "Enter") { e.preventDefault(); searchChat(hs.value); }
    if (e.key === "Escape") { e.stopPropagation(); hs.value = ""; hs.blur(); searchChat(""); }
  });
}

// What a relay operator can and cannot do. Shown when you turn relay-only on,
// and when a link — yours or the other side's choice — runs through a relay.
const RELAY_FACTS = `
  <p>Połączenie idzie przez przekaźnik (TURN) connectora, a nie bezpośrednio między wami.</p>
  <ul class="relay-facts">
    <li><b>Operator connectora widzi</b>, kto z kim rozmawia, kiedy, jak długo i ile danych przesyłacie. Może to zapisywać.</li>
    <li><b>Może zablokować albo zepsuć połączenie</b> w dowolnej chwili.</li>
    <li>Connector może prowadzić każdy. Najlepiej używaj connectora, którego operatora znasz albo któremu ufasz.</li>
    <li><b>Nie widzi treści</b>: wiadomości, pliki i rozmowy głosowe są szyfrowane end-to-end między wami. Nagrany ruch to dla niego szum, a klucze nie przechodzą przez connector.</li>
  </ul>`;

const relayWarned = new Set(); // pubs we already told you about this session

function relayInfo() {
  return modal({ title: "Rozmowa przez przekaźnik", body: RELAY_FACTS, actions: [{ label: "Rozumiem", kind: "primary", value: true }] });
}

// Tell once per person and session when a link goes through a relay — also
// when it was the other side who chose relay-only.
function warnRelayed(people) {
  for (const m of people) {
    if (!m.relay || relayWarned.has(m.pub)) continue;
    relayWarned.add(m.pub);
    toast(`Połączenie z ${m.name} idzie przez przekaźnik connectora (${m.relay}). Operator widzi, że rozmawiacie, ale nie treść.`, "warn");
  }
}

function updateHead(c) {
  const relayed = c.members.filter((m) => !m.me && m.relay);
  warnRelayed(relayed);
  const relayTag = relayed.length
    ? `<button class="relay-tag" data-act="relay-info" title="${esc(relayed.map((m) => `${m.name}: ${m.relay}`).join(", "))}">przez przekaźnik</button>`
    : "";
  const extra = [relayTag, c.hidden ? `<em>${c.hidden} schowanych</em>` : ""].filter(Boolean).join(" · ");
  const back = `<button class="icon-btn back" data-act="back" title="Wróć">${icon("chev")}</button>`;
  const peer = c.members.find((m) => !m.me);
  // a direct chat is about the person, a group header names the channel
  const id = c.direct
    ? `<button class="head-id" data-act="profile" data-pub="${c.peer}" title="Profil">
        ${presenceAvatar(c.avatar, c.name, c.peer, peer?.online)}<span class="t">${esc(c.name)}</span></button>
        <span class="s">${peer?.online ? "online" : "offline"}${extra ? " · " + extra : ""}</span>`
    : `<span class="h-ico">${icon("hash")}</span><b class="t">${esc(chanName(S.snap.chats.find((x) => x.id === c.id), S.ch))}</b><i class="h-div"></i>
        <span class="s">${esc(c.name)} · ${people(c.members.length)}, ${c.online} online${extra ? " · " + extra : ""}</span>`;
  $("#conv-head").innerHTML = `${back}${id}<span class="spacer"></span>
    ${c.call.joined ? "" : `<button class="icon-btn" data-act="join-call" title="Zadzwoń">${icon("phone")}</button>`}
    <button class="icon-btn" data-act="pins" title="Przypięte wiadomości">${icon("pin")}</button>
    ${c.direct ? "" : `<button class="icon-btn" data-act="invite" title="Zaproś">${icon("key")}</button>
      <button class="icon-btn ${S.showMembers ? "on" : ""}" data-act="members-toggle" title="Lista członków">${icon("people")}</button>`}
    <button class="icon-btn" data-act="chat-menu" title="Więcej">${icon("gear")}</button>`;
}

// Which messages have already been on screen. The list is rebuilt wholesale on
// every change, so without this every repaint would replay the entrance
// animation for the entire history.
let seenChat = null, seenIds = new Set();

function updateMsgs(c) {
  const list = $("#msgs");
  const keepBottom = list.scrollTop + list.clientHeight >= list.scrollHeight - 60 || !list.scrollHeight;
  const prevTop = list.scrollTop, prevHeight = list.scrollHeight;

  const opened = seenChat !== S.chat;
  if (opened) { seenChat = S.chat; seenIds = new Set(); }
  // Anything past the newest message we already know about is genuinely new.
  // Older pages pulled in by scrolling up land *before* it and stay still.
  const lastKnown = c.msgs.findLastIndex((m) => seenIds.has(m.id));

  const me = S.snap.me;
  // the first unread message is pinned by id once, so the line stays put
  // while newer messages keep arriving under it
  const mark = S.newMark?.chat === S.chat ? S.newMark : null;
  if (mark && !mark.id) mark.id = c.msgs[Math.max(0, c.msgs.length - mark.n)]?.id;
  let out = "", lastDay = "", prev = null;
  c.msgs.forEach((m, i) => {
    const d = dayLabel(m.ts);
    if (d !== lastDay) { out += `<div class="day">${esc(d)}</div>`; lastDay = d; prev = null; }
    if (mark && m.id === mark.id && i > 0) { out += `<div class="new-line"><span>nowe</span></div>`; prev = null; }
    // only a message that lands while you are watching the bottom slides in
    out += msgHTML(m, prev, me, keepBottom && !opened && i > lastKnown && !seenIds.has(m.id));
    prev = m;
  });
  seenIds = new Set(c.msgs.map((m) => m.id));
  list.innerHTML = out || `<div class="empty" style="margin:auto"><i class="empty-ico">${icon("chat")}</i><strong>Cisza</strong>Napisz pierwszą wiadomość.</div>`;

  // Growing the list from the top (older page) must not move what you read.
  if (keepBottom) list.scrollTop = list.scrollHeight;
  else list.scrollTop = prevTop + (list.scrollHeight - prevHeight);
}

function paintConv(rebuilt) {
  const c = S.conv;
  const host = $("#conv");
  const shell = $("#shell");
  if (!host) return;

  if (!c) {
    const home = S.space === "home";
    shell?.classList.toggle("in-chat", home && S.fpage);
    shell?.classList.toggle("with-members", home);
    shell?.classList.remove("searching");
    if (home) { paintFriends(); paintActive(); S.builtChat = "friends"; return; }
    if (S.builtChat !== null) { host.innerHTML = welcomeHTML; host.dataset.fsig = ""; host.dataset.vk = ""; S.builtChat = null; appear(host.firstElementChild); }
    $("#members").innerHTML = "";
    return;
  }
  shell?.classList.add("in-chat");
  shell?.classList.toggle("with-members", !c.direct && S.showMembers && !S.voice);
  shell?.classList.toggle("searching", !!S.search);

  shell?.classList.toggle("voice-view", !!S.voice);
  if (S.voice) {
    if (rebuilt) buildVoiceView();
    paintCall();
    return;
  }
  if (rebuilt) buildConv();
  updateHead(c);
  $("#typing").innerHTML = typingHTML(c);
  paintMembers(c);
  paintCall();
}

function wireMsgScroll(list) {
  const jump = $(".jump");
  let loading = false;
  const update = () => {
    const bottom = list.scrollTop + list.clientHeight >= list.scrollHeight - 60;
    jump.classList.toggle("show", !bottom);
    S.atBottom = bottom;
    // reaching the top pulls another page of history
    if (list.scrollTop < 40 && !loading && S.conv && S.conv.msgs.length >= S.limit) {
      loading = true;
      S.limit += 60;
      refreshConv().then(() => { S.msgSig = ""; paint(); loading = false; });
    }
  };
  list.addEventListener("scroll", update, { passive: true });
  update();
}

function paintMembers(c) {
  const host = $("#members");
  if (S.search) return paintResults();
  if (!host || c.direct) { if (host) host.innerHTML = ""; return; }
  appear(host, c.id);
  const online = c.members.filter((m) => m.online);
  const offline = c.members.filter((m) => !m.online);
  const row = (m) => `<button class="member ${m.online ? "" : "off"} ${m.inCall ? "in-call" : ""}" data-act="profile" data-pub="${m.pub}">
      <span class="spk" data-speak="${m.pub}">${presenceAvatar(m.avatar, m.name, m.pub, m.online, "s")}</span>
      <span class="nm"><b${nameFx(m.pub, m.style)}>${esc(m.name)}</b><span>${esc(m.status || (m.owner ? "właściciel" : ""))}</span></span>
      ${m.inCall ? `<span class="live">${icon("phone")}</span>` : ""}
      ${m.owner ? `<span class="tag" title="właściciel">${icon("crown")}</span>` : ""}
    </button>`;
  host.innerHTML = `
    <div class="scroll">
      <div class="sec">Dostępni · ${online.length}</div>
      <div class="rows">${online.map(row).join("")}</div>
      ${offline.length ? `<div class="sec">Niedostępni · ${offline.length}</div>
        <div class="rows">${offline.map(row).join("")}</div>` : ""}
    </div>`;
}

/* ---------------------------- video tiles ---------------------------- */

// The voice view: one tile per person in the call, plus one per shared
// screen. Without a camera the tile shows the avatar with its speaking ring.
function buildVoiceView() {
  $("#conv").innerHTML = `
    <header class="conv-head"><div class="head-main" id="conv-head"></div></header>
    <div class="vview">
      <div class="vtiles" id="vtiles"></div>
      <div class="vbar" id="vbar"></div>
    </div>`;
  $("#conv").dataset.vk = "";
  appear($(".vview"));
}

function liveFrames(key) {
  const now = Date.now();
  return [...S.frames.entries()].filter(([k, f]) => k.startsWith(key + ".") && now - f.at < 4000);
}

// setFrame paints a call frame into a canvas. The bytes are fetched and
// decoded off the main thread, and while one frame loads, newer ones only
// replace the URL waiting next: a slow moment skips frames instead of
// queueing them, so the picture never lags behind.
function setFrame(cv, src) {
  cv._want = src;
  if (cv._busy) return;
  cv._busy = true;
  const load = (url) => fetch(url)
    .then((r) => (r.ok ? r.blob() : null))
    .then((b) => b && createImageBitmap(b))
    .then((bm) => {
      if (!bm) return;
      if (cv.width !== bm.width || cv.height !== bm.height) { cv.width = bm.width; cv.height = bm.height; }
      cv.getContext("2d").drawImage(bm, 0, 0);
      bm.close();
    })
    .catch(() => {})
    .then(() => { if (cv._want !== url) return load(cv._want); cv._busy = false; });
  load(src);
}

// keyed reconcile: tiles stay put between repaints, only their picture moves
function syncTiles(host, tiles) {
  const want = new Set(tiles.map((t) => t.key));
  for (const el of [...host.children]) if (!want.has(el.dataset.key)) el.remove();
  for (const t of tiles) {
    let el = host.querySelector(`[data-key="${CSS.escape(t.key)}"]`);
    if (!el) {
      el = document.createElement("div");
      el.dataset.key = t.key;
      host.append(el);
    }
    if (el.dataset.sig !== t.sig) { el.dataset.sig = t.sig; el.className = t.cls; el.innerHTML = t.html; }
    const cv = el.querySelector("canvas.v");
    if (cv && t.src) setFrame(cv, t.src);
  }
}

function paintCall() {
  const g = S.snap?.chats.find((x) => x.call);
  paintPip(g);
  if (!S.voice || !$("#vtiles")) return;
  const c = S.conv, key = S.voice;
  const grp = S.snap.chats.find((x) => x.id === S.chat);
  const joined = grp?.callKey === key;
  const name = chanName(grp, key.slice(S.chat.length), "voice");
  const people = joined ? c.members.filter((m) => m.inCall)
    : c.members.filter((m) => (grp?.voice[key.slice(S.chat.length)] || []).includes(m.name));
  const frames = new Map(liveFrames(key));
  const tiles = [];
  for (const m of people) {
    const cam = frames.get(`${key}.${m.pub}.c`);
    tiles.push({
      key: m.pub, cls: "vtile" + (cam ? " cam" : "") + (peerVol.get(m.pub)?.mute ? " muted" : ""),
      sig: (cam ? "c" : "a" + m.avatar) + peerVol.get(m.pub)?.mute + m.flags,
      src: cam && `/frame/${key}.${m.pub}.c?n=${cam.seq}`,
      html: cam ? `<canvas class="v"></canvas><span class="cap">${esc(m.name)}${callFlags(m.flags)}</span>${FULL_BTN}`
        : `<span class="spk" data-speak="${m.pub}">${avatar(m.avatar, m.name, m.pub, "xl")}</span><span class="cap">${esc(m.name)}${callFlags(m.flags)}</span>`,
    });
    const scr = frames.get(`${key}.${m.pub}.s`);
    if (scr) tiles.push({ key: m.pub + ".s", cls: "vtile cam screen", sig: "s", src: `/frame/${key}.${m.pub}.s?n=${scr.seq}`,
      html: `<canvas class="v"></canvas><span class="cap">${esc(m.name)} · ekran</span>${FULL_BTN}` });
  }
  const host = $("#vtiles");
  host.dataset.n = Math.min(tiles.length, 4);
  // a focused tile whose picture stopped drops back into the grid
  if (S.focus && !tiles.some((t) => t.key === S.focus && t.src)) setFocus(null);
  syncTiles(host, tiles);
  host.classList.toggle("focused", !!S.focus);
  for (const el of host.children) {
    el.classList.toggle("focus", el.dataset.key === S.focus);
    el.classList.toggle("full", el.dataset.key === S.focus && S.full);
  }
  if (!people.length) host.innerHTML = `<div class="empty"><i class="empty-ico">${icon("volume")}</i><strong>Pusto</strong>Nikogo nie ma w tym kanale.</div>`;

  const head = `<button class="icon-btn back" data-act="back" title="Wróć">${icon("chev")}</button>
    <span class="h-ico">${icon("volume")}</span><b class="t">${esc(name)}</b><i class="h-div"></i>
    <span class="s">${esc(grp?.name || "")}</span><span class="spacer"></span>`;
  if ($("#conv-head").dataset.sig !== head) { $("#conv-head").dataset.sig = head; $("#conv-head").innerHTML = head; }
  const k = grp?.call, d = `data-chat="${key}"`;
  const bar = joined ? `
      <button class="ctl ${k.mic ? "" : "off"}" data-act="mic" ${d} title="Mikrofon">${icon(k.mic ? "mic" : "micOff")}</button>
      <button class="ctl ${k.deaf ? "off" : ""}" data-act="deaf" ${d} title="Głośnik">${icon(k.deaf ? "deaf" : "volume")}</button>
      <button class="ctl ${k.cam ? "on" : ""}" data-act="cam" ${d} title="Kamera">${icon("cam")}</button>
      <button class="ctl ${k.screen ? "on" : ""}" data-act="screen" ${d} title="Udostępnij ekran">${icon("screen")}</button>
      <button class="ctl hang" data-act="hangup" ${d} title="Rozłącz">${icon("hangup")}</button>`
    : `<button class="btn join" data-act="voice-chan" data-chat="${S.chat}" data-ch="${key.slice(S.chat.length)}">${icon("phone")}Dołącz do kanału</button>`;
  if ($("#vbar").dataset.sig !== bar) { $("#vbar").dataset.sig = bar; $("#vbar").innerHTML = bar; }
}

const FULL_BTN = `<button class="vfull" data-act="tile-full" title="Pełny ekran">${icon("expand")}</button>`;

// setFocus: a click on a video tile blows it up over the stage (the rest
// shrink to a strip), full also covers the whole screen. null goes back.
function setFocus(key, full = false) {
  if (S.full !== (full && !!key)) api("SetFullscreen", full && !!key).catch(() => {});
  S.focus = key;
  S.full = full && !!key;
  paintCall();
}

// Floating window: while you are in a call with video and look at something
// else, the picture keeps playing in a small window you can drag anywhere.
// A click on it goes back to the voice view.
function paintPip(g) {
  let pip = $("#pip");
  const key = g?.callKey;
  const fr = key && S.voice !== key ? liveFrames(key) : [];
  // someone else's screen first, then any camera, your own preview last
  const me = S.snap?.me.pub;
  fr.sort(([a], [b]) => (b.endsWith(".s") - a.endsWith(".s")) || (a.includes(me) - b.includes(me)));
  if (!fr.length) { pip?.remove(); return; }
  if (!pip) {
    pip = document.createElement("div");
    pip.id = "pip";
    pip.innerHTML = `<canvas class="v"></canvas><span class="cap"></span>`;
    document.body.append(pip);
    if (S.pip) Object.assign(pip.style, { left: S.pip[0] + "px", top: S.pip[1] + "px", right: "auto", bottom: "auto" });
    dragPip(pip);
  }
  pip.dataset.key = key;
  const [k, f] = fr[0];
  pip.dataset.tile = k.slice(key.length + 1).replace(/\.c$/, "");
  const src = `/frame/${k}?n=${f.seq}`;
  setFrame(pip.querySelector("canvas"), src);
  pip.querySelector(".cap").textContent = f.name + (k.endsWith(".s") ? " · ekran" : "") + " · " + chanName(g, key.slice(g.id.length), "voice");
}

function dragPip(pip) {
  pip.addEventListener("pointerdown", (e) => {
    e.preventDefault(); // a drag, not a text selection across the page
    const r = pip.getBoundingClientRect(), ox = e.clientX - r.left, oy = e.clientY - r.top;
    let moved = false;
    pip.setPointerCapture(e.pointerId);
    const move = (ev) => {
      if (!moved && Math.hypot(ev.clientX - e.clientX, ev.clientY - e.clientY) < 4) return;
      moved = true;
      const x = Math.max(8, Math.min(innerWidth - r.width - 8, ev.clientX - ox));
      const y = Math.max(8, Math.min(innerHeight - r.height - 8, ev.clientY - oy));
      S.pip = [x, y];
      Object.assign(pip.style, { left: x + "px", top: y + "px", right: "auto", bottom: "auto" });
    };
    pip.addEventListener("pointermove", move);
    pip.addEventListener("pointerup", () => {
      pip.removeEventListener("pointermove", move);
      if (!moved) openVoice(pip.dataset.key).then(() => setFocus(pip.dataset.tile));
    }, { once: true });
  });
}

const curGroup = () => S.snap.chats.find((c) => c.id === S.space);
const callKey = () => S.snap.chats.find((c) => c.call)?.callKey || S.chat;

async function putChannel(id, kind, name, cat, arch = false) {
  try { await api("PutChannel", S.space, id, kind, name, cat, arch); await refresh(); } catch (e) { fail(e); }
}
async function addChannel(kind, name, cat) {
  try { await api("AddChannel", S.space, kind, name, cat); await refresh(); } catch (e) { fail(e); }
}

function chanDialog(cat, kind) {
  const kinds = [["text", "hash", "Tekstowy", "Wiadomości, obrazy, pliki"], ["voice", "volume", "Głosowy", "Rozmowa, kamera i ekran"]];
  return modal({
    title: "Utwórz kanał",
    body: `<div class="field"><label>Typ kanału</label><div class="kinds" id="ck">${kinds.map(([k, ic, t, d]) =>
        `<label class="kind"><input type="radio" name="ck" value="${k}" ${k === kind ? "checked" : ""}>${icon(ic)}<span><b>${t}</b><small>${d}</small></span></label>`).join("")}</div></div>
      <div class="field"><label>Nazwa kanału</label><input class="input" id="pv" placeholder="nowy-kanał" maxlength="64"></div>`,
    actions: [
      { label: "Anuluj", kind: "ghost", value: null },
      { label: "Utwórz kanał", kind: "primary", get value() { return { name: $("#pv").value.trim(), kind: $("#ck input:checked").value }; } },
    ],
    onMount: (box) => $("#pv", box).addEventListener("keydown", (e) => {
      if (e.key === "Enter") $("footer .primary", box).click();
    }),
  }).then((r) => { if (r?.name) addChannel(r.kind, r.name, cat); });
}

// Per-person sound in calls, kept for the session: a new call starts a new
// mixer, so the settings are pushed again on every join.
const peerVol = new Map(); // pub -> {gain, pan, mute}
function setPeer(pub, patch) {
  const v = { gain: 1, pan: 0, mute: false, ...peerVol.get(pub), ...patch };
  peerVol.set(pub, v);
  S.listSig = ""; // the voice list shows who you muted
  api("SetVoice", pub, v.gain, v.pan).catch(() => {});
  api("MutePeer", pub, v.mute).catch(() => {});
  for (const el of $$(`[data-speak="${pub}"]`)) el.closest(".vtile, .vc-user, .member")?.classList.toggle("muted", v.mute);
}
const reapplyPeers = () => { for (const pub of peerVol.keys()) setPeer(pub, {}); };

// userMenu: what you can do with a person, opened at anchor (an element or a
// point). Your own entry carries your call controls instead.
function userMenu(pub, anchor) {
  const m = S.conv?.members.find((p) => p.pub === pub);
  if (!m) return;
  const g = S.snap.chats.find((c) => c.id === S.conv.id);
  const v = { gain: 1, mute: false, ...peerVol.get(pub) };
  const item = (act, label, extra = "", cls = "") => `<button class="menu-item ${cls}" data-act="${act}" data-pub="${pub}" ${extra}>${esc(label)}</button>`;
  const sep = `<i class="menu-sep"></i>`;
  let body;
  if (m.me) {
    const k = g?.call;
    body = `${item("profile", "Profil")}${item("settings", "Edytuj profil", `data-section="profile"`)}
      ${k ? sep + item("ub-mic", k.mic ? "Wycisz mikrofon" : "Włącz mikrofon")
        + item("ub-deaf", k.deaf ? "Włącz dźwięk" : "Wycisz dźwięk")
        + item("hangup", "Rozłącz", `data-chat="${g.callKey}"`, "danger") : ""}`;
  } else {
    // the call they are in: the voice map lists names per channel
    const ch = Object.keys(g?.voice || {}).find((k) => g.voice[k].includes(m.name));
    const key = ch !== undefined ? g.id + ch : m.inCall ? g?.callKey : "";
    const friend = S.snap.friends.some((f) => f.pub === pub);
    body = `${item("profile", "Profil")}${item("dm", "Napisz wiadomość")}
      ${friend ? "" : item("befriend", "Dodaj do znajomych")}
      ${!m.inCall ? "" : `${sep}
        <div class="menu-vol"><span>Głośność użytkownika <b class="mono" id="uv-l">${Math.round(v.gain * 100)}%</b></span>
          <input class="slider" id="uv" type="range" min="0" max="2" step="0.05" value="${v.gain}"></div>
        <label class="menu-item check">Wycisz<input type="checkbox" id="um" ${v.mute ? "checked" : ""}></label>`}
      ${g?.isOwner && key ? sep + item("call-kick", "Rozłącz z kanału", `data-chat="${key}"`, "danger") : ""}`;
  }
  const el = popover(anchor, `<div class="menu">${body}</div>`, "bottom", "menu-pop");
  if (!el) return;
  $("#uv", el)?.addEventListener("input", (e) => {
    setPeer(pub, { gain: +e.target.value });
    $("#uv-l", el).textContent = Math.round(e.target.value * 100) + "%";
  });
  $("#um", el)?.addEventListener("change", (e) => setPeer(pub, { mute: e.target.checked }));
}

async function copyText(t) {
  try { await navigator.clipboard.writeText(t); toast("Skopiowane", "ok"); }
  catch { toast("Nie udało się skopiować", "err"); }
}

// msgMenu: what a right click on a message offers, like the hover tools plus
// the things that do not fit there.
function msgMenu(id, at) {
  const m = S.conv?.msgs.find((x) => x.id === id);
  if (!m || m.kind === "info") return;
  const d = `data-id="${m.id}"`;
  const item = (act, label, ic, extra = "", cls = "") => `<button class="menu-item ${cls}" data-act="${act}" ${d} ${extra}>${esc(label)}${icon(ic)}</button>`;
  const sep = `<i class="menu-sep"></i>`;
  popover(at, `<div class="menu">
      ${m.deleted ? "" : `<div class="menu-reacts">${REACT_SET.slice(0, 4).map((e) =>
        `<button data-act="react" ${d} data-emoji="${e}">${e}</button>`).join("")}</div>
      ${item("react-more", "Dodaj reakcję", "smile")}${sep}
      ${item("reply", "Odpowiedz", "reply")}
      ${item("pin", m.pinned ? "Odepnij wiadomość" : "Przypnij wiadomość", "pin", m.pinned ? `data-unpin="1"` : "")}
      ${m.text ? item("copy-text", "Kopiuj tekst", "copy") : ""}
      ${m.mime === "image/gif" && m.have ? (gifs().some((g) => g.blob === m.blob)
        ? item("gif-unsave", "Usuń z zapisanych GIF-ów", "trash", `data-blob="${m.blob}"`)
        : item("gif-save", "Zapisz GIF", "star")) : ""}${sep}`}
      ${item("hide", "Schowaj u mnie", "eye")}
      ${m.mine && !m.deleted || S.conv.isOwner && !m.deleted ? item("delete", "Usuń wiadomość", "trash", "", "danger") : ""}
      ${S.snap.me.dev ? sep + item("copy-id", "Kopiuj ID wiadomości", "hash") : ""}
    </div>`, "bottom", "menu-pop");
}

// Saved GIFs: only ones that came through a chat, kept as the blob hash on this
// device. Sending one again re-reads the local copy and posts it as a new file.
function gifs() {
  try { return JSON.parse(localStorage.getItem("szept.gifs") || "[]"); } catch { return []; }
}
function setGifs(list) {
  try { localStorage.setItem("szept.gifs", JSON.stringify(list.slice(0, 200))); } catch {}
}
function gifPicker(anchor) {
  const list = gifs();
  const el = popover(anchor, `<div class="gifs">${list.length ? `<div class="gif-grid">${list.map((g) => `
      <div class="gif-cell"><button class="gif-send" data-blob="${esc(g.blob)}" title="${esc(g.file || "GIF")}"><img src="/blob/${esc(g.blob)}" alt="" loading="lazy"></button>
        <button class="gif-x" data-blob="${esc(g.blob)}" title="Usuń z zapisanych">${icon("close")}</button></div>`).join("")}</div>`
    : `<div class="quiet"><i class="empty-ico">${icon("star")}</i><b>Brak zapisanych GIF-ów</b>Kliknij GIF na czacie prawym przyciskiem i wybierz „Zapisz GIF”.</div>`}</div>`,
    "top", "gif-pop");
  el?.addEventListener("click", async (e) => {
    const x = e.target.closest(".gif-x");
    if (x) { setGifs(gifs().filter((g) => g.blob !== x.dataset.blob)); x.parentElement.remove(); return; }
    const b = e.target.closest(".gif-send");
    if (!b) return;
    popClose?.();
    try {
      const g = gifs().find((y) => y.blob === b.dataset.blob);
      const data = await (await fetch("/blob/" + b.dataset.blob)).blob();
      if (!data.size) throw new Error("Tego GIF-a nie ma już na tym urządzeniu.");
      await sendFileObject(new File([data], g?.file || "gif.gif", { type: "image/gif" }));
    } catch (err) { fail(err); }
  });
}

// pinsPop lists the chat's pinned messages; a click jumps to one.
function pinsPop(anchor) {
  const pins = S.conv?.pinned || [];
  popover(anchor, `<div class="pins"><h3>Przypięte wiadomości</h3>${pins.length ? pins.slice().reverse().map((m) => `
      <div class="pin-row">
        <button class="pin-go" data-act="goto" data-id="${m.id}">
          <span class="pin-who"><b>${esc(m.name)}</b><span class="t">${hhmm(m.ts)}</span></span>
          <span class="pin-tx">${esc(m.text || m.file || "")}</span>
        </button>
        <button class="icon-btn" data-act="pin" data-id="${m.id}" data-unpin="1" title="Odepnij">${icon("close")}</button>
      </div>`).join("") : `<div class="quiet"><i class="empty-ico">${icon("pin")}</i><b>Nic nie przypięto</b>Kliknij wiadomość prawym przyciskiem i wybierz „Przypnij”.</div>`}</div>`,
    "bottom", "pins-pop");
}

// Right click: on a person anywhere (call tile, voice list, member list) the
// person menu; on the channel list the owner's channel menu.
document.addEventListener("contextmenu", (e) => {
  const person = e.target.closest("[data-speak], .vc-user[data-pub], .member[data-pub], .vtile");
  const pub = person?.dataset.speak || person?.dataset.pub || person?.querySelector("[data-speak]")?.dataset.speak
    || (person?.classList.contains("vtile") && person.dataset.key.split(".")[0]);
  const at = { getBoundingClientRect: () => new DOMRect(e.clientX, e.clientY, 0, 0), contains: () => false };
  if (pub) {
    e.preventDefault();
    return userMenu(pub, at);
  }
  const msg = e.target.closest(".msg[data-id]");
  // a selection or a link keeps the native menu (copy, inspect in dev mode)
  if (msg && !getSelection().toString() && !e.target.closest("a")) {
    e.preventDefault();
    return msgMenu(msg.dataset.id, at);
  }
  const g = e.target.closest("#list") && curGroup();
  if (!g?.isOwner) return;
  e.preventDefault();
  const r = e.target.closest("[data-ctx]");
  const box = e.target.closest(".cat");
  const x = r ? chanOf(g, r.dataset.ch, r.dataset.kind) : box?.dataset.cat ? chanOf(g, box.dataset.cat, "cat") : null;
  const item = (act, label, ic, extra = "", danger = "") => `<button class="menu-item ${danger}" data-act="${act}" ${extra}>${esc(label)}${icon(ic)}</button>`;
  const xa = x && `data-ch="${x.id}" data-kind="${x.kind}"`;
  popover(at, `<div class="menu">
      ${item("chan-new", "Utwórz kanał", "plus", `data-cat="${box?.dataset.cat || ""}" data-kind="${box?.dataset.kind || "text"}"`)}
      ${item("cat-new", "Utwórz kategorię", "plus")}
      ${!x ? "" : x.arch ? `<i class="menu-sep"></i>
        ${item("chan-restore", "Przywróć kanał", "plus", xa)}
        ${item("chan-purge", "Usuń na stałe", "trash", xa, "danger")}` : `<i class="menu-sep"></i>
        ${item("chan-rename", x.kind === "cat" ? "Zmień nazwę kategorii" : "Zmień nazwę kanału", "edit", xa)}
        ${item("chan-del", x.kind === "cat" ? "Usuń kategorię" : "Przenieś do archiwum", "trash", xa, "danger")}`}
    </div>`, "bottom", "menu-pop");
});

// dropChannel archives (name kept) or deletes (name "") channel x, first
// leaving its call and its message list if you are in them.
async function dropChannel(g, x, name, arch) {
  if (x.kind === "voice" && g.callKey === g.id + x.id) await act("LeaveCall", g.callKey);
  await putChannel(x.id, x.kind, name, x.cat, arch);
  if (x.kind === "text" && S.chat === g.id && S.ch === x.id) await openChat(g.id);
}

// Starting a camera or screen share asks how: monitor (when there is a
// choice), frame rate, resolution, quality. The last pick is preselected.
// Stopping, and the slash commands, skip the question.
const VIDEO_CHOICES = {
  fps: [[15, "15 fps"], [30, "30 fps"], [60, "60 fps"]],
  cam: [[480, "480p"], [720, "720p"], [1080, "1080p"]],
  screen: [[720, "720p"], [1080, "1080p"], [1440, "1440p"]],
  quality: [[40, "Niska"], [60, "Średnia"], [85, "Wysoka"]],
};

async function startVideo(anchor, kind, key) {
  const method = kind === "cam" ? "ToggleCam" : "ToggleScreen";
  const pre = (o) => (kind === "cam" ? [o] : [0, o]);
  if (S.snap.chats.find((c) => c.callKey === key)?.call?.[kind]) return act(method, key, ...pre({}));
  const [picks, mons] = await Promise.all([api("VideoPicks").catch(() => ({})),
    kind === "screen" ? api("Displays").catch(() => []) : []]);
  const p = picks[kind] || {};
  if (!anchor) return act(method, key, ...pre(p));
  const sel = (name, label, list, v) => `<label class="vp-row"><span>${label}</span>
    <select class="input" data-k="${name}">${list.map(([n, t]) =>
      `<option value="${n}" ${n === v ? "selected" : ""}>${esc(t)}</option>`).join("")}</select></label>`;
  popover(anchor, `<div class="vpick">
    ${mons?.length > 1 ? sel("d", "Monitor", mons.map((d, i) => [i, `Monitor ${i + 1} · ${d}`]), 0) : ""}
    ${sel("fps", "Klatki", VIDEO_CHOICES.fps, p.fps)}
    ${sel("height", "Rozdzielczość", VIDEO_CHOICES[kind], p.height)}
    ${sel("quality", "Jakość", VIDEO_CHOICES.quality, p.quality)}
    <button class="btn primary" data-act="video-go" data-kind="${kind}" data-chat="${key}">${kind === "cam" ? "Włącz kamerę" : "Udostępnij ekran"}</button>
  </div>`, "top", "menu-pop");
}

// openVoice shows the voice view of a call key (chat ID + channel ID).
async function openVoice(key) {
  const chat = key.slice(0, 64);
  if (S.chat !== chat) await openChat(chat);
  S.voice = key;
  paint();
}

setInterval(() => { if (S.frames.size) paintCall(); }, 1500);

/* ============================ composer ============================ */

const COMMANDS = [
  { n: "/pomoc", d: "spis komend" },
  { n: "/help", d: "command list" },
  { n: "/nowy", a: "<nazwa>", d: "nowa grupa" },
  { n: "/new", a: "<name>", d: "create group" },
  { n: "/dm", a: "<znajomy>", d: "czat na dwoje ze znajomym" },
  { n: "/kod", d: "mój kod QR i pole na cudzy" },
  { n: "/code", d: "my QR code" },
  { n: "/znajomy", a: "<kod SZF->", d: "dodaj znajomego z kodu" },
  { n: "/friend", a: "<code SZF->", d: "add friend from code" },
  { n: "/dolacz", a: "<kod SZC->", d: "wejdź do grupy z zaproszenia" },
  { n: "/join", a: "<code SZC->", d: "join group from invite" },
  { n: "/zapros", d: "kod zaproszenia + PIN" },
  { n: "/invite", d: "invite code + PIN" },
  { n: "/szukaj", a: "<fraza>", d: "szukaj w historii czatu" },
  { n: "/search", a: "<query>", d: "search chat history" },
  { n: "/kto", d: "członkowie czatu" },
  { n: "/who", d: "chat members" },
  { n: "/kick", a: "<nazwa>", d: "usuń członka (rotuje klucz)" },
  { n: "/rekey", d: "rotacja klucza, nowa epoka" },
  { n: "/zmien", a: "<nazwa>", d: "zmień nazwę czatu" },
  { n: "/rename", a: "<name>", d: "rename chat" },
  { n: "/call", d: "dołącz do rozmowy" },
  { n: "/rozlacz", d: "wyjdź z rozmowy" },
  { n: "/leave", d: "leave call" },
  { n: "/hangup", d: "leave call" },
  { n: "/mic", d: "mikrofon włącz/wyłącz" },
  { n: "/cam", d: "kamera włącz/wyłącz" },
  { n: "/ekran", d: "udostępnianie ekranu" },
  { n: "/screen", d: "share screen" },
  { n: "/wyslij", d: "wyślij plik z dysku" },
  { n: "/send", d: "send a file" },
  { n: "/awatar", d: "zmień swój awatar" },
  { n: "/avatar", d: "change avatar" },
  { n: "/connector", a: "[adres]", d: "pokaż albo zmień connector" },
  { n: "/ja", d: "twój profil i ustawienia" },
  { n: "/me", d: "my profile" },
  { n: "/lang", a: "<pl|en>", d: "zmień język / change language" },
  { n: "/jezyk", a: "<pl|en>", d: "zmień język" },
];

const CMD = {
  "/pomoc": cmdHelp, "/help": cmdHelp,
  "/nowy": cmdNew, "/new": cmdNew,
  "/dm": cmdDM,
  "/kod": () => myCodeDialog(), "/code": () => myCodeDialog(),
  "/znajomy": (a) => addFriendDialog(a), "/friend": (a) => addFriendDialog(a),
  "/dolacz": (a) => joinDialog(a), "/join": (a) => joinDialog(a),
  "/zapros": () => inviteDialog(), "/invite": () => inviteDialog(),
  "/szukaj": (a) => searchChat(a), "/search": (a) => searchChat(a),
  "/kto": () => membersDialog(), "/who": () => membersDialog(),
  "/kick": cmdKick,
  "/rekey": () => act("Rekey", S.chat),
  "/zmien": cmdRename, "/rename": cmdRename,
  "/call": () => HANDLERS["join-call"]({ dataset: {} }),
  "/rozlacz": () => act("LeaveCall", callKey()), "/leave": () => act("LeaveCall", callKey()),
  "/hangup": () => act("LeaveCall", callKey()),
  "/mic": () => act("ToggleMic", callKey()),
  "/cam": () => startVideo(null, "cam", callKey()),
  "/ekran": () => startVideo(null, "screen", callKey()), "/screen": () => startVideo(null, "screen", callKey()),
  "/wyslij": () => act("PickFiles", S.chat, ""), "/send": () => act("PickFiles", S.chat, ""),
  "/awatar": () => pickPicture("avatar"), "/avatar": () => pickPicture("avatar"),
  "/connector": cmdConnector,
  "/ja": () => openSettings("profile"), "/me": () => openSettings("profile"),
  "/lang": cmdLang, "/jezyk": cmdLang,
};

// act: fire a bound method and surface its error as a toast.
async function act(method, ...args) {
  try { await api(method, ...args); refresh(); } catch (e) { fail(e); }
}

function cmdHelp() {
  const rows = COMMANDS.map((c) =>
    `<div class="list-line"><span class="mono">${esc(c.n)} ${esc(c.a || "")}</span><span style="color:var(--muted)">${esc(c.d)}</span></div>`).join("");
  modal({ title: "Komendy", wide: true, body: `<div class="block" style="margin:0 16px">${rows}</div>` });
}

async function cmdNew(arg) {
  const r = await modal({
    title: "Nowa grupa",
    body: `<div class="field"><label>Nazwa</label><input class="input" id="pv" value="${esc(arg || "")}" placeholder="np. ekipa"></div>
      <label class="check-line"><input type="checkbox" id="ph"> Nowi członkowie widzą wcześniejszą historię</label>
      <p class="hint-text">Bez tego nowa osoba widzi tylko to, co napisano po jej dołączeniu: dostaje klucze od momentu wejścia, a Ty po każdym dodaniu kogoś zmieniasz klucz automatycznie.</p>`,
    actions: [
      { label: "Anuluj", kind: "ghost", value: null },
      { label: "Załóż", kind: "primary", get value() { return { name: $("#pv").value.trim(), history: $("#ph").checked }; } },
    ],
  });
  if (!r || !r.name) return;
  try { openChat(await api("CreateGroup", r.name, r.history)); } catch (e) { fail(e); }
}

async function cmdDM(arg) {
  const f = S.snap.friends.find((x) => x.name.toLowerCase() === String(arg).toLowerCase());
  if (!f) return toast("Nie znam takiego znajomego: " + arg, "err");
  try { openChat(f.chat || await api("CreateDirect", f.pub)); } catch (e) { fail(e); }
}

async function cmdKick(arg) {
  const m = S.conv?.members.find((x) => x.name.toLowerCase() === String(arg).toLowerCase());
  if (!m) return toast("Nie ma tu nikogo o nazwie " + arg, "err");
  if (await confirmBox("Usunąć członka", `Usunąć ${m.name}? Klucz czatu zostanie obrócony.`)) {
    try { await api("Kick", S.chat, m.pub); toast("Usunięty"); } catch (e) { fail(e); }
  }
}

async function cmdRename(arg) {
  const name = arg || await promptBox({ title: "Zmień nazwę", label: "Nazwa czatu", value: S.conv?.name || "" });
  if (!name) return;
  try { await api("Rename", S.chat, name); } catch (e) { fail(e); }
}

async function cmdConnector(arg) {
  if (!arg) return toast("Connector: " + (S.snap.me.connector || "brak"));
  const s = await api("Settings");
  s.connector = arg;
  try { await api("SaveSettings", s); toast("Connector: " + arg, "ok"); } catch (e) { fail(e); }
}

async function cmdLang(arg) {
  const lang = (arg || "").toLowerCase();
  if (!["pl", "en"].includes(lang)) return toast("Użycie: /lang pl albo /lang en", "err");
  const s = await api("Settings");
  s.lang = lang;
  try { await api("SaveSettings", s); toast("Język: " + lang, "ok"); } catch (e) { fail(e); }
}

function matchCommands(text) {
  if (!text.startsWith("/") || text.includes(" ")) return [];
  const q = text.toLowerCase();
  return COMMANDS.filter((c) => c.n.startsWith(q)).slice(0, 7);
}

function paintHints() {
  const box = $("#hints");
  if (!box) return;
  if (!S.hints.length) { box.hidden = true; return; }
  box.hidden = false;
  box.innerHTML = S.hints.map((c, i) =>
    `<button class="hint ${i === S.hintIdx ? "sel" : ""}" data-act="hint" data-i="${i}">
       <b>${esc(c.n)}</b><i>${esc(c.a || "")}</i><span>${esc(c.d)}</span></button>`).join("");
}

let typingTimer = null;

// field-sizing:content is not in the WebKitGTK several distros ship, and a
// composer that refuses to grow past one line is worse than a few lines of JS.
function autoGrow(input) {
  if (!input) return;
  input.style.height = "auto";
  input.style.height = Math.min(input.scrollHeight, 168) + "px";
}

function wireComposer() {
  const input = $("#input");
  const send = $(".composer .send");
  if (!input) return;

  const sync = () => {
    autoGrow(input);
    send.disabled = input.value.trim() === "";
    S.hints = matchCommands(input.value.trim());
    S.hintIdx = Math.min(S.hintIdx, Math.max(0, S.hints.length - 1));
    paintHints();
  };

  const typing = (on) => api("Typing", S.chat, on).catch(() => {});
  input.addEventListener("input", () => {
    sync();
    if (!input.value.startsWith("/")) {
      typing(true);
      clearTimeout(typingTimer);
      typingTimer = setTimeout(() => typing(false), 2500);
    }
  });

  input.addEventListener("keydown", (e) => {
    if (S.hints.length) {
      if (e.key === "ArrowDown") { e.preventDefault(); S.hintIdx = (S.hintIdx + 1) % S.hints.length; return paintHints(); }
      if (e.key === "ArrowUp") { e.preventDefault(); S.hintIdx = (S.hintIdx - 1 + S.hints.length) % S.hints.length; return paintHints(); }
      // Tab always completes; Enter only completes when there is something
      // left to complete, otherwise it runs what you already typed.
      const pick = S.hints[S.hintIdx];
      const done = input.value.trim().toLowerCase() === pick.n;
      if (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey && !done)) {
        e.preventDefault();
        input.value = pick.n + (pick.a ? " " : "");
        S.hints = []; paintHints(); sync();
        return;
      }
      if (e.key === "Escape") { S.hints = []; return paintHints(); }
    }
    if (e.key === "Escape" && S.reply) { e.stopPropagation(); return setReply(null); }
    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); submit(); }
  });

  send.addEventListener("click", submit);
  input.addEventListener("paste", async (e) => {
    const files = [...e.clipboardData.files];
    if (!files.length) return;
    e.preventDefault();
    for (const f of files) await sendFileObject(f);
  });
  input.focus();
  sync();
}

// Drag & drop lives on the document, not on the conversation: the conversation
// element is re-created whenever you switch chats, and re-binding there piled
// up a fresh set of listeners each time.
// A channel row dragged onto a category moves there; the default sections
// take only their own kind.
(function wireDrop() {
  let depth = 0, ch = null, over = null;
  const drop = () => $("#drop");
  const files = (e) => e.dataTransfer?.types.includes("Files");
  const mark = (c) => { if (over !== c) { over?.classList.remove("drop-to"); c?.classList.add("drop-to"); over = c; } };
  const target = (e) => {
    const c = ch && e.target.closest?.(".cat");
    return c && (c.dataset.cat || c.dataset.kind === ch.kind) ? c : null;
  };
  document.addEventListener("dragstart", (e) => {
    const r = e.target.closest?.(".row.chan[draggable]");
    ch = r ? chanOf(curGroup(), r.dataset.ch, r.dataset.kind) : null;
    if (ch) { e.dataTransfer.effectAllowed = "move"; e.dataTransfer.setData("text/plain", ch.name); }
  });
  document.addEventListener("dragend", () => { ch = null; mark(null); });
  document.addEventListener("dragenter", (e) => {
    e.preventDefault();
    if (!files(e)) return;
    ch = null;
    if (++depth === 1 && drop()) drop().hidden = false;
  });
  document.addEventListener("dragover", (e) => {
    e.preventDefault();
    if (!ch) return;
    const c = target(e);
    mark(c);
    e.dataTransfer.dropEffect = c ? "move" : "none";
  });
  document.addEventListener("dragleave", (e) => {
    if (files(e) && --depth <= 0) { depth = 0; if (drop()) drop().hidden = true; }
  });
  document.addEventListener("drop", async (e) => {
    e.preventDefault();
    if (ch) {
      const x = ch, c = target(e);
      ch = null; mark(null);
      if (c && (x.cat || "") !== c.dataset.cat) putChannel(x.id, x.kind, x.name, c.dataset.cat, x.arch);
      return;
    }
    depth = 0;
    if (drop()) drop().hidden = true;
    for (const f of e.dataTransfer.files) await sendFileObject(f);
  });
})();

async function submit() {
  const input = $("#input");
  const text = input.value.trim();
  if (!text) return;
  input.value = "";
  autoGrow(input);
  $(".composer .send").disabled = true;
  S.hints = []; paintHints();

  if (text.startsWith("/")) {
    const [name, ...rest] = text.split(" ");
    const run = CMD[name.toLowerCase()];
    if (!run) return toast(`Nie znam komendy ${name}. Wpisz /pomoc`, "err");
    return run(rest.join(" ").trim());
  }
  if (!S.chat) return toast("Najpierw wybierz czat.", "err");
  clearTimeout(typingTimer);
  api("Typing", S.chat, false).catch(() => {});
  const reply = S.reply;
  setReply(null);
  try { await (reply ? api("SendReply", S.chat, text, reply.id) : api("Send", S.chat, text)); }
  catch (e) { fail(e); input.value = text; setReply(reply); }
}

// setReply shows (or with null clears) the "answering X" strip over the composer.
function setReply(m) {
  S.reply = m;
  const bar = $("#replying");
  if (!bar) return;
  bar.hidden = !m;
  bar.innerHTML = m ? `${icon("reply")}<span>Odpowiadasz <b>${esc(m.name)}</b></span>
    <button class="icon-btn" data-act="reply-cancel" title="Anuluj">${icon("close")}</button>` : "";
  if (m) $("#input")?.focus();
}

function readAsDataURL(file) {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(r.result);
    r.onerror = () => reject(r.error);
    r.readAsDataURL(file);
  });
}

async function sendFileObject(file) {
  if (!S.chat) return toast("Najpierw wybierz czat.", "err");
  try {
    toast("Wysyłam " + file.name);
    await api("SendData", S.chat, file.name, await readAsDataURL(file), "");
  } catch (e) { fail(e); }
}

/* ============================ dialogs ============================ */

async function myCodeDialog() {
  const me = S.snap.me;
  const qr = await api("QR", me.code);
  modal({
    title: "Twój kod",
    body: `<img class="qr" src="${qr}" alt="">
      <div class="code-box">${esc(me.code)}</div>
      ${keyBlock(me.pub)}
      <div class="btn-row"><button class="btn" data-act="copy" data-text="${esc(me.code)}">${icon("copy")}Kopiuj</button></div>
      <div class="field"><label>Kod znajomego</label>
        <input class="input" id="fc" placeholder="SZF-…"></div>`,
    actions: [
      { label: "Zamknij", kind: "ghost", value: null },
      { label: "Dodaj znajomego", kind: "primary", get value() { return { add: $("#fc").value }; } },
    ],
  }).then(async (r) => {
    if (r?.add) {
      try { toast("Dodano: " + await api("AddFriend", r.add), "ok"); } catch (e) { fail(e); }
    }
  });
}

async function addFriendDialog(prefill = "") {
  const code = prefill || await promptBox({
    title: "Dodaj znajomego", label: "Kod znajomego", placeholder: "SZF-…",
    hint: "Kod dostaniesz od drugiej osoby. U niej to „Twój kod”.", ok: "Dodaj",
  });
  if (!code) return;
  try { toast("Dodano: " + await api("AddFriend", code), "ok"); } catch (e) { fail(e); }
}

async function joinDialog(prefill = "") {
  const r = await modal({
    title: "Wejdź do grupy",
    body: `<div class="field"><label>Kod zaproszenia</label>
        <input class="input" id="jc" value="${esc(prefill)}" placeholder="SZC-…"></div>
      <div class="field"><label>PIN</label>
        <input class="input" id="jp" placeholder="6 cyfr" inputmode="numeric"></div>`,
    actions: [
      { label: "Anuluj", kind: "ghost", value: null },
      { label: "Wejdź", kind: "primary", get value() { return { code: $("#jc").value, pin: $("#jp").value }; } },
    ],
  });
  if (!r?.code) return;
  try { await api("Redeem", r.code, r.pin); toast("Zaproszenie przyjęte", "ok"); } catch (e) { fail(e); }
}

async function inviteDialog() {
  if (!S.chat) return toast("Najpierw wybierz czat.", "err");
  try {
    const inv = await api("NewInvite", S.chat);
    modal({
      title: "Zaproszenie do czatu",
      body: `<img class="qr" src="${inv.qr}" alt="">
        <div class="code-box">${esc(inv.code)}</div>
        <div class="field"><label>PIN jednorazowy</label>
          <div class="code-box" style="margin:0;font-size:20px;letter-spacing:6px;text-align:center">${esc(inv.pin)}</div>
          <p class="hint-text">Przekaż PIN innym kanałem niż kod. Dopiero razem wpuszczają do grupy.</p></div>
        <div class="btn-row"><button class="btn" data-act="copy" data-text="${esc(inv.code)}">${icon("copy")}Kopiuj kod</button></div>`,
      actions: [{ label: "Gotowe", kind: "primary", value: null }],
    });
  } catch (e) { fail(e); }
}

// Search works the way the big chat apps do it: type in the header, hits
// stack up in a side panel, and each one jumps back to its place in the chat.
async function searchChat(q) {
  q = q.trim();
  if (!q || !S.chat) { S.search = null; S.conv && paintConv(false); return; }
  try {
    S.search = { q, chat: S.chat, hits: (await api("Search", S.chat, q)).reverse() };
  } catch (e) { return fail(e); }
  const hs = $("#hs");
  if (hs && hs.value !== q) hs.value = q;
  paintConv(false);
}

function paintResults() {
  const host = $("#members"), r = S.search;
  const re = new RegExp(esc(r.q).replace(/[.*+?^${}()|[\]\\]/g, "\\$&"), "gi");
  host.innerHTML = `
    <div class="res-head">
      <b>${r.hits.length ? plural(r.hits.length, "wynik", "wyniki", "wyników") : "Brak wyników"}</b>
      <button class="icon-btn" data-act="search-close" title="Zamknij">${icon("x")}</button>
    </div>
    <div class="scroll"><div class="res-list">
      ${r.hits.map((m) => `<button class="res" data-act="search-jump" data-id="${m.id}">
          <div class="gutter">${avatar(m.avatar, m.name, m.author)}</div>
          <div class="msg-col">
            <div class="msg-who"><span class="n">${esc(m.name)}</span>
              <span class="t">${new Date(m.ts * 1000).toLocaleDateString("pl-PL")} ${hhmm(m.ts)}</span></div>
            <div class="bubble">${esc(m.text).replace(re, "<mark>$&</mark>")}</div>
          </div>
          <span class="go">Przejdź</span>
        </button>`).join("") || `<div class="empty"><i class="empty-ico">${icon("search")}</i><strong>Nic nie znalazłem</strong>Spróbuj innego słowa.</div>`}
    </div></div>`;
}

// jumpTo: scroll the hit into view, pulling older pages until it is loaded.
async function jumpTo(id) {
  for (let i = 0; i < 20 && S.conv && !$(`.msg[data-id="${id}"]`); i++) {
    if (S.conv.msgs.length < S.limit) break; // whole history already loaded
    S.limit += 200;
    await refreshConv();
    S.msgSig = ""; paint();
  }
  const el = $(`.msg[data-id="${id}"]`);
  if (!el) return toast("Ta wiadomość jest schowana albo usunięta.");
  el.scrollIntoView({ block: "center" });
  el.classList.remove("flash"); void el.offsetWidth; el.classList.add("flash");
}

function membersDialog() {
  if (!S.conv) return toast("Najpierw wybierz czat.", "err");
  modal({
    title: `Członkowie · ${S.conv.name}`,
    body: `<div class="rows" style="padding:0 8px 8px">${S.conv.members.map((m) =>
      `<button class="member" data-act="profile" data-pub="${m.pub}">
         ${presenceAvatar(m.avatar, m.name, m.pub, m.online, "s")}
         <span class="nm"><b>${esc(m.name)}</b><span>${esc(m.status || (m.online ? "online" : "offline"))}</span></span>
         ${m.owner ? `<span class="tag" title="właściciel">${icon("crown")}</span>` : ""}
       </button>`).join("")}</div>`,
    actions: [{ label: "Zamknij", kind: "ghost", value: null }],
  });
}

// A profile opens as a card next to whatever you clicked (a name, a member,
// a friend). Without an anchor, e.g. from a slash command, it is a dialog.
async function profileDialog(pub, anchor) {
  let p;
  try { p = await api("ProfileOf", pub); } catch (e) { return fail(e); }
  const canKick = S.conv && !S.conv.direct && S.conv.isOwner && !p.isMe;
  const hist = p.history.length > 1
    ? `<div class="sec-label">Historia nazw</div>${p.history.map((h, i) =>
        `<div class="list-line"><span class="mono">${esc(h.name)}</span>
           ${i === p.history.length - 1 ? `<span class="tag">teraz</span>` : ""}
           <span class="when">${new Date(h.ts * 1000).toLocaleDateString("pl-PL")}</span></div>`).join("")}`
    : "";
  const style = p.isMe ? styleOf(p.pub) : p.style;
  const html = `<div class="pcard" ${cardFx(style)}>
    <div class="card-banner ${p.isMe ? `own" data-act="edit-banner" title="Zmień baner` : ""}" style="--tone:${tint(p.pub)};${p.banner ? `background-image:url('${esc(p.banner)}')` : ""}">
      ${p.isMe ? `<span class="icon-btn edit">${icon("edit")}</span>` : ""}</div>
    <div class="card-id">
      <div class="av-wrap ${p.online ? "online" : ""}">${p.isMe
        ? `<button data-act="edit-avatar" title="Zmień awatar">${avatar(p.avatar, p.name, p.pub, "xl", style)}</button>`
        : avatar(p.avatar, p.name, p.pub, "xl", style)}<i class="presence"></i></div>
      <div class="nm"><span${nameFx(p.pub, style)}>${esc(p.name || "nieznany")}</span></div>
      <div class="st">${esc(p.status || (p.online ? "online" : "offline"))}</div>
      ${p.bio ? `<div class="bio">${esc(p.bio)}</div>` : ""}
    </div>
    ${keyBlock(p.pub)}
    <div class="btn-row">
      ${p.isMe ? `<button class="btn" data-act="settings" data-section="profile">${icon("edit")}Edytuj profil</button>` : `
        <button class="btn primary" data-act="dm" data-pub="${p.pub}">${icon("chat")}Napisz</button>
        ${p.isFriend
          ? `<button class="btn danger" data-act="unfriend" data-pub="${p.pub}">${icon("trash")}Usuń ze znajomych</button>`
          : ""}
        ${canKick ? `<button class="btn danger" data-act="kick" data-pub="${p.pub}">${icon("logout")}Wyrzuć z czatu</button>` : ""}`}
    </div>
    ${hist}</div>`;
  if (anchor?.isConnected) {
    const side = anchor.closest(".members, .userbar") ? (anchor.closest(".userbar") ? "top" : "left") : "right";
    return popover(anchor, html, side, "card");
  }
  // the card is the whole sheet, so its frame and effect cover all of it
  modal({ title: "Profil", body: html, bare: true });
}

function chatMenuHTML(c) {
  const item = (act, ic, label, danger = "") =>
    `<button class="menu-item ${danger}" data-act="${act}">${esc(label)}${icon(ic)}</button>`;
  return `<div class="menu">
      ${c.direct ? "" : item("invite", "key", "Zaproś ludzi")}
      ${!c.direct && c.isOwner ? item("server-look", "edit", "Wygląd serwera") + item("chan-new", "plus", "Utwórz kanał") + item("cat-new", "plus", "Utwórz kategorię") : ""}
      ${item("rename", "edit", "Zmień nazwę")}
      ${c.direct ? "" : item("members", "people", "Członkowie")}
      ${c.hidden ? item("unhide", "eye", `Pokaż schowane (${c.hidden})`) : ""}
      <i class="menu-sep"></i>
      ${item("rekey", "shield", "Obróć klucz")}
      ${c.direct ? "" : item("leave", "logout", c.isOwner ? "Usuń serwer" : "Opuść serwer", "danger")}
    </div>`;
}

function chatMenu(anchor) {
  if (S.conv) popover(anchor, chatMenuHTML(S.conv), "bottom", "menu-pop");
}

// Emoji with Polish and English words to search by. A hand-picked
// set, not the whole Unicode list; swap in emoji data if people miss some.
const EMOJI = `😀 uśmiech smile grin|😃 radość happy|😄 śmiech laugh|😁 szczerzy grin|😆 xd lol|😅 pot sweat ulga|😂 płacz ze śmiechu joy lol|🤣 turla rofl|🥲 łza smile tear|😊 rumieniec blush|😇 aniołek angel|🙂 lekki uśmiech|🙃 odwrócony upside|😉 oczko wink|😌 ulga relieved|😍 zakochany love heart eyes|🥰 serduszka love|😘 buziak kiss|😋 pyszne yum|😛 język tongue|😜 szalony wink tongue|🤪 wariat zany|😝 bleh|🤑 kasa money|🤗 przytulas hug|🤭 ups giggle|🤫 cicho shh|🤔 myśli think hmm|🤐 zamknięte zip|🤨 podejrzliwy brow|😐 neutralny neutral|😑 bez wyrazu|😶 bez słów|😏 zadowolony smirk|😒 znudzony unamused|🙄 przewraca oczami eyeroll|😬 grymas grimace|😮‍💨 wydech exhale|🤥 kłamie lie|😔 smutny pensive|😪 senny sleepy|🤤 ślinka drool|😴 śpi sleep|😷 maska mask chory|🤒 gorączka sick|🤢 mdłości nausea|🤮 wymioty vomit|🥵 gorąco hot|🥶 zimno cold|🥴 pijany woozy|😵 zawrót dizzy|🤯 wybuch mind blown|🤠 kowboj cowboy|🥳 impreza party|🥸 przebranie disguise|😎 okulary cool|🤓 nerd|🧐 monokl monocle|😕 zmieszany confused|😟 zmartwiony worried|🙁 smutek frown|😮 wow oh|😯 zdziwiony hushed|😲 szok astonished|😳 zawstydzony flushed|🥺 proszę pleading|🥹 wzruszony|😦 frown open|😧 udręka|😨 strach fear|😰 niepokój anxious|😥 smutny ulga|😢 płacze cry|😭 ryczy sob|😱 krzyk scream|😖 zmęczony|😣 wytrwały|😞 zawiedziony disappointed|😓 pot zimny|😩 znużony weary|😫 zmęczony tired|🥱 ziewa yawn|😤 parska triumph|😡 wściekły angry rage|😠 zły angry|🤬 przekleństwa swear|😈 diabełek devil|💀 czaszka skull dead|☠️ trupia|💩 kupa poop|🤡 klaun clown|👻 duch ghost|👽 kosmita alien|🤖 robot|😺 kot cat|🙈 nie widzę monkey|🙉 nie słyszę|🙊 nie mówię|💋 usta kiss|💯 sto hundred|💢 złość anger|💥 bum boom|💫 gwiazdki dizzy|💦 krople sweat|💨 pęd dash|💬 dymek speech|💤 zzz sleep|👋 cześć wave hello|🤚 ręka hand|✋ stop|🖖 vulcan|👌 ok okay|🤌 włoski pinched|🤏 trochę pinch|✌️ pokój peace|🤞 kciuki crossed fingers|🤟 kocham love you|🤘 metal rock|🤙 zadzwoń call|👈 lewo left|👉 prawo right|👆 góra up|👇 dół down|☝️ uwaga point|👍 kciuk tak like yes|👎 nie dislike no|✊ pięść fist|👊 cios punch|🤛 żółwik|👏 brawo clap|🙌 hurra raise|👐 otwarte|🤲 dłonie|🤝 zgoda handshake deal|🙏 proszę dziękuję pray thanks|✍️ pisze write|💪 siła strong muscle|🧠 mózg brain|👀 patrzy eyes look|👁️ oko eye|👅 język|❤️ serce heart love|🧡 pomarańczowe serce|💛 żółte serce|💚 zielone serce|💙 niebieskie serce|💜 fioletowe serce|🖤 czarne serce|🤍 białe serce|💔 złamane serce broken|❤️‍🔥 płonące serce|💕 serduszka hearts|💖 błyszczące serce|✨ iskry sparkles|⭐ gwiazda star|🌟 świeci glow|🔥 ogień fire hot|⚡ błyskawica zap|🌈 tęcza rainbow|☀️ słońce sun|🌙 księżyc moon|❄️ śnieg snow|🌧️ deszcz rain|🎉 tada party konfetti|🎊 konfetti|🎈 balon balloon|🎁 prezent gift|🎂 tort urodziny cake birthday|🏆 puchar trophy|🥇 złoto gold|🎮 gra game|🎵 muzyka music|🎶 nuty notes|🎧 słuchawki headphones|🎤 mikrofon mic|📷 aparat camera|📞 telefon phone|💻 laptop|📱 komórka mobile|⌨️ klawiatura keyboard|🔒 kłódka lock|🔑 klucz key|💡 pomysł idea|📌 pinezka pin|📎 spinacz clip|✅ gotowe done check|❌ nie cross|⚠️ uwaga warning|❓ pytanie question|❗ wykrzyknik|🆗 ok|🆘 pomocy sos|☕ kawa coffee|🍵 herbata tea|🍺 piwo beer|🍷 wino wine|🍕 pizza|🍔 burger|🍟 frytki fries|🌭 hotdog|🍣 sushi|🍩 pączek donut|🍪 ciastko cookie|🍰 ciasto|🍫 czekolada chocolate|🍎 jabłko apple|🍌 banan banana|🍓 truskawka|🥑 awokado|🌶️ papryczka|🍿 popcorn|🐶 pies dog|🐱 kot cat|🐭 mysz|🐰 królik|🦊 lis fox|🐻 miś bear|🐼 panda|🐸 żaba frog|🐵 małpa|🐔 kura|🐧 pingwin|🦆 kaczka duck|🦉 sowa owl|🦄 jednorożec unicorn|🐝 pszczoła bee|🦋 motyl|🐢 żółw|🐍 wąż snake|🐙 ośmiornica|🐟 ryba fish|🌸 kwiat flower|🌹 róża rose|🌻 słonecznik|🌲 drzewo tree|🍀 koniczyna luck|🚀 rakieta rocket|✈️ samolot plane|🚗 auto car|🚲 rower bike|🏠 dom home|⏰ budzik alarm|⏳ czas time|📅 kalendarz|🇵🇱 polska poland`.split("|").map((x) => { const i = x.indexOf(" "); return [x.slice(0, i), x.slice(i + 1)]; });

function emojiPicker(anchor, pick) {
  let recent = [];
  try { recent = JSON.parse(localStorage.getItem("szept.emoji") || "[]"); } catch {}
  const grid = (list) => list.map(([e, w]) => `<button data-e="${e}" title="${esc(w.split(" ")[0])}">${e}</button>`).join("");
  const all = (q) => {
    q = q.trim().toLowerCase();
    if (q) return grid(EMOJI.filter(([, w]) => w.includes(q))) || `<p class="emoji-none">Brak emoji dla „${esc(q)}”</p>`;
    const r = recent.length ? `<h4>Ostatnie</h4><div class="emoji-grid">${grid(recent.map((e) => [e, ""]))}</div><h4>Wszystkie</h4>` : "";
    return r + `<div class="emoji-grid">${grid(EMOJI)}</div>`;
  };
  const el = popover(anchor, `<div class="emoji">
      <div class="search emoji-q"><input placeholder="Szukaj emoji" autocomplete="off">${icon("search")}</div>
      <div class="emoji-body">${all("")}</div></div>`, "top", "emoji-pop");
  if (!el) return;
  const q = $("input", el), b = $(".emoji-body", el);
  q.focus();
  q.addEventListener("input", () => { b.innerHTML = q.value.trim() ? `<div class="emoji-grid">${all(q.value)}</div>` : all(""); });
  q.addEventListener("keydown", (e) => { if (e.key === "Enter") $("[data-e]", b)?.click(); });
  el.addEventListener("click", (e) => {
    const t = e.target.closest("[data-e]");
    if (!t) return;
    const em = t.dataset.e;
    recent = [em, ...recent.filter((x) => x !== em)].slice(0, 14);
    try { localStorage.setItem("szept.emoji", JSON.stringify(recent)); } catch {}
    pick(em); popClose?.();
  });
}

/* ---------------------------- picture crop ---------------------------- */

// pickPicture: file chooser -> pan/zoom framing -> Go. The crop rectangle is
// reported in *source* pixels, which is exactly what attach.Avatar wants.
function pickPicture(kind, chat) {
  popClose?.(); // a profile card would sit on top of the crop dialog
  // attached to the page: WebKit drops the change event of a detached input
  // once nothing else holds on to it
  const input = document.createElement("input");
  input.type = "file";
  input.accept = "image/*";
  input.hidden = true;
  document.body.append(input);
  input.onchange = async () => {
    const file = input.files?.[0];
    input.remove();
    if (!file) return;
    try { await cropDialog(kind, await readAsDataURL(file), chat); } catch (e) { fail(e); }
  };
  input.click();
}

// kind: avatar | banner, or with chat set a group's icon | banner
function cropDialog(kind, dataURL, chat) {
  const round = kind === "avatar" || kind === "icon";
  const ratio = round ? 1 : 3;
  return modal({
    title: chat ? (round ? "Wykadruj ikonę serwera" : "Wykadruj baner serwera") : round ? "Wykadruj awatar" : "Wykadruj baner",
    wide: !round,
    body: `<div class="crop-stage" id="cs"><img id="ci" src="${dataURL}" alt=""><div class="crop-mask ${round ? "round" : ""}" id="cm"></div></div>
      <div class="field"><label>Powiększenie</label>
        <input class="slider" id="cz" type="range" min="1" max="4" step="0.01" value="1"></div>
      <p class="hint-text" style="padding:0 16px">Przeciągnij obraz, żeby przesunąć kadr.</p>`,
    actions: [
      { label: "Anuluj", kind: "ghost", value: null },
      { label: "Zapisz", kind: "primary", get value() { return cropState.rect(); } },
      { label: "Usuń obraz", kind: "danger", value: "clear" },
    ],
    onMount: (box) => setupCrop(box, ratio),
  }).then(async (r) => {
    if (chat) {
      try { if (r) await api("SetServerImage", chat, kind, r === "clear" ? "" : dataURL, r === "clear" ? {} : r); await refresh(); } catch (e) { fail(e); }
      return serverLook(chat);
    }
    if (!r) return;
    try {
      if (r === "clear") await api(round ? "ClearAvatar" : "ClearBanner");
      else await api(round ? "SetAvatar" : "SetBanner", dataURL, r);
      await afterEdit("Zapisane");
    } catch (e) { fail(e); }
  });
}

// serverLook: the owner's editor for a group's icon, banner and decoration.
// The decoration is a draft until "Zapisz"; pictures save straight from the crop.
function serverLook(chat) {
  const g = () => S.snap.chats.find((c) => c.id === chat);
  let st = parseStyle(g()?.style);
  const body = () => {
    const x = g();
    const tiles = FX.d.map((label, v) => `<button class="fx-tile ${st.d === v ? "on" : ""}" data-k="d" data-v="${v}" title="${esc(label)}">
        <span class="sl-icon">${x.avatar ? `<img src="${esc(x.avatar)}" alt="">` : `<span class="ini">${esc(initials(x.name))}</span>`}${serverDeco(styleStr({ ...st, d: v }))}</span><span>${esc(label)}</span></button>`).join("");
    const sw = (k) => `<div class="fx-sw">${PAL.map((c, v) =>
      `<button class="${st[k] === v ? "on" : ""}" data-k="${k}" data-v="${v}" style="--c:${c}" title="Kolor ${v + 1}"></button>`).join("")}</div>`;
    return `<div class="sl">
        <div class="sl-banner" style="--tone:${tint(chat)};${x.banner ? `background-image:url('${esc(x.banner)}')` : ""}">
          <span class="sl-icon big">${x.avatar ? `<img src="${esc(x.avatar)}" alt="">` : `<span class="ini">${esc(initials(x.name))}</span>`}${serverDeco(styleStr(st))}</span></div>
        <div class="btn-row">
          <button class="btn ghost" data-pick="icon">Zmień ikonę</button>
          ${x.avatar ? `<button class="btn ghost" data-clear="icon">Usuń ikonę</button>` : ""}
          <button class="btn ghost" data-pick="banner">Zmień baner</button>
          ${x.banner ? `<button class="btn ghost" data-clear="banner">Usuń baner</button>` : ""}
        </div>
        <div class="fx-row"><label>Dekoracja ikony</label><div class="fx-tiles">${tiles}</div></div>
        <div class="fx-row"><label>Kolory obwódki</label><div class="fx-cols">${sw("a")}${sw("b")}</div></div>
      </div>`;
  };
  return modal({
    title: "Wygląd serwera", wide: true, body: body(),
    actions: [{ label: "Anuluj", kind: "ghost", value: null }, { label: "Zapisz dekorację", kind: "primary", get value() { return styleStr(st); } }],
    onMount: (box) => box.addEventListener("click", async (e) => {
      const t = e.target.closest("[data-k], [data-pick], [data-clear]");
      if (!t) return;
      if (t.dataset.pick) return pickPicture(t.dataset.pick, chat);
      if (t.dataset.clear) {
        try { await api("SetServerImage", chat, t.dataset.clear, "", {}); await refresh(); } catch (err) { fail(err); }
      } else st[t.dataset.k] = +t.dataset.v;
      $(".body", box).innerHTML = body();
    }),
  }).then(async (v) => {
    if (v === null || v === (g()?.style || "0.0.0.0.0.0.0")) return;
    try { await api("SetServerStyle", chat, v); await afterEdit("Zapisane"); } catch (e) { fail(e); }
  });
}

const cropState = { rect: () => ({ X: 0, Y: 0, W: 0, H: 0 }) };

function setupCrop(box, ratio) {
  const stage = $("#cs", box), img = $("#ci", box), mask = $("#cm", box), zoom = $("#cz", box);
  let fw = 0, fh = 0, base = 1, scale = 1, tx = 0, ty = 0, dragging = false;

  const layout = () => {
    const sw = stage.clientWidth, sh = stage.clientHeight;
    fw = Math.min(sw - 32, (sh - 32) * ratio);
    fh = fw / ratio;
    Object.assign(mask.style, {
      left: `${(sw - fw) / 2}px`, top: `${(sh - fh) / 2}px`,
      width: `${fw}px`, height: `${fh}px`, inset: "auto",
    });
    base = Math.max(fw / img.naturalWidth, fh / img.naturalHeight);
    apply();
  };

  // Dragged past the edge the picture keeps following the pointer, just less
  // and less: a hard stop reads as "frozen", progressive resistance reads as
  // "still responding, there is simply nothing more here".
  const rubber = (over, dim) => (over * dim * 0.55) / (dim + 0.55 * Math.abs(over));
  const give = (v, max, dim) =>
    (Math.abs(v) <= max ? v : Math.sign(v) * (max + rubber(Math.abs(v) - max, dim)));

  const apply = (settle = false) => {
    scale = base * parseFloat(zoom.value);
    const dw = img.naturalWidth * scale, dh = img.naturalHeight * scale;
    const mx = Math.max(0, (dw - fw) / 2), my = Math.max(0, (dh - fh) / 2);
    if (!dragging) { // release and zoom both pull the frame back inside the picture
      tx = Math.min(mx, Math.max(-mx, tx));
      ty = Math.min(my, Math.max(-my, ty));
    }
    img.style.transition = settle ? "left .32s var(--ease), top .32s var(--ease)" : "none";
    Object.assign(img.style, {
      width: `${dw}px`, height: `${dh}px`,
      left: `${(stage.clientWidth - dw) / 2 + (dragging ? give(tx, mx, fw) : tx)}px`,
      top: `${(stage.clientHeight - dh) / 2 + (dragging ? give(ty, my, fh) : ty)}px`,
    });
  };

  cropState.rect = () => {
    const w = Math.round(fw / scale), h = Math.round(fh / scale);
    const cx = img.naturalWidth / 2 - tx / scale, cy = img.naturalHeight / 2 - ty / scale;
    const clamp = (v, hi) => Math.max(0, Math.min(hi, Math.round(v)));
    return { X: clamp(cx - w / 2, img.naturalWidth - w), Y: clamp(cy - h / 2, img.naturalHeight - h), W: w, H: h };
  };

  if (img.complete && img.naturalWidth) layout(); else img.onload = layout;
  zoom.addEventListener("input", apply);

  let px = 0, py = 0;
  stage.addEventListener("pointerdown", (e) => {
    dragging = true; px = e.clientX; py = e.clientY; stage.setPointerCapture(e.pointerId);
  });
  stage.addEventListener("pointermove", (e) => {
    if (!dragging) return;
    tx += e.clientX - px; ty += e.clientY - py; px = e.clientX; py = e.clientY;
    apply();
  });
  const release = () => { if (!dragging) return; dragging = false; apply(true); };
  stage.addEventListener("pointerup", release);
  stage.addEventListener("pointercancel", release);
  stage.addEventListener("wheel", (e) => {
    e.preventDefault();
    zoom.value = Math.max(1, Math.min(4, parseFloat(zoom.value) - e.deltaY / 600));
    apply();
  }, { passive: false });
}

/* ============================ settings ============================ */

const SECTIONS = [
  { g: "Ustawienia użytkownika" },
  { id: "profile", t: "Profil", d: "nazwa status awatar baner opis kod", i: "user" },
  { id: "accounts", t: "Konta", d: "profile na tym urządzeniu katalog", i: "people" },
  { id: "security", t: "Hasło i kopia", d: "backup restore hasło", i: "shield" },
  { id: "connector", t: "Connector", d: "serwer spotkań listy ip przekaźnik", i: "server" },
  { g: "Ustawienia aplikacji" },
  { id: "voice", t: "Głos i wideo", d: "mikrofon głośnik głośność kamera dźwięki powiadomienia", i: "volume" },
  { id: "look", t: "Wygląd", d: "motyw kolor akcentu jasny ciemny", i: "palette" },
  { id: "lang", t: "Język", d: "pl en polski english", i: "globe" },
  { id: "bots", t: "Boty", d: "webhook http token", i: "bot" },
  { g: "" },
  { id: "donate", t: "Wesprzyj", d: "donate dotacja wsparcie pieniądze", i: "heart" },
  { id: "about", t: "O szepcie", d: "wersja licencja", i: "chat" },
];

// Settings float over the app as one big sheet: the chat stays underneath and
// keeps syncing, so closing it lands exactly where you were.
async function openSettings(section = "profile") {
  closeModal(null);
  S.screen = "settings";
  S.section = section;
  S.setQ = "";
  S.setPaged = false;
  S.settings = await api("Settings");
  renderSettings();
}

// Unsaved edits hold the layer open: the bar shakes instead, the way it says
// "save or reset first".
function holdUnsaved() {
  if (!S.dirty) return false;
  const bar = $("#unsaved");
  bar.classList.remove("shake"); void bar.offsetWidth; bar.classList.add("shake");
  return true;
}

function closeSettings() {
  if (holdUnsaved()) return;
  S.draftStyle = undefined;
  S.screen = "app";
  if (S.micTest) micTest(false);
  const layer = $(".set-layer");
  if (!layer) return;
  layer.classList.add("out");
  setTimeout(() => { if (S.screen === "app") $("#set-root").innerHTML = ""; }, 200);
}

const ACCENTS = ["2f8a8a", "4a6fa5", "5f7a4a", "b0703a", "8a5a78", "6b7280"];

function settingsBody() {
  const s = S.settings, me = S.snap.me;
  switch (S.section) {
    case "profile": return `
      <h2>Profil</h2><p class="lead">Wszystko tutaj jest podpisane twoim kluczem i trafia do każdego czatu, w którym jesteś.</p>
      <div class="block">
        <div class="card-banner own" data-act="edit-banner" title="Zmień baner" style="--tone:${tint(me.pub)};${me.banner ? `background-image:url('${esc(me.banner)}')` : ""}">
          <span class="icon-btn edit">${icon("edit")}</span>
        </div>
        <div class="card-id">
          <button data-act="edit-avatar">${avatar(me.avatar, me.name, me.pub, "xl")}</button>
          <div class="nm">${esc(me.name)}</div>
          <div class="st">${esc(me.status || "bez statusu")}</div>
        </div>
        <div class="btn-row">
          <button class="btn" data-act="edit-nick">${icon("edit")}Nazwa</button>
          <button class="btn" data-act="edit-status">${icon("smile")}Status</button>
          <button class="btn" data-act="edit-bio">${icon("edit")}Opis</button>
        </div>
      </div>
      ${fxEditor(me)}
      <div class="block">
        <h4>Twój kod</h4>
        <p class="hint-text">Wizytówka: klucz publiczny plus podpowiedź, przez który connector cię szukać.</p>
        <div class="code-box" style="margin:12px 16px">${esc(me.code)}</div>
        ${keyBlock(me.pub)}
        <div class="btn-row"><button class="btn" data-act="copy" data-text="${esc(me.code)}">${icon("copy")}Kopiuj</button>
          <button class="btn ghost" data-act="my-code">${icon("qr")}QR</button></div>
      </div>`;

    case "connector": return `
      <h2>Connector</h2><p class="lead">Lekki serwer spotkań: kojarzy peerów i przekazuje sygnalizację. Treści nie widzi: ruch idzie P2P i jest szyfrowany.</p>
      <div class="block">
        <h4>Aktywny</h4>
        <div class="field"><input class="input" id="s-conn" value="${esc(s.connector)}" placeholder="ws://twoj-vps:7777"></div>
        <p class="hint-text" style="padding:0 16px">Stan: ${me.connUp ? "połączony" : esc(me.connErr || "łączenie…")}</p>
        <div class="btn-row"><button class="btn primary" data-act="save-conn">Zapisz i połącz</button></div>
      </div>
      <div class="block">
        <div class="toggle"><div class="tx"><b>Ukryj mój adres IP</b><span>połączenia idą tylko przez przekaźnik connectora, rozmówcy widzą jego adres zamiast twojego. Wolniej. ${me.connRelay ? "Ten connector ma przekaźnik." : "Ten connector nie ma przekaźnika. Z włączoną opcją nie połączysz się z nikim."}</span></div>
          <button class="switch ${s.relayOnly ? "on" : ""}" data-act="toggle-relay"></button></div>
        <div class="toggle"><div class="tx"><b>Ukryj listę kontaktów przed connectorem</b><span>Normalnie aplikacja co 30 s pyta connector o każdy kontakt, więc connector zna całą Twoją listę kontaktów. Z tą opcją pyta tylko o osoby z czatów otwartych albo używanych w ostatnich 15 minutach. Kropki obecności pozostałych kontaktów nie będą aktualne.</span></div>
          <button class="switch ${s.quietLookup ? "on" : ""}" data-act="toggle-quiet"></button></div>
      </div>
      <div class="block">
        <h4>Zapisane adresy</h4>
        ${s.connectors.length
          ? s.connectors.map((c) => `<div class="list-line"><span class="mono">${esc(c)}</span>
              ${c === s.connector ? `<span class="tag">aktywny</span>` : `<button class="btn ghost" data-act="use-conn" data-v="${esc(c)}">Użyj</button>`}
              <button class="icon-btn" data-act="del-conn" data-v="${esc(c)}">${icon("trash")}</button></div>`).join("")
          : `<p class="hint-text" style="padding:0 16px">Lista pusta.</p>`}
        <div class="field"><label>Dodaj</label><input class="input" id="s-newconn" placeholder="ws://inny-vps:7777"></div>
        <div class="btn-row"><button class="btn" data-act="add-conn">${icon("plus")}Dodaj</button></div>
      </div>
      <div class="block">
        <h4>Dynamiczne listy</h4>
        <p class="hint-text">Adres HTTP zwracający tekst, jeden connector (ws://…) na linię.</p>
        ${s.connectorLists.map((l) => `<div class="list-line"><span class="mono">${esc(l)}</span>
            <button class="icon-btn" data-act="del-list" data-v="${esc(l)}">${icon("trash")}</button></div>`).join("")}
        <div class="field"><label>Dodaj listę</label><input class="input" id="s-newlist" placeholder="https://example.com/connectors.txt"></div>
        <div class="btn-row"><button class="btn" data-act="add-list">${icon("plus")}Dodaj</button>
          ${s.connectorLists.length ? `<button class="btn ghost" data-act="refresh-lists">Odśwież teraz</button>` : ""}</div>
      </div>`;

    case "voice": {
      if (!S.devices) api("AudioDevices").then((d) => { S.devices = d; if (S.section === "voice") renderSettings(); }).catch(fail);
      const pick = (id, list, cur) => `<div class="select-wrap"><select class="input" id="${id}">
          <option value="">Domyślne systemowe</option>
          ${(list || []).map((d) => `<option value="${esc(d.id)}" ${d.id === cur ? "selected" : ""}>${esc(d.name)}</option>`).join("")}
        </select></div>`;
      return `
      <h2>Głos i wideo</h2><p class="lead">Rozmowy idą pełną siatką P2P: każdy wysyła do każdego. Wygodnie do ok. ośmiu osób.</p>
      <div class="block">
        <div class="dev-row">
          <div class="field"><label>Mikrofon</label>${pick("s-mic", S.devices?.mics.filter((d) => !d.name.startsWith("Monitor of ")), s.micDevice)}</div>
          <div class="field"><label>Głośnik</label>${pick("s-spk", S.devices?.speakers, s.speakerDevice)}</div>
        </div>
        <p class="hint-text" style="padding:0 16px">Zmiana urządzenia działa od następnej rozmowy.</p>
        <div class="btn-row"><button class="btn primary" data-act="save-settings">Zapisz</button></div>
      </div>
      <div class="block">
        <h4>Głośność mikrofonu</h4>
        <div class="field"><input class="slider" id="s-gain" type="range" min="0" max="2" step="0.05" value="${s.micGain || 1}"></div>
        <p class="hint-text" style="padding:0 16px" id="s-gain-l">${Math.round((s.micGain || 1) * 100)}%</p>
        <div class="mic-test">
          <button class="btn ${S.micTest ? "" : "primary"}" data-act="mic-test">${S.micTest ? "Zatrzymaj test" : "Test mikrofonu"}</button>
          <div class="meter" id="meter"><i></i></div>
        </div>
        <p class="hint-text" style="padding:0 16px 8px">Powiedz coś: pasek pokazuje, jak głośno inni Cię słyszą. Nic nie jest wysyłane.</p>
      </div>
      <div class="block">
        <h4>Głośność rozmowy</h4>
        <div class="field"><input class="slider" id="s-vol" type="range" min="0" max="2" step="0.05" value="${s.outputVol || 1}"></div>
        <p class="hint-text" style="padding:0 16px" id="s-vol-l">${Math.round((s.outputVol || 1) * 100)}%</p>
      </div>
      <div class="block">
        <h4>Kamera</h4>
        <div class="field"><label>Urządzenie</label>
          <input class="input" id="s-cam" value="${esc(s.camDevice)}" placeholder="/dev/video0"></div>
        <p class="hint-text" style="padding:0 16px">Puste = pierwsza znaleziona kamera.</p>
        <div class="btn-row"><button class="btn primary" data-act="save-settings">Zapisz</button></div>
      </div>
      <div class="block">
        <div class="toggle"><div class="tx"><b>Powiadomienia systemowe</b><span>gdy przychodzi wiadomość w innym czacie</span></div>
          <button class="switch ${s.noNotify ? "" : "on"}" data-act="toggle-notify"></button></div>
      </div>`;
    }

    case "look": return `
      <h2>Wygląd</h2><p class="lead">Jeden kolor akcentu przechodzi przez cały interfejs: przyciski, dymki, wskaźniki.</p>
      <div class="block">
        <h4>Motyw</h4>
        <p class="hint-text">Automatyczny idzie za ustawieniem pulpitu, więc wieczorem sam ściemnieje.</p>
        <div class="segmented">
          ${[["auto", "Automatyczny"], ["light", "Jasny"], ["dark", "Ciemny"]].map(([v, t]) =>
            `<button class="${themePref === v ? "sel" : ""}" data-act="theme" data-v="${v}">${t}</button>`).join("")}
        </div>
      </div>
      <div class="block">
        <h4>Kolor akcentu</h4>
        <div class="swatches">${ACCENTS.map((c) =>
          `<button class="swatch ${(s.accent || "2f8a8a").toLowerCase() === c ? "sel" : ""}" style="background:#${c}" data-act="accent" data-v="${c}"></button>`).join("")}</div>
        <div class="field"><label>Własny (hex)</label><input class="input" id="s-accent" value="${esc(s.accent || "2f8a8a")}" placeholder="2f8a8a"></div>
        <div class="btn-row"><button class="btn primary" data-act="save-accent">Zastosuj</button></div>
      </div>
      <div class="block">
        <div class="toggle"><div class="tx"><b>Tryb dewelopera</b><span>menu kontekstowe przeglądarki i inspektor. Inspektor działa po restarcie aplikacji</span></div>
          <button class="switch ${s.devMode ? "on" : ""}" data-act="toggle-dev"></button></div>
      </div>`;

    case "lang": return `
      <h2>Język</h2><p class="lead">Dotyczy komunikatów wysyłanych do innych (np. „dodał X do czatu”).</p>
      <div class="block">
        <div class="field"><label>Język</label>
          <div class="select-wrap"><select class="input" id="s-lang">
            <option value="pl" ${s.lang === "pl" ? "selected" : ""}>polski</option>
            <option value="en" ${s.lang === "en" ? "selected" : ""}>english</option>
          </select></div></div>
        <div class="btn-row"><button class="btn primary" data-act="save-settings">Zapisz</button></div>
      </div>`;

    case "accounts": return `
      <h2>Konta</h2><p class="lead">Każde konto to osobny katalog danych i osobna tożsamość. Przełączenie restartuje aplikację.</p>
      <div class="block">
        ${s.pinned
          ? `<p class="hint-text" style="padding:0 16px">Aplikacja działa z <span class="mono">-data</span>, konta są wyłączone.</p>`
          : s.profiles.map((p) => `<div class="list-line"><span class="mono">${esc(p || "domyślne")}</span>
              ${p === s.profile ? `<span class="tag">aktywne</span>`
                : `<button class="btn ghost" data-act="use-profile" data-v="${esc(p)}">Przełącz</button>`}</div>`).join("")}
        ${s.pinned ? "" : `<div class="btn-row"><button class="btn" data-act="new-profile">${icon("plus")}Nowe konto</button></div>`}
      </div>
      <div class="block">
        <h4>Katalog danych</h4>
        <div class="code-box" style="margin:6px 16px 12px">${esc(s.dataDir)}</div>
      </div>`;

    case "security": return `
      <h2>Hasło i kopia</h2><p class="lead">Baza jest szyfrowana losowym kluczem, który chroni hasło. Zmiana hasła losuje nowy klucz i przepisuje bazę.</p>
      <div class="block">
        <h4>Hasło klucza</h4>
        <div class="field"><label>Stare hasło</label><input class="input" id="p-old" type="password" placeholder="(puste jeśli nie było)"></div>
        <div class="field"><label>Nowe hasło</label><input class="input" id="p-new" type="password"></div>
        <div class="btn-row"><button class="btn primary" data-act="change-pass">Zmień hasło</button></div>
      </div>
      <div class="block">
        <h4>Kopia zapasowa</h4>
        <p class="hint-text">Zip z kluczem tożsamości i spójną migawką bazy. Bez hasła kopia jest bezużyteczna dla znalazcy.</p>
        <div class="btn-row">
          <button class="btn" data-act="backup">${icon("save")}Zapisz kopię</button>
          <button class="btn ghost" data-act="restore">${icon("file")}Przywróć z kopii</button>
        </div>
        <p class="hint-text" style="padding:0 16px 8px">Przywracanie nigdy nie nadpisuje obecnych danych. Kopia ląduje jako nowe konto.</p>
      </div>`;

    case "bots": return `
      <h2>Boty</h2><p class="lead">Lokalny webhook HTTP: skrypt na tym komputerze może wrzucać wiadomości do czatów.</p>
      <div class="block">
        <div class="toggle"><div class="tx"><b>Webhook</b><span>nasłuch na localhost</span></div>
          <button class="switch ${s.webhookOn ? "on" : ""}" data-act="toggle-webhook"></button></div>
        <div class="field"><label>Port</label><input class="input" id="w-port" type="number" value="${s.webhookPort || 8787}"></div>
        <div class="field"><label>Token</label><input class="input" id="w-token" value="${esc(s.webhookToken)}" placeholder="wygeneruj"></div>
        <div class="btn-row">
          <button class="btn ghost" data-act="gen-token">Nowy token</button>
          <button class="btn primary" data-act="save-webhook">Zapisz</button>
        </div>
      </div>`;

    case "donate": return `
      <h2>Wesprzyj szepta</h2><p class="lead">Szept nie ma reklam, kont ani serwera, który by na tobie zarabiał.</p>
      <div class="block">
        <div class="quiet"><i class="empty-ico">${icon("heart")}</i><b>Wkrótce</b>Sposoby wsparcia pojawią się tutaj w jednej z następnych wersji.</div>
      </div>`;

    default: return `
      <h2>O szepcie</h2>
      <div class="block">
        <div class="card-id" style="padding-top:16px;margin-top:0">
          <div class="nm">szept</div>
          <div class="st">wersja ${esc(S.version || "dev")}</div>
          <div class="bio">Szyfrowany komunikator P2P. Historia to podpisany DAG zdarzeń,
transport to WebRTC (pion), a stan leży lokalnie w bbolt.
Connector kojarzy peerów i nie widzi treści.</div>
        </div>
      </div>
      <div class="block">
        <h4>Licencja</h4>
        <p class="hint-text">Szept to wolne oprogramowanie na licencji GNU GPL w wersji 3 lub dowolnej późniejszej
(GPL-3.0-or-later). Możesz go używać, zmieniać i rozpowszechniać na jej warunkach.</p>
        <p class="hint-text">Program jest dostarczany „tak jak jest”, <b>bez żadnej gwarancji</b>, w tym gwarancji
bezpieczeństwa i przydatności do określonego celu, w zakresie dozwolonym przez prawo.
Szczegóły w punktach 15 i 16 licencji: https://www.gnu.org/licenses/gpl-3.0.html</p>
      </div>`;
  }
}

// repaintFx swaps only the effects block, so the page keeps its scroll.
function repaintFx() {
  const old = $(".block.fx");
  if (!old) return renderSettings();
  const t = document.createElement("template");
  t.innerHTML = fxEditor(S.snap.me);
  old.replaceWith(t.content);
}

// fxEditor: name style, avatar decoration, card frame and effect, with a live
// card preview. Nothing leaves the device until "Zapisz".
function fxEditor(me) {
  const cur = S.draftStyle ?? me.style ?? "";
  const st = parseStyle(cur);
  const dirty = S.draftStyle !== undefined && S.draftStyle !== (me.style || "");
  const tiles = (k, list, draw) => `<div class="fx-tiles">${list.map((label, v) =>
    `<button class="fx-tile ${st[k] === v ? "on" : ""}" data-act="fx-pick" data-k="${k}" data-v="${v}" title="${esc(label)}">
      ${draw(v)}<span>${esc(label)}</span></button>`).join("")}</div>`;
  const sw = (k) => `<div class="fx-sw">${PAL.map((c, v) =>
    `<button class="${st[k] === v ? "on" : ""}" data-act="fx-pick" data-k="${k}" data-v="${v}" style="--c:${c}" title="Kolor ${v + 1}"></button>`).join("")}</div>`;
  const one = (o) => styleStr({ ...st, f: 0, e: 0, d: 0, r: 0, x: 0, ...o });
  return `
    <div class="block fx">
      <h4>Efekty profilu</h4>
      <p class="hint-text">Widzą je wszyscy w twoich czatach. Animacje wyłączają się, gdy system prosi o mniej ruchu.</p>
      <div class="fx-grid">
        <div class="fx-opts">
          <div class="fx-row"><label>Czcionka nazwy</label>${tiles("f", FX.f, (v) => `<b class="fx-name"${nameFx("", one({ f: v, e: st.e }))}>Aa</b>`)}</div>
          <div class="fx-row"><label>Efekt nazwy</label>${tiles("e", FX.e, (v) => `<b class="fx-name"${nameFx("", one({ f: st.f, e: v }))}>Aa</b>`)}</div>
          <div class="fx-row"><label>Kolory</label><div class="fx-cols">${sw("a")}${sw("b")}</div></div>
          <div class="fx-row"><label>Dekoracja awatara</label>${tiles("d", FX.d, (v) => avatar(me.avatar, me.name, me.pub, "", one({ d: v })))}</div>
          <div class="fx-row"><label>Ramka karty</label>${tiles("r", FX.r, (v) => `<i class="fx-mini pcard" ${cardFx(one({ r: v }))}></i>`)}</div>
          <div class="fx-row"><label>Efekt karty</label>${tiles("x", FX.x, (v) => `<i class="fx-mini pcard" ${cardFx(one({ x: v }))}></i>`)}</div>
        </div>
        <div class="fx-preview">
          <div class="pcard" ${cardFx(cur)}>
            <div class="card-banner" style="--tone:${tint(me.pub)};${me.banner ? `background-image:url('${esc(me.banner)}')` : ""}"></div>
            <div class="card-id">
              <div class="av-wrap online">${avatar(me.avatar, me.name, me.pub, "xl", cur)}<i class="presence"></i></div>
              <div class="nm"><span${nameFx("", cur)}>${esc(me.name)}</span></div>
              <div class="st">${esc(me.status || "online")}</div>
            </div>
          </div>
        </div>
      </div>
      <div class="btn-row">
        <button class="btn primary" data-act="fx-save" ${dirty ? "" : "disabled"}>Zapisz efekty</button>
        ${dirty ? `<button class="btn ghost" data-act="fx-reset">Cofnij</button>` : ""}
        <button class="btn ghost" data-act="fx-clear">Bez efektów</button>
      </div>
    </div>`;
}

function setNav() {
  const q = S.setQ.toLowerCase();
  const hit = (x) => !q || (x.t + " " + x.d).toLowerCase().includes(q);
  return SECTIONS.map((x, i) => {
    if ("g" in x) {
      const any = SECTIONS.slice(i + 1).find((y) => "g" in y || hit(y));
      if (!any || "g" in any) return "";
      return x.g ? `<div class="set-group">${esc(x.g)}</div>` : `<div class="set-sep"></div>`;
    }
    return hit(x) ? `<button class="set-row ${x.id === S.section ? "active" : ""}" data-act="section" data-id="${x.id}">
        ${icon(x.i)}<span>${esc(x.t)}</span></button>` : "";
  }).join("");
}

function renderSettings() {
  const me = S.snap.me;
  const root = $("#set-root");
  const fresh = !$(".set-layer", root);
  if (fresh) {
    root.innerHTML = `
      <div class="set-layer">
        <div class="settings" role="dialog" aria-modal="true" aria-label="Ustawienia">
          <nav class="set-nav">
            <button class="icon-btn set-nav-close" data-act="close-settings" title="Zamknij">${icon("x")}</button>
            <button class="set-me" data-act="section" data-id="profile">
              ${avatar(me.avatar, me.name, me.pub)}
              <span><b>${esc(me.name)}</b><span>Edytuj profil</span></span>
            </button>
            <div class="search">${icon("search")}<input id="set-q" placeholder="Szukaj"></div>
            <div id="set-rows"></div>
          </nav>
          <div class="set-body">
            <button class="icon-btn set-back" data-act="set-back" title="Wróć">${icon("chev")}</button>
            <div class="wrap" id="set-wrap"></div>
            <div class="set-close"><button class="round-btn" data-act="close-settings" title="Zamknij">${icon("x")}</button><span>Esc</span></div>
          </div>
          <div class="unsaved" id="unsaved"><span>Masz niezapisane zmiany</span>
            <button class="btn link" data-act="unsaved-reset">Cofnij</button>
            <button class="btn save" data-act="unsaved-save">Zapisz zmiany</button></div>
        </div>
      </div>`;
    $("#set-wrap", root).addEventListener("input", (e) => {
      if (!e.target.matches(".input")) return;
      S.dirty = e.target.closest(".block");
      $("#unsaved").classList.add("show");
    });
    // the block's own save button counts as saving, same as the bar's
    $("#set-wrap", root).addEventListener("click", (e) => {
      const b = e.target.closest(".btn.primary");
      if (!b || !S.dirty?.contains(b)) return;
      S.dirty = null;
      $("#unsaved").classList.remove("show", "shake");
    });
    const q$ = $("#set-q", root);
    q$.addEventListener("input", () => { S.setQ = q$.value; $("#set-rows").innerHTML = setNav(); });
  }
  $("#set-rows").innerHTML = setNav();
  $("#set-wrap").innerHTML = settingsBody();
  if (!fresh) appear($("#set-wrap"), S.section);
  else $("#set-wrap").dataset.vk = S.section;
  S.dirty = null;
  $("#unsaved").classList.remove("show", "shake");
  $(".settings", root).classList.toggle("paged", !!S.setPaged);
  if (!fresh) $(".set-body").scrollTop = 0;

  const gain = $("#s-gain");
  gain?.addEventListener("input", () => {
    $("#s-gain-l").textContent = Math.round(gain.value * 100) + "%";
    S.settings.micGain = parseFloat(gain.value);
    api("SetMicGain", S.settings.micGain).catch(() => {});
  });
  if (S.micTest && S.section !== "voice") micTest(false);

  const vol = $("#s-vol");
  vol?.addEventListener("input", () => {
    $("#s-vol-l").textContent = Math.round(vol.value * 100) + "%";
    api("SetOutputVolume", parseFloat(vol.value)).catch(() => {});
  });
}

/* ============================ settings actions ============================ */

// micTest: the level meter in the voice settings. It reads your own entry of
// VoiceLevels, so in a call it shows the call's microphone instead.
let meterTimer = 0;
async function micTest(on) {
  clearInterval(meterTimer);
  S.micTest = on;
  try { await api("MicTest", on); } catch (e) { S.micTest = false; fail(e); }
  if (S.screen === "settings") renderSettings();
  if (!S.micTest) return;
  meterTimer = setInterval(async () => {
    const lv = (await api("VoiceLevels").catch(() => ({})))[S.snap.me.pub] || 0;
    $("#meter")?.style.setProperty("--lvl", Math.min(1, lv * 2.5).toFixed(3));
  }, 80);
}

async function saveSettings(patch = {}) {
  const s = { ...S.settings, ...patch };
  try {
    await api("SaveSettings", s);
    toast("Zapisane", "ok");
  } catch (e) {
    fail(e);
  }
  S.settings = await api("Settings");
  applyAccent(S.settings.accent);
  await refresh();
  if (S.screen === "settings") renderSettings();
}

// The accent is the user's to pick, so readability on top of it has to be
// derived, not assumed: white on amber is unreadable, black on indigo is worse.
// Ink follows the accent's luminance, and when the ink is white the filled
// surfaces are deepened until the label actually carries.
function applyAccent(hex) {
  const clean = /^[0-9a-fA-F]{6}$/.test(hex || "") ? hex : "2f8a8a";
  const lin = (c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
  const [r, g, b] = [0, 2, 4].map((i) => lin(parseInt(clean.slice(i, i + 2), 16) / 255));
  const pale = 0.2126 * r + 0.7152 * g + 0.0722 * b > 0.32;

  const css = document.documentElement.style;
  css.setProperty("--accent", "#" + clean);
  css.setProperty("--on-accent", pale ? "#101319" : "#fff");
  css.setProperty("--accent-fill", pale ? "#" + clean : `color-mix(in oklab, #${clean} 82%, #000)`);
}

/* ============================ actions ============================ */

// leaveChat closes the open chat and lands on a sidebar or the friends page.
function leaveChat(to) {
  Object.assign(S, { chat: "", ch: "", voice: null, search: null, filter: "" }, to);
  if (S.focus) setFocus(null);
  api("Open", "").catch(() => {});
  refresh();
}

// openChat with no channel lands on the default text channel, or on the first
// live one once the default is archived or deleted.
async function openChat(id, ch) {
  if (!id) return;
  if (ch === undefined) {
    const g = S.snap?.chats.find((c) => c.id === id);
    const live = g?.channels.filter((x) => x.kind === "text" && !x.arch) || [];
    ch = g && !g.direct && !live.some((x) => !x.id) && live[0] ? live[0].id : "";
  }
  if (id !== S.chat) {
    const n = S.snap?.chats.find((c) => c.id === id)?.unread || 0;
    S.newMark = n ? { chat: id, n } : null;
  }
  if (id !== S.chat || ch !== S.ch) S.builtChat = undefined; // another channel, another message list
  if (id !== S.chat || ch !== S.ch) S.reply = null;
  S.chat = id;
  S.ch = ch;
  S.voice = null;
  if (S.focus) setFocus(null);
  await api("OpenChannel", id, ch);
  S.search = null;
  S.limit = 60;
  S.msgSig = "";
  await api("Open", id);
  await refresh();
  $("#input")?.focus();
}

// afterEdit: a profile change is not pushed by core.Subscribe fast enough to
// beat the user's eye, and the settings view deliberately ignores background
// syncs — so pull once and repaint whatever is on screen.
async function afterEdit(msg) {
  if (msg) toast(msg, "ok");
  await refresh();
  if (S.screen === "settings") renderSettings();
}

async function refreshConv() {
  if (!S.chat) { S.conv = null; return; }
  try {
    S.conv = await api("Conversation", S.chat, S.limit);
  } catch {
    S.conv = null;
    S.chat = "";
  }
}

/* ---------------------------- sounds ---------------------------- */

// Short synthesized cues, no audio files: [frequency Hz, start s] per note.
// Rising = something started, falling = it stopped.
const SFX = {
  join: [[523, 0], [784, .1]],
  leave: [[784, 0], [523, .1]],
  peerJoin: [[659, 0], [988, .08]],
  peerLeave: [[988, 0], [659, .08]],
  mute: [[440, 0], [330, .06]],
  unmute: [[330, 0], [440, .06]],
  deaf: [[392, 0], [294, .07], [220, .14]],
  undeaf: [[220, 0], [294, .07], [392, .14]],
  streamOn: [[587, 0], [740, .07], [880, .14]],
  streamOff: [[880, 0], [740, .07], [587, .14]],
  msg: [[880, 0], [1320, .05]],
};
// Cues are synthesized in Go, so they play on the speaker picked in the
// settings; WebKitGTK cannot route WebAudio to a chosen device.
function sfx(name) { api("Tones", SFX[name]).catch(() => {}); }

// callSounds diffs the call you are in between two snapshots, so a cue plays
// however the change happened: button, slash command or the peer's side.
let lastCall = null, lastUnread = null;
function callSounds(snap) {
  const c = snap.chats.find((x) => x.call);
  const now = c ? { id: c.id, ...c.call, peers: c.inCall.length } : null;
  const was = lastCall;
  lastCall = now;
  if (lastUnread !== null && snap.unread > lastUnread) sfx("msg");
  lastUnread = snap.unread;
  if (!was && !now) return;
  if (!was || !now || was.id !== now.id) return sfx(now ? "join" : "leave");
  if (now.deaf !== was.deaf) sfx(now.deaf ? "deaf" : "undeaf");
  else if (now.mic !== was.mic) sfx(now.mic ? "unmute" : "mute");
  if (now.screen !== was.screen) sfx(now.screen ? "streamOn" : "streamOff");
  else if (now.cam !== was.cam) sfx(now.cam ? "streamOn" : "streamOff");
  if (now.peers !== was.peers) sfx(now.peers > was.peers ? "peerJoin" : "peerLeave");
}

let refreshing = false, again = false;

async function refresh() {
  if (refreshing) { again = true; return; }
  refreshing = true;
  try {
    S.snap = await api("Snapshot");
    callSounds(S.snap);
    warnRelayed(S.snap.friends);
    await refreshConv();
    paint();
  } catch (e) {
    if (errText(e) !== "nie zalogowano") fail(e);
  } finally {
    refreshing = false;
    if (again) { again = false; refresh(); }
  }
}

function paintStatus() {
  const bar = $("#tb-status");
  const me = S.snap?.me;
  if (!me) { bar.hidden = true; return; }
  bar.hidden = false;
  const state = me.connErr ? "err" : me.connUp ? "up" : "wait";
  bar.dataset.state = state;
  // the raw dial error is for whoever debugs it, not for the title bar
  $(".txt", bar).textContent = me.connErr
    ? "Brak połączenia z connectorem"
    : me.connUp ? "połączony · " + me.connector : "łączenie z connectorem…";
  bar.title = me.connErr || "";
  $("#tb-sub").textContent = me.profile ? "· " + me.profile : "";
  paintUserbar(me, state);
}

// The account strip under the list: who you are, whether the connector
// carries you, and the way into settings.
function paintUserbar(me, state) {
  const host = $("#userbar");
  if (!host) return;
  // mic and headphones live here like on Discord: in a call they drive the
  // call, outside one they are what the next call starts with
  const k = S.snap.chats.find((c) => c.call)?.call;
  const mic = k ? k.mic : !S.selfMute, deaf = k ? k.deaf : S.selfDeaf;
  const sig = [me.avatar, me.name, me.status, me.style, state, mic, deaf].join("\0");
  if (host.dataset.sig === sig) return;
  host.dataset.sig = sig;
  host.innerHTML = `
    <button class="ub-id" data-act="profile" data-pub="${me.pub}" title="Mój profil">
      ${presenceAvatar(me.avatar, me.name, me.pub, state === "up", "s")}
      <span class="nm"><b${nameFx(me.pub)}>${esc(me.name)}</b><span>${esc(me.status || (state === "up" ? "online" : "offline"))}</span></span>
    </button>
    <button class="icon-btn ${mic ? "" : "off"}" data-act="ub-mic" title="${mic ? "Wycisz mikrofon" : "Włącz mikrofon"}">${icon(mic ? "mic" : "micOff")}</button>
    <button class="icon-btn ${deaf ? "off" : ""}" data-act="ub-deaf" title="${deaf ? "Włącz dźwięk" : "Wyłącz dźwięk"}">${icon(deaf ? "deaf" : "volume")}</button>
    <button class="icon-btn" data-act="settings" title="Ustawienia">${icon("gear")}</button>`;
}

// paint is the single repaint entry point. Each pane has its own signature so
// a background sync only touches what actually moved — settings and the
// composer are never rebuilt underneath the user's cursor.
function paint() {
  if (S.stage !== "ready") return;
  paintStatus();
  if (!$("#shell")) { renderShell(); S.builtChat = undefined; S.listSig = ""; }
  // an open chat decides which space you are in; a space that went away
  // (left the group) falls back home
  const cur = S.snap.chats.find((c) => c.id === S.chat);
  if (cur) S.space = cur.direct ? "home" : cur.id;
  else if (S.space !== "home" && !S.snap.chats.some((c) => c.id === S.space)) S.space = "home";
  paintRail();
  paintVoice();

  const listSig = JSON.stringify([S.space, S.filter, S.chat, S.ch, S.voice, S.snap.chats, S.snap.friends, [...S.collapsed],
    S.conv?.id === S.space ? S.conv.members.map((m) => m.avatar) : 0]);
  if (listSig !== S.listSig) { S.listSig = listSig; paintList(); }

  const mount = S.voice ? "voice:" + S.voice : S.chat;
  const rebuilt = S.builtChat !== mount && !!S.conv;
  if (rebuilt) { S.builtChat = mount; S.msgSig = ""; }
  const sig = msgSignature(S.conv);
  paintConv(rebuilt);
  if (S.conv && !S.voice && (rebuilt || sig !== S.msgSig)) { S.msgSig = sig; updateMsgs(S.conv); }
  trackNav();
}

// Back and forward, like a browser: the mouse side buttons (and Alt+arrows)
// walk through the places you opened.
const nav = { list: [], at: -1, busy: false };
const navKey = () => JSON.stringify([S.space, S.chat, S.ch, S.voice, S.fpage]);
function trackNav() {
  if (nav.busy) return;
  const k = navKey();
  if (nav.list[nav.at] === k) return;
  nav.list = nav.list.slice(0, nav.at + 1).concat(k).slice(-50);
  nav.at = nav.list.length - 1;
}
async function goNav(step) {
  const k = nav.list[nav.at + step];
  if (!k) return;
  nav.at += step;
  nav.busy = true;
  try {
    const [space, chat, ch, voice, fpage] = JSON.parse(k);
    if (!chat || !S.snap.chats.some((c) => c.id === chat)) leaveChat({ space, fpage });
    else { await openChat(chat, ch); if (voice) await openVoice(voice); }
  } finally { nav.busy = false; }
}
document.addEventListener("mouseup", (e) => { if (e.button === 3 || e.button === 4) { e.preventDefault(); goNav(e.button === 3 ? -1 : 1); } });
document.addEventListener("keydown", (e) => {
  if (e.altKey && (e.key === "ArrowLeft" || e.key === "ArrowRight")) { e.preventDefault(); goNav(e.key === "ArrowLeft" ? -1 : 1); }
});

/* ============================ click routing ============================ */

const HANDLERS = {
  /* gate */
  "do-setup": async () => {
    const pass = $("#g-pass").value;
    // seizure of the device is in the threat model: no password has to be a
    // deliberate choice, never a skipped field
    if (!pass && !(await confirmBox("Bez hasła?",
      "Hasło szyfruje klucz tożsamości i historię rozmów zapisane na tym urządzeniu. Bez niego są przechowywane bez szyfrowania. Hasło możesz ustawić także później w ustawieniach. Kontynuować bez hasła?"))) {
      $("#g-pass").focus();
      return;
    }
    try {
      await api("Setup", $("#g-name").value, $("#g-conn").value, pass);
      await boot();
    } catch (e) { gateError(errText(e)); }
  },
  "do-unlock": async () => {
    try { await api("Unlock", $("#g-pass").value); await boot(); }
    catch (e) { gateError(errText(e)); }
  },
  "new-profile": async () => {
    const name = await promptBox({
      title: "Nowe konto", label: "Nazwa", placeholder: "np. praca",
      hint: "Małe litery, cyfry i myślniki. Aplikacja uruchomi się od nowa.", ok: "Utwórz",
    });
    if (!name) return;
    try { await api("UseProfile", name); await boot(); } catch (e) { fail(e); }
  },
  "use-profile": async (el) => {
    if (!await confirmBox("Przełączyć konto?", "Aplikacja uruchomi się od nowa.", false)) return;
    try { await api("UseProfile", el.dataset.v); await boot(); } catch (e) { fail(e); }
  },

  /* navigation */
  home: () => leaveChat({ space: "home", fpage: false }),
  space: (el) => {
    // a phone shows the channel list first, a desktop goes straight to the chat
    if (NARROW_Q.matches) return leaveChat({ space: el.dataset.chat });
    openChat(el.dataset.chat);
  },
  friends: () => leaveChat({ space: "home", fpage: true }),
  back: () => leaveChat({ fpage: false }),
  ftab: (el) => { S.ftab = el.dataset.v; paint(); $("#fr-code")?.focus(); },
  "fr-add": async () => {
    const code = $("#fr-code").value.trim();
    if (!code) return $("#fr-code").focus();
    try { toast("Dodano: " + await api("AddFriend", code), "ok"); S.ftab = "all"; refresh(); } catch (e) { fail(e); }
  },
  cat: (el) => {
    const id = el.dataset.id;
    S.collapsed.has(id) ? S.collapsed.delete(id) : S.collapsed.add(id);
    el.closest(".cat").classList.toggle("shut"); // fold now, the repaint just agrees
  },
  "members-toggle": () => { S.showMembers = !S.showMembers; paint(); },
  "g-menu": async (el) => {
    if (S.chat !== S.space) await openChat(S.space);
    chatMenu($(".g-head") || el);
  },
  emoji: (el) => emojiPicker(el, (e) => {
    const input = $("#input");
    const at = input.selectionStart ?? input.value.length;
    input.value = input.value.slice(0, at) + e + input.value.slice(input.selectionEnd ?? at);
    input.focus();
    input.setSelectionRange(at + e.length, at + e.length);
    input.dispatchEvent(new Event("input"));
  }),
  "unsaved-reset": () => renderSettings(),
  "unsaved-save": () => {
    const b = S.dirty?.querySelector(".btn.primary") || S.dirty?.querySelector(".btn-row .btn");
    S.dirty = null;
    $("#unsaved").classList.remove("show");
    b?.click();
  },
  settings: (el) => openSettings(el.dataset.section || "profile"),
  "close-settings": () => closeSettings(),
  section: (el) => { if (holdUnsaved()) return; S.section = el.dataset.id; S.setPaged = true; renderSettings(); },
  "set-back": () => { S.setPaged = false; renderSettings(); },
  open: (el) => openChat(el.dataset.chat),
  friend: (el) => profileDialog(el.dataset.pub),
  profile: (el) => {
    const inModal = el.closest(".modal");
    closeModal(null);
    profileDialog(el.dataset.pub, inModal || el.closest(".pop") ? null : el);
  },
  // a member you have not befriended yet becomes a friend first: a direct
  // chat is only made with friends
  dm: async (el) => {
    closeModal(null);
    const f = S.snap.friends.find((x) => x.pub === el.dataset.pub);
    try {
      if (!f && S.conv) await api("Befriend", S.conv.id, el.dataset.pub);
      openChat(f?.chat || await api("CreateDirect", el.dataset.pub));
    } catch (e) { fail(e); }
  },
  befriend: async (el) => {
    try { await api("Befriend", S.conv.id, el.dataset.pub); toast("Dodano do znajomych"); refresh(); } catch (e) { fail(e); }
  },
  "user-menu": (el) => userMenu(el.dataset.pub, el),
  "call-kick": (el) => act("KickFromCall", el.dataset.chat, el.dataset.pub),
  // Only this jump glides — the automatic scroll after a new message must stay
  // instant, or the list would still be travelling when the next one lands.
  jump: () => $("#msgs").scrollTo({ top: $("#msgs").scrollHeight, behavior: "smooth" }),

  /* modals — the backdrop only dismisses when the click misses the dialog */
  "modal-dismiss": () => closeModal(null),
  "modal-action": (el) => {
    const a = $("#modal-root")._actions[+el.dataset.i];
    closeModal(a.value);
  },
  hint: (el) => {
    const c = S.hints[+el.dataset.i];
    const input = $("#input");
    input.value = c.n + (c.a ? " " : "");
    autoGrow(input);
    S.hints = []; paintHints(); input.focus();
  },

  /* chat actions */
  "new-group": () => cmdNew(""),
  join: () => joinDialog(),
  "add-friend": () => addFriendDialog(),
  "my-code": () => { closeModal(null); myCodeDialog(); },
  invite: () => { closeModal(null); inviteDialog(); },
  "search-close": () => { $("#hs") && ($("#hs").value = ""); searchChat(""); },
  "search-jump": (el) => jumpTo(el.dataset.id),
  "chat-menu": (el) => chatMenu(el),
  rename: () => { closeModal(null); cmdRename(""); },
  "server-look": () => { popClose?.(); serverLook(S.conv.id); },
  members: () => { closeModal(null); membersDialog(); },
  rekey: async () => {
    closeModal(null);
    if (await confirmBox("Obrócić klucz?", "Powstanie nowa epoka; peerzy dostaną nowy klucz przy najbliższym kontakcie.", false)) {
      try { await api("Rekey", S.chat); toast("Klucz obrócony", "ok"); } catch (e) { fail(e); }
    }
  },
  unhide: async () => { closeModal(null); try { await api("UnhideAll", S.chat); } catch (e) { fail(e); } },
  leave: async () => {
    popClose?.();
    const c = S.conv;
    const owner = c.isOwner;
    if (!(await confirmBox(owner ? "Usunąć serwer?" : "Opuścić serwer?",
      owner ? "Serwer zniknie z tego urządzenia razem z historią. Możesz go usunąć, gdy nie ma w nim już nikogo poza tobą."
        : "Pozostali zobaczą, że opuszczasz serwer. Historia zniknie z tego urządzenia; wrócić można tylko przez nowe zaproszenie."))) return;
    try { await api("Leave", c.id); leaveChat({ space: "home", fpage: false }); } catch (e) { fail(e); }
  },
  kick: async (el) => {
    closeModal(null);
    if (await confirmBox("Wyrzucić z czatu?", "Klucz zostanie obrócony, żeby usunięty nie czytał dalej.")) {
      try { await api("Kick", S.chat, el.dataset.pub); } catch (e) { fail(e); }
    }
  },
  unfriend: async (el) => {
    closeModal(null);
    if (await confirmBox("Usunąć ze znajomych?", "Historia rozmów zostaje. Znika tylko wpis w książce adresowej.")) {
      try { await api("DelFriend", el.dataset.pub); } catch (e) { fail(e); }
    }
  },

  /* messages */
  react: async (el) => { try { await api("React", S.chat, el.dataset.id, el.dataset.emoji); } catch (e) { fail(e); } },
  "react-more": (el) => emojiPicker(el, (e) => api("React", S.chat, el.dataset.id, e).catch(fail)),
  reply: (el) => { const m = S.conv?.msgs.find((x) => x.id === el.dataset.id); if (m) setReply(m); popClose?.(); },
  "reply-cancel": () => setReply(null),
  pin: async (el) => { popClose?.(); try { await api("Pin", S.chat, el.dataset.id, el.dataset.unpin === "1"); } catch (e) { fail(e); } },
  pins: (el) => pinsPop(el),
  goto: (el) => { popClose?.(); jumpTo(el.dataset.id); },
  "copy-text": (el) => { popClose?.(); copyText(S.conv?.msgs.find((x) => x.id === el.dataset.id)?.text || ""); },
  "copy-id": (el) => { popClose?.(); copyText(el.dataset.id); },
  gif: (el) => gifPicker(el),
  "gif-save": (el) => {
    popClose?.();
    const m = S.conv?.msgs.find((x) => x.id === el.dataset.id);
    if (!m) return;
    setGifs([{ blob: m.blob, file: m.file }, ...gifs().filter((g) => g.blob !== m.blob)]);
    toast("GIF zapisany", "ok");
  },
  "gif-unsave": (el) => { popClose?.(); setGifs(gifs().filter((g) => g.blob !== el.dataset.blob)); },
  hide: async (el) => { try { await api("HideMsg", S.chat, el.dataset.id); } catch (e) { fail(e); } },
  delete: async (el) => {
    if (!await confirmBox("Usunąć wiadomość?", "Zniknie u wszystkich w czacie.")) return;
    try { await api("DeleteMsg", S.chat, el.dataset.id); } catch (e) { fail(e); }
  },
  zoom: (el) => lightbox(el.dataset.src),
  fetch: async (el) => {
    const d = el.dataset;
    try { await api("Fetch", S.chat, d.blob, d.mime, +d.size, d.author); toast("Pobieram…"); } catch (e) { fail(e); }
  },
  save: async (el) => {
    try {
      const p = await api("SaveAs", el.dataset.blob, el.dataset.name);
      if (p) toast("Zapisane: " + p, "ok");
    } catch (e) { fail(e); }
  },
  attach: () => api("PickFiles", S.chat, "").catch(fail),
  ext: (el) => { window.open(el.dataset.url, "_blank"); },

  /* calls */
  // the voice panel drives the call you are in, which need not be the open chat
  "join-call": (el) => HANDLERS["voice-chan"]({ dataset: { chat: el.dataset.chat || S.chat, ch: "" } }),
  // a voice channel joins its call and shows it; Discord does the same
  "voice-chan": async (el) => {
    const key = el.dataset.chat + el.dataset.ch;
    const g = S.snap.chats.find((c) => c.id === el.dataset.chat);
    if (g?.callKey !== key) {
      await act("JoinCall", key);
      reapplyPeers();
      if (S.selfMute) await act("ToggleMic", key);
      if (S.selfDeaf) await act("ToggleDeafen", key);
    }
    await openVoice(key);
  },
  "voice-open": (el) => openVoice(el.dataset.chat),
  "ub-mic": () => {
    const g = S.snap.chats.find((c) => c.call);
    if (g) return act("ToggleMic", g.callKey);
    S.selfMute = !S.selfMute; paint();
  },
  "ub-deaf": () => {
    const g = S.snap.chats.find((c) => c.call);
    if (g) return act("ToggleDeafen", g.callKey);
    S.selfDeaf = !S.selfDeaf; paint();
  },
  chan: (el) => openChat(el.dataset.chat, el.dataset.ch),
  "chan-new": (el) => chanDialog(el.dataset.cat || "", el.dataset.kind || "text"),
  "cat-new": async () => {
    const name = await promptBox({ title: "Utwórz kategorię", label: "Nazwa kategorii", placeholder: "nowa kategoria", ok: "Utwórz" });
    if (name?.trim()) addChannel("cat", name.trim(), "");
  },
  "chan-rename": async (el) => {
    const x = chanOf(curGroup(), el.dataset.ch, el.dataset.kind);
    if (!x) return;
    const name = await promptBox({ title: x.kind === "cat" ? "Zmień nazwę kategorii" : "Zmień nazwę kanału", label: "Nazwa", value: x.name });
    if (name?.trim()) putChannel(x.id, x.kind, name.trim(), x.cat, x.arch);
  },
  // a channel goes to the owner's archive; only a category is deleted outright
  "chan-del": async (el) => {
    const g = curGroup(), x = chanOf(g, el.dataset.ch, el.dataset.kind);
    if (!x) return;
    if (x.kind === "cat") {
      if (!await confirmBox(`Usunąć kategorię „${x.name}”?`, "Kanały z tej kategorii przejdą do domyślnych.")) return;
      return putChannel(x.id, "cat", "", "");
    }
    await dropChannel(g, x, x.name, true);
  },
  "chan-restore": (el) => {
    const x = chanOf(curGroup(), el.dataset.ch, el.dataset.kind);
    if (x) putChannel(x.id, x.kind, x.name, x.cat, false);
  },
  "chan-purge": async (el) => {
    const g = curGroup(), x = chanOf(g, el.dataset.ch, el.dataset.kind);
    if (!x || !await confirmBox(`Usunąć „${x.name}” na stałe?`, "Kanału nie da się potem przywrócić, a jego wiadomości znikną z listy u wszystkich.")) return;
    await dropChannel(g, x, "", false);
  },
  hangup: (el) => act("LeaveCall", el.dataset.chat || S.chat),
  mic: (el) => act("ToggleMic", el.dataset.chat || S.chat),
  deaf: (el) => act("ToggleDeafen", el.dataset.chat || S.chat),
  cam: (el) => startVideo(el, "cam", el.dataset.chat || S.chat),
  screen: (el) => startVideo(el, "screen", el.dataset.chat || S.chat),
  "video-go": (el) => {
    const v = Object.fromEntries([...el.parentElement.querySelectorAll("select")].map((x) => [x.dataset.k, +x.value]));
    const o = { fps: v.fps, height: v.height, quality: v.quality };
    if (el.dataset.kind === "cam") act("ToggleCam", el.dataset.chat, o);
    else act("ToggleScreen", el.dataset.chat, v.d || 0, o);
  },
  "tile-full": (el) => { const k = el.closest(".vtile").dataset.key; setFocus(k, !(S.full && S.focus === k)); },
  spatial: async (el) => {
    if (el.dataset.chat && el.dataset.chat !== S.chat) await openChat(el.dataset.chat);
    spatialDialog();
  },

  /* profile edits */
  "edit-nick": async () => {
    const v = await promptBox({ title: "Nazwa", label: "Jak mają cię widzieć", value: S.snap.me.name });
    if (v) { try { await api("SetNick", v); await afterEdit("Zapisane"); } catch (e) { fail(e); } }
  },
  "edit-status": async () => {
    const v = await promptBox({ title: "Status", label: "Krótka linijka", value: S.snap.me.status, placeholder: "np. koduję" });
    if (v !== null) { try { await api("SetStatus", v); await afterEdit("Zapisane"); } catch (e) { fail(e); } }
  },
  "fx-pick": (el) => {
    const st = parseStyle(S.draftStyle ?? S.snap.me.style);
    st[el.dataset.k] = +el.dataset.v;
    S.draftStyle = styleStr(st);
    repaintFx();
  },
  "fx-save": async () => {
    try { await api("SetStyle", S.draftStyle); S.snap.me.style = S.draftStyle; S.draftStyle = undefined; toast("Zapisane", "ok"); }
    catch (e) { fail(e); }
    repaintFx(); refresh();
  },
  "fx-reset": () => { S.draftStyle = undefined; repaintFx(); },
  "fx-clear": () => { S.draftStyle = "0.0.0.0.0.0.0"; repaintFx(); },
  "edit-bio": async () => {
    const v = await promptBox({ title: "Opis", label: "O tobie", value: S.snap.me.bio, multiline: true });
    if (v !== null) { try { await api("SetBio", v); await afterEdit("Zapisane"); } catch (e) { fail(e); } }
  },
  "edit-avatar": () => pickPicture("avatar"),
  "edit-banner": () => pickPicture("banner"),
  copy: (el) => copyText(el.dataset.text),

  /* settings */
  "save-conn": () => saveSettings({ connector: $("#s-conn").value }),
  "use-conn": (el) => saveSettings({ connector: el.dataset.v }),
  "add-conn": () => {
    const v = $("#s-newconn").value.trim();
    if (v) saveSettings({ connectors: [...new Set([...S.settings.connectors, v])] });
  },
  "del-conn": (el) => saveSettings({ connectors: S.settings.connectors.filter((c) => c !== el.dataset.v) }),
  "add-list": () => {
    const v = $("#s-newlist").value.trim();
    if (!/^https:\/\//.test(v)) return toast("Adres listy musi zaczynać się od https://", "err");
    saveSettings({ connectorLists: [...new Set([...S.settings.connectorLists, v])] });
  },
  "del-list": (el) => saveSettings({ connectorLists: S.settings.connectorLists.filter((c) => c !== el.dataset.v) }),
  "refresh-lists": async () => {
    try { S.settings = await api("RefreshConnectors"); toast("Listy odświeżone", "ok"); renderSettings(); }
    catch (e) { fail(e); }
  },
  "save-settings": () => saveSettings({
    camDevice: $("#s-cam")?.value ?? S.settings.camDevice,
    micDevice: $("#s-mic")?.value ?? S.settings.micDevice,
    speakerDevice: $("#s-spk")?.value ?? S.settings.speakerDevice,
    lang: $("#s-lang")?.value ?? S.settings.lang,
  }),
  "toggle-notify": () => saveSettings({ noNotify: !S.settings.noNotify }),
  "toggle-dev": () => saveSettings({ devMode: !S.settings.devMode }),
  "mic-test": () => micTest(!S.micTest),
  "toggle-relay": async () => {
    if (S.settings.relayOnly) return saveSettings({ relayOnly: false });
    const ok = await modal({
      title: "Ukryć Twój adres IP?",
      body: `<p>Rozmówcy zobaczą adres connectora zamiast Twojego. W zamian wszystko, co robisz, przechodzi przez connector.</p>${RELAY_FACTS}
        <p>Każdy Twój rozmówca dostanie informację, że łączycie się przez przekaźnik. Rozmowy będą wolniejsze, a gdy connector nie ma przekaźnika, nie połączysz się z nikim.</p>`,
      actions: [
        { label: "Anuluj", kind: "ghost", value: false },
        { label: "Rozumiem, włącz", kind: "danger", value: true },
      ],
    });
    if (ok) saveSettings({ relayOnly: true });
  },
  "toggle-quiet": () => saveSettings({ quietLookup: !S.settings.quietLookup }),
  "relay-info": () => relayInfo(),
  theme: (el) => { setTheme(el.dataset.v); renderSettings(); },
  accent: (el) => { applyAccent(el.dataset.v); saveSettings({ accent: el.dataset.v }); },
  "save-accent": () => {
    const v = $("#s-accent").value.replace("#", "").trim();
    if (!/^[0-9a-fA-F]{6}$/.test(v)) return toast("Podaj kolor jako 6 znaków hex, np. 8b7cf6", "err");
    applyAccent(v); saveSettings({ accent: v });
  },
  "change-pass": async () => {
    try {
      await api("ChangePass", $("#p-old").value, $("#p-new").value);
      toast("Hasło zmienione", "ok");
      $("#p-old").value = $("#p-new").value = "";
    } catch (e) { fail(e); }
  },
  backup: async () => {
    try { const p = await api("Backup"); if (p) toast("Kopia zapisana: " + p, "ok"); } catch (e) { fail(e); }
  },
  restore: async () => {
    try {
      const name = await api("Restore");
      if (!name) return;
      if (await confirmBox("Kopia wgrana", `Kopia wgrała się jako konto „${name}”. Przełączyć się teraz? Aplikacja uruchomi się od nowa.`, false)) {
        await api("UseProfile", name);
        await boot();
      }
    } catch (e) { fail(e); }
  },
  "toggle-webhook": () => { S.settings.webhookOn = !S.settings.webhookOn; renderSettings(); },
  "gen-token": async () => { $("#w-token").value = await api("NewWebhookToken"); },
  "save-webhook": () => saveSettings({
    webhookOn: S.settings.webhookOn,
    webhookPort: +$("#w-port").value,
    webhookToken: $("#w-token").value.trim(),
  }),

};

const WIN_ACTION = { min: "Minimise", max: "ToggleMax", close: "CloseApp" };

document.addEventListener("click", (e) => {
  const winBtn = e.target.closest("[data-win]");
  if (winBtn) return act(WIN_ACTION[winBtn.dataset.win]);
  const el = e.target.closest("[data-act]");
  if (!el) return;
  const fn = HANDLERS[el.dataset.act];
  if (!fn) return;
  // a click inside a dialog bubbles to the backdrop: leave it alone, or
  // preventDefault would undo checkboxes and labels in the dialog
  if (el.classList.contains("backdrop") && e.target !== el) return;
  e.preventDefault();
  e.stopPropagation();
  fn(el, e);
  // a picked menu item closes its menu; the card's own buttons do too
  if (el.closest(".pop")) popClose?.();
});

document.addEventListener("click", (e) => {
  const t = e.target.closest(".vtile.cam");
  if (t && !e.target.closest("[data-act]")) setFocus(S.focus === t.dataset.key && !S.full ? null : t.dataset.key, S.full);
});
document.addEventListener("dblclick", (e) => {
  const t = e.target.closest(".vtile.cam");
  if (t) setFocus(t.dataset.key, !S.full);
});

document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    if (popClose) return popClose();
    if ($("#modal-root").innerHTML) return closeModal(null);
    if ($(".lightbox")) return closeLightbox();
    if (S.screen === "settings") return closeSettings();
    if (S.focus) return setFocus(S.full ? S.focus : null);
  }
  if (e.key === "f" && (e.ctrlKey || e.metaKey) && S.chat) { e.preventDefault(); $("#hs")?.focus(); }
  if (e.key === "k" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); $("#q")?.focus(); }
});

/* ---------------------------- spatial audio ---------------------------- */

// Drag a participant around the room to place them in the stereo field.
// Distance drops the gain, horizontal offset pans; the mapping mirrors the
// Fyne client's spatial page so both UIs sound the same.
// You sit at the bottom middle of the room. Distance from you sets a person's
// gain, the side sets the pan. Positions are kept in peerVol, so reopening the
// room shows where everyone was left.
const EAR = [0.5, 0.86];
function spatialDialog() {
  if (!S.conv) return;
  const me = S.conv.members.find((m) => m.me);
  const peers = S.conv.members.filter((m) => m.inCall && !m.me);
  if (!peers.length) return toast("Nikogo jeszcze nie ma w rozmowie.");
  modal({
    title: "Przestrzeń dźwięku",
    wide: true,
    body: `<p>Przeciągnij osobę: bliżej ciebie = głośniej, w bok = w lewy lub prawy głośnik.</p>
      <div class="room" id="room">
        ${[0.25, 0.5, 0.75].map((r) => `<i class="room-ring" style="--r:${r}"></i>`).join("")}
        <span class="room-side l">L</span><span class="room-side r">P</span>
        <div class="room-me" style="left:${EAR[0] * 100}%;top:${EAR[1] * 100}%">${avatar(me?.avatar, me?.name || "Ty", me?.pub || "me", "m")}<b>Ty</b></div>
      </div>`,
    actions: [{ label: "Gotowe", kind: "ghost", value: null }],
    onMount: (box) => {
      const room = $("#room", box);
      peers.forEach((m, i) => {
        const t = document.createElement("div");
        t.className = "room-peer";
        t.innerHTML = `${avatar(m.avatar, m.name, m.pub, "m")}<b>${esc(m.name)}</b><span class="mono"></span>`;
        room.append(t);
        const place = (fx, fy, apply) => {
          t.style.left = fx * 100 + "%";
          t.style.top = fy * 100 + "%";
          const pan = Math.max(-1, Math.min(1, (fx - EAR[0]) * 2.2));
          const gain = Math.max(0.1, Math.min(1.5, 1.35 - Math.hypot(fx - EAR[0], fy - EAR[1]) * 1.6));
          t.lastChild.textContent = `${Math.round(gain * 100)}%${Math.abs(pan) > 0.05 ? ` · ${pan < 0 ? "L" : "P"}${Math.round(Math.abs(pan) * 100)}` : ""}`;
          if (apply) setPeer(m.pub, { gain, pan, pos: [fx, fy] });
        };
        // someone placed before keeps their spot; a newcomer goes on an arc
        // around you, spread left to right
        const pos = peerVol.get(m.pub)?.pos;
        if (pos) place(...pos, false);
        else {
          const a = (peers.length === 1 ? 0 : i / (peers.length - 1) - 0.5) * 1.6;
          place(EAR[0] + Math.sin(a) * 0.3, EAR[1] - Math.cos(a) * 0.45, true);
        }
        t.addEventListener("pointerdown", (e) => {
          e.preventDefault(); // no text selection, no native image drag
          t.setPointerCapture(e.pointerId);
          t.classList.add("held");
        });
        t.addEventListener("pointermove", (e) => {
          if (!t.hasPointerCapture(e.pointerId)) return;
          const r = room.getBoundingClientRect();
          place(
            Math.max(0.06, Math.min(0.94, (e.clientX - r.left) / r.width)),
            Math.max(0.1, Math.min(0.9, (e.clientY - r.top) / r.height)),
            true,
          );
        });
        const drop = () => t.classList.remove("held");
        t.addEventListener("pointerup", drop);
        t.addEventListener("lostpointercapture", drop);
      });
    },
  });
}

/* ============================ boot ============================ */

async function boot() {
  S.boot = await api("Boot");
  applyAccent(S.boot.accent);
  if (S.boot.stage === "ready") {
    S.stage = "ready";
    S.version = await api("Version");
    renderShell();
    await refresh();
  } else {
    S.stage = "gate";
    renderGate();
  }
}

Events.On("sync", () => { if (S.stage === "ready") refresh(); });

let callRaf = 0;
Events.On("frame", (e) => {
  const d = e.data;
  const name = S.conv?.members.find((m) => m.pub === d.from)?.name || S.frames.get(d.key)?.name || "peer";
  S.frames.set(d.key, { seq: d.seq, kind: d.kind, name, at: Date.now() });
  // several streams at 15-24 fps: one repaint per screen refresh is plenty
  callRaf ||= requestAnimationFrame(() => { callRaf = 0; paintCall(); });
});

Events.On("frame-end", (e) => { S.frames.delete(e.data); paintCall(); });

Events.On("frames-cleared", (e) => {
  for (const k of [...S.frames.keys()]) if (k.startsWith(e.data + ".")) S.frames.delete(k);
  paintCall();
});

// Speaking ring: while you are in a call, every [data-speak] avatar gets
// --lvl (0..1) from the live audio peak. Speech peaks sit around .1-.4, so
// the level is stretched; below .02 is room noise and stays dark.
setInterval(async () => {
  const live = S.stage === "ready" && S.snap?.chats.some((c) => c.call);
  const lv = live ? await api("VoiceLevels").catch(() => ({})) : {};
  for (const el of $$("[data-speak]")) {
    const v = lv[el.dataset.speak] || 0;
    el.style.setProperty("--lvl", v < .02 ? 0 : Math.min(1, v * 4).toFixed(2));
  }
}, 150);

// The connector state changes without touching the DAG, so poll it gently.
setInterval(() => { if (S.stage === "ready" && !refreshing) refresh(); }, 4000);

boot().catch((e) => {
  view.innerHTML = `<div class="gate"><div class="gate-card"><div class="panel">
      <h3>szept nie wstał</h3><p class="hint-text err" style="padding:0 16px 16px">${esc(errText(e))}</p>
    </div></div></div>`;
});
