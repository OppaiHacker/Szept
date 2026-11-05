// Avatar grounds: eight inks at one lightness and chroma, so no face shouts
// louder than another and white initials read on all of them.
const AVATAR_HUES = [15, 45, 145, 185, 220, 255, 290, 330];

export function tint(seed: string): string {
  let h = 0;
  for (const ch of String(seed)) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return `oklch(.54 .09 ${AVATAR_HUES[h % AVATAR_HUES.length]})`;
}

export function initials(name: string): string {
  const parts = String(name || "?").trim().split(/\s+/).slice(0, 2);
  return parts.map((p) => [...p][0] || "").join("").toUpperCase() || "?";
}
