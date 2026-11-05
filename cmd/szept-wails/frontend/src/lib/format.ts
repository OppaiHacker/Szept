// Dates, counts and sizes, Polish style.

const PL_MONTHS = ["sty", "lut", "mar", "kwi", "maj", "cze", "lip", "sie", "wrz", "paź", "lis", "gru"];

export const hhmm = (ts: number) =>
  new Date(ts * 1000).toLocaleTimeString("pl-PL", { hour: "2-digit", minute: "2-digit" });

export const dateOf = (ts: number) => new Date(ts * 1000).toLocaleDateString("pl-PL");

export function dayLabel(ts: number): string {
  const d = new Date(ts * 1000), now = new Date();
  const midnight = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diff = midnight(now) - midnight(d);
  if (diff === 0) return "Dziś";
  if (diff === 864e5) return "Wczoraj";
  return `${d.getDate()} ${PL_MONTHS[d.getMonth()]} ${d.getFullYear()}`;
}

// Polish counts: 1 osoba, 2-4 osoby, 5+ osób (12-14 too)
export function plural(n: number, one: string, few: string, many: string): string {
  if (n === 1) return `${n} ${one}`;
  const d = n % 10, t = n % 100;
  return `${n} ${d >= 2 && d <= 4 && (t < 12 || t > 14) ? few : many}`;
}
export const people = (n: number) => plural(n, "osoba", "osoby", "osób");

export function humanSize(n = 0): string {
  if (n >= 1 << 30) return (n / (1 << 30)).toFixed(1) + " GB";
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + " MB";
  if (n >= 1 << 10) return Math.round(n / (1 << 10)) + " kB";
  return n + " B";
}

// badge numbers stop at 99+
export const count = (n: number) => (n > 99 ? "99+" : String(n));

// media player clock, m:ss
export const clock = (s: number) =>
  isFinite(s) ? `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, "0")}` : "0:00";

// errText: what a rejected Go call or a thrown error says, for a toast
export function errText(e: unknown): string {
  const x = e as { message?: string; value?: string } | null;
  return (x && (x.message || x.value || String(x))) || "coś poszło nie tak";
}
