// A profile style is "font.effect.hue.hue.deco.frame.fx", seven small numbers
// that travel inside the signed profile card (core SetStyle). Unknown values
// render as nothing, so an older client and a newer preset never break.

export const PAL = ["oklch(.74 .15 25)", "oklch(.8 .14 70)", "oklch(.86 .15 100)", "oklch(.76 .15 150)", "oklch(.78 .11 195)",
  "oklch(.72 .13 240)", "oklch(.7 .15 290)", "oklch(.74 .15 340)", "oklch(.97 0 0)", "oklch(.82 .12 85)",
  "oklch(.6 .21 22)", "oklch(.55 .12 200)", "oklch(.58 .2 300)", "oklch(.3 .02 260)"];

// [file, left%, top%, width%, tilt, motion, delay] in the avatar's own box
export type Sprite = [string, number, number, number, number, string, number];
// [name, sprites, ring]: ring true is a full double ring, a number is an arc
// whose gap is centred at that angle (clockwise from 3 o'clock)
export type Deco = [string, Sprite[], (boolean | number)?];

// Avatar decorations, anime themed and deliberately lopsided like a sticker
// stuck on a photo. Each sprite scales with every avatar size.
// Art: Microsoft Fluent Emoji 3D (MIT), see public/fx/LICENSE.txt.
export const DECO: Deco[] = [
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

export const decoSrc = (f: string) => `/fx/${f.includes(".") ? f : f + ".png"}`;

export interface CardFxOpts { wobble?: 1; roll?: 1; spin?: 1; dots?: 1; blink?: 1 }
// [name, count per 300x260, images, size, direction, speed, options]
export type CardFx = [string, number?, string[]?, [number, number]?, string?, [number, number]?, CardFxOpts?];

// Profile card effects: tsParticles (MIT) on a canvas behind the frame.
export const CARDFX: CardFx[] = [
  ["Brak"],
  ["Hanami", 18, ["petal.svg", "petal.svg", "petal.svg", "cherry_blossom"], [4, 9], "bottom-right", [.6, 1.5], { wobble: 1, roll: 1, spin: 1 }],
  ["Momiji", 11, ["maple_leaf", "maple_leaf", "fallen_leaf"], [6, 11], "bottom", [.5, 1.2], { wobble: 1, roll: 1, spin: 1 }],
  ["Yuki", 34, ["snowflake"], [2.5, 6], "bottom", [.3, .9], { wobble: 1, spin: 1, dots: 1 }],
  ["Hoshi", 14, ["sparkles", "glowing_star"], [4, 9], "none", [.1, .3], { blink: 1 }],
  ["Hotaru", 16, ["glow.svg"], [4, 10], "none", [.3, .8], { blink: 1 }],
  ["Tōrō", 7, ["red_paper_lantern"], [8, 13], "top", [.25, .55], { wobble: 1 }],
  ["Doki doki", 10, ["sparkling_heart", "two_hearts"], [5, 10], "top", [.4, 1], { wobble: 1 }],
];

export const FX = {
  f: ["Zwykła", "Mono", "Szeryf", "Kapitaliki", "Wąska"],
  e: ["Bez efektu", "Kolor", "Gradient", "Neon", "Komiks", "Połysk"],
  d: DECO.map((x) => x[0]),
  r: ["Brak", "Linia", "Podwójna", "Gradient", "Narożniki"],
  x: CARDFX.map((x) => x[0]),
};

export interface Style { f: number; e: number; a: number; b: number; d: number; r: number; x: number }
export type StyleKey = keyof Style;

export function parseStyle(s?: string): Style {
  const [f, e, a, b, d, r, x] = String(s || "").split(".").map((n) => +n || 0);
  const ok = (v: number, list: unknown[]) => (v >= 0 && v < list.length ? v : 0);
  const c = (v: number) => (v >= 0 && v < PAL.length ? v : 0);
  return { f: ok(f, FX.f), e: ok(e, FX.e), a: c(a), b: c(b), d: ok(d, FX.d), r: ok(r, FX.r), x: ok(x, FX.x) };
}

export const styleStr = (st: Style) => [st.f, st.e, st.a, st.b, st.d, st.r, st.x].join(".");

export const NO_STYLE = "0.0.0.0.0.0.0";

// nameFx: attributes that dress a display name (font, effect, colors)
export function nameFx(style?: string): Record<string, string> {
  const st = parseStyle(style);
  if (!st.f && !st.e) return {};
  return { "data-nf": String(st.f), "data-ne": String(st.e), style: `--na:${PAL[st.a]};--nb:${PAL[st.b]}` };
}

// decoVars: the two ring colors of a decoration
export const decoVars = (st: Style) => ({ "--da": PAL[st.a], "--db": PAL[st.b] });
