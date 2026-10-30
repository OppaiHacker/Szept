// szept front end. No framework, no build step: the whole UI is DOM built from
// template strings plus one delegated click handler. Go pushes a "sync" event
// whenever the DAG changes; everything below repaints from a fresh snapshot.

import { Call, Events } from "/wails/runtime.js";

const api = (method, ...args) => Call.ByName("main.App." + method, ...args);

/* ============================ theme ============================ */

// The desktop decides unless the user pins a theme. Resolving "auto" here and
// stamping the answer on <html> keeps the stylesheet down to one palette per
// theme — no media query duplicated for every token.
const LIGHT_Q = matchMedia("(prefers-color-scheme: light)");
let themePref = "auto";
try { themePref = localStorage.getItem("theme") || "auto"; } catch { /* opaque origin */ }

function applyTheme() {
  document.documentElement.dataset.theme =
    themePref === "auto" ? (LIGHT_Q.matches ? "light" : "dark") : themePref;
}
function setTheme(pref) {
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

const AVATAR_HUES = [265, 200, 330, 145, 25, 190, 300, 95];
function tint(seed) {
  let h = 0;
  for (const ch of String(seed)) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return `hsl(${AVATAR_HUES[h % AVATAR_HUES.length]} 62% 68%)`;
}

function initials(name) {
  const parts = String(name || "?").trim().split(/\s+/).slice(0, 2);
  return parts.map((p) => [...p][0] || "").join("").toUpperCase() || "?";
}

function avatar(url, name, seed, cls = "") {
  if (url) return `<div class="avatar ${cls}"><img src="${esc(url)}" alt=""></div>`;
  return `<div class="avatar ${cls}" style="background:${tint(seed || name)}">${esc(initials(name))}</div>`;
}

function presenceAvatar(url, name, seed, online, cls = "") {
  return `<div class="av-wrap ${online ? "online" : ""}">${avatar(url, name, seed, cls)}<i class="presence"></i></div>`;
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

function humanSize(n) {
  if (n >= 1 << 30) return (n / (1 << 30)).toFixed(1) + " GB";
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + " MB";
  if (n >= 1 << 10) return Math.round(n / (1 << 10)) + " kB";
  return n + " B";
}

const errText = (e) => (e && (e.message || e.value || e.toString())) || "coś poszło nie tak";

/* ============================ icons ============================ */

const I = {
  chat: '<path d="M20 12a8 8 0 0 1-11.6 7.1L4 20l.9-4.4A8 8 0 1 1 20 12Z"/>',
  people: '<path d="M16 19v-1.5a3.5 3.5 0 0 0-3.5-3.5h-5A3.5 3.5 0 0 0 4 17.5V19"/><circle cx="10" cy="8" r="3.2"/><path d="M20 19v-1.5a3.5 3.5 0 0 0-2.6-3.4M15.5 5.2a3.2 3.2 0 0 1 0 5.6"/>',
  user: '<circle cx="12" cy="8" r="3.6"/><path d="M5 20a7 7 0 0 1 14 0"/>',
  gear: '<circle cx="12" cy="12" r="3"/><path d="M19.4 14.5a1.6 1.6 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.6 1.6 0 0 0-2.7 1.1V21a2 2 0 1 1-4 0v-.1a1.6 1.6 0 0 0-2.7-1.2l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.6 1.6 0 0 0-1.1-2.7H3a2 2 0 1 1 0-4h.1a1.6 1.6 0 0 0 1.2-2.7l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.6 1.6 0 0 0 2.7-1.1V3a2 2 0 1 1 4 0v.1a1.6 1.6 0 0 0 2.7 1.2l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.6 1.6 0 0 0 1.1 2.7H21a2 2 0 1 1 0 4h-.1a1.6 1.6 0 0 0-1.5 1.1Z"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  search: '<circle cx="11" cy="11" r="6.5"/><path d="m20 20-3.6-3.6"/>',
  send: '<path d="M4.5 12 20 4.5 15 20l-3.4-5.6L4.5 12Z"/>',
  clip: '<path d="M20 11.5 12.3 19a4.6 4.6 0 0 1-6.5-6.5l8-8a3 3 0 1 1 4.3 4.3l-8 8a1.5 1.5 0 0 1-2.1-2.1l7.3-7.3"/>',
  phone: '<path d="M6.5 3.5 9 4l1 3.5-2 1.5a11 11 0 0 0 5.5 5.5l1.5-2 3.5 1 .5 2.5c0 1.1-.9 2-2 2A14.5 14.5 0 0 1 4.5 5.5c0-1.1.9-2 2-2Z"/>',
  hangup: '<path d="M3 10.5c5-3.5 13-3.5 18 0l-1.2 2.8-3.6-.6-.5-2.4a12 12 0 0 0-7.4 0l-.5 2.4-3.6.6L3 10.5Z"/><path d="m4 20 16-16"/>',
  mic: '<rect x="9" y="3" width="6" height="10" rx="3"/><path d="M5.5 11.5a6.5 6.5 0 0 0 13 0M12 18v3"/>',
  micOff: '<path d="M9 5a3 3 0 0 1 6 0v5m-6 .5V11a3 3 0 0 0 4.4 2.7"/><path d="M5.5 11.5a6.5 6.5 0 0 0 9.7 5.6M18.5 11.5c0 .5 0 1-.2 1.5M12 18v3"/><path d="m4 4 16 16"/>',
  cam: '<rect x="3" y="6.5" width="12" height="11" rx="2.5"/><path d="m15 11 5.5-3v8L15 13"/>',
  screen: '<rect x="3" y="4.5" width="18" height="12" rx="2"/><path d="M9 20h6M12 16.5V20"/>',
  volume: '<path d="M11 5 6.5 8.5H3v7h3.5L11 19V5Z"/><path d="M15 9.5a3.5 3.5 0 0 1 0 5M17.5 7a7 7 0 0 1 0 10"/>',
  deaf: '<path d="M11 5 6.5 8.5H3v7h3.5L11 19V5Z"/><path d="m15.5 9.5 5 5m0-5-5 5"/>',
  down: '<path d="M12 5v14m0 0-6-6m6 6 6-6"/>',
  x: '<path d="M6 6l12 12M18 6 6 18"/>',
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
  tab: "chats",
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
    setTimeout(() => el.remove(), 260);
  }, kind === "err" ? 4200 : 2400);
}

const fail = (e) => toast(errText(e), "err");

// A rejected call that nobody caught is still a broken button — say so out
// loud instead of leaving the UI silently doing nothing.
window.addEventListener("unhandledrejection", (e) => fail(e.reason));
window.addEventListener("error", (e) => fail(e.message));

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
function modal({ title, body: bodyHTML, actions = [], wide = false, onMount }) {
  closeModal(null);
  return new Promise((resolve) => {
    const root = $("#modal-root");
    root.innerHTML = `
      <div class="backdrop" data-act="modal-dismiss">
        <div class="modal ${wide ? "wide" : ""}" role="dialog" aria-modal="true">
          <header><h3>${esc(title)}</h3>
            <button class="icon-btn" data-act="modal-dismiss">${icon("x")}</button>
          </header>
          <div class="body">${bodyHTML}</div>
          ${actions.length ? `<footer>${actions.map((a, i) =>
            `<button class="btn ${a.kind || "ghost"}" data-act="modal-action" data-i="${i}">${esc(a.label)}</button>`).join("")}</footer>` : ""}
        </div>
      </div>`;
    const box = $(".modal", root);
    anchor(box);
    modalClose = (result) => { root.innerHTML = ""; modalClose = null; resolve(result); };
    root._actions = actions;
    onMount?.(box);
    const first = $("input, textarea, select", box);
    first?.focus();
    first?.select?.();
  });
}

function closeModal(result) { modalClose?.(result); }

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
  el.addEventListener("click", () => el.remove());
  document.body.append(el);
}

/* ============================ gate (login) ============================ */

const LOGO = `<svg viewBox="0 0 24 24"><path d="M12 3c-4.7 0-8.5 3.2-8.5 7.2 0 2.3 1.3 4.4 3.3 5.7l-.7 3.6a.5.5 0 0 0 .74.53l3.9-2.3c.4.05.8.07 1.26.07 4.7 0 8.5-3.2 8.5-7.6S16.7 3 12 3Z"/></svg>`;

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
          <p>szyfrowany komunikator P2P — bez serwera, bez śladów</p></div>
        <div class="panel">
          <h3>${title}</h3>
          ${setup ? `
            <div class="field"><label>Nazwa</label>
              <input class="input" id="g-name" value="${esc(b.profile)}" placeholder="np. ala"></div>
            <div class="field"><label>Connector</label>
              <input class="input" id="g-conn" value="${esc(b.connector)}" placeholder="ws://twoj-vps:7777"></div>
            <div class="field"><label>Hasło klucza <span style="text-transform:none;color:var(--faint)">(zalecane)</span></label>
              <input class="input" id="g-pass" type="password" placeholder="•••••"></div>
            <p class="hint-text" style="padding:0 16px">Connector to lekki serwer spotkań — sam nie widzi treści rozmów.</p>
            <p class="hint-text err" style="padding:0 16px">Bez hasła klucz i cała historia rozmów leżą na dysku bez szyfrowania — kto ma dostęp do urządzenia, przeczyta wszystko.</p>
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

function renderShell() {
  view.innerHTML = `
    <div class="shell" id="shell">
      <nav class="rail">
        <button class="rail-btn" data-act="tab" data-tab="chats" title="Rozmowy">${icon("chat")}<span class="pip" hidden></span></button>
        <button class="rail-btn" data-act="tab" data-tab="friends" title="Znajomi">${icon("people")}</button>
        <div class="rail-spacer"></div>
        <button class="rail-btn" data-act="tab" data-tab="me" title="Mój profil">${icon("user")}</button>
        <button class="rail-btn" data-act="settings" title="Ustawienia">${icon("gear")}</button>
      </nav>
      <section class="list" id="list"></section>
      <section class="conv" id="conv"></section>
      <aside class="members" id="members"></aside>
    </div>`;
}

function paintRail() {
  $$(".rail-btn[data-tab]").forEach((b) => b.classList.toggle("active", b.dataset.tab === S.tab));
  const pip = $(".rail-btn[data-tab=chats] .pip");
  const n = S.snap?.unread || 0;
  if (pip) { pip.hidden = n === 0; pip.textContent = n > 99 ? "99+" : n; }
}

/* ---------------------------- list pane ---------------------------- */

function chatRow(c) {
  const active = c.id === S.chat;
  const tail = c.unread
    ? `<span class="badge">${c.unread > 99 ? "99+" : c.unread}</span>`
    : c.inCall.length
      ? `<span class="chip-live">rozmowa</span>`
      : c.online
        ? `<span class="badge soft">${c.online}</span>`
        : "";
  return `<button class="row ${active ? "active" : ""} ${c.unread ? "unread" : ""}" data-act="open" data-chat="${c.id}">
      ${presenceAvatar(c.avatar, c.name, c.peer || c.id, c.online > 0 || (c.direct && c.online > 0))}
      <div class="row-body">
        <div class="row-top">
          <span class="row-name">${esc(c.name)}</span>
          <span class="row-time">${shortTime(c.previewTS)}</span>
        </div>
        <div class="row-sub">${esc(c.preview || (c.direct ? "brak wiadomości" : `${c.members} osób`))}</div>
      </div>
      <div class="row-tail">${tail}</div>
    </button>`;
}

function friendRow(f) {
  return `<button class="row" data-act="friend" data-pub="${f.pub}">
      ${presenceAvatar(f.avatar, f.name, f.pub, f.online)}
      <div class="row-body">
        <div class="row-top"><span class="row-name">${esc(f.name)}</span></div>
        <div class="row-sub">${f.alias ? `przedstawia się jako „${esc(f.alias)}” · ` : ""}${esc(f.status || (f.online ? "online" : "offline"))}</div>
      </div>
      <div class="row-tail">
        <span class="icon-btn" data-act="dm" data-pub="${f.pub}" title="Napisz">${icon("chat")}</span>
      </div>
    </button>`;
}

function paintList() {
  const list = $("#list");
  if (!list) return;
  const q = S.filter.toLowerCase();
  // the search box is rebuilt with the pane, so put the caret back afterwards
  const old = $("#q");
  const focused = old && document.activeElement === old;
  const caret = old?.selectionStart ?? 0;

  if (S.tab === "me") { list.innerHTML = mePane(); return; }

  const isChats = S.tab === "chats";
  const items = isChats
    ? S.snap.chats.filter((c) => !q || c.name.toLowerCase().includes(q) || c.preview.toLowerCase().includes(q))
    : S.snap.friends.filter((f) => !q || f.name.toLowerCase().includes(q));

  list.innerHTML = `
    <div class="pane-head">
      <h2>${isChats ? "Rozmowy" : "Znajomi"}</h2>
      <div class="spacer"></div>
      ${isChats
        ? `<button class="icon-btn" data-act="new-group" title="Nowa grupa">${icon("plus")}</button>
           <button class="icon-btn" data-act="join" title="Wejdź z zaproszenia">${icon("key")}</button>`
        : `<button class="icon-btn" data-act="add-friend" title="Dodaj znajomego">${icon("plus")}</button>
           <button class="icon-btn" data-act="my-code" title="Mój kod">${icon("qr")}</button>`}
    </div>
    <div class="search">${icon("search")}
      <input id="q" placeholder="${isChats ? "Szukaj rozmowy…" : "Szukaj znajomego…"}" value="${esc(S.filter)}">
    </div>
    <div class="scroll"><div class="rows">
      ${items.length
        ? items.map(isChats ? chatRow : friendRow).join("")
        : `<div class="empty"><strong>${isChats ? "Pusto tutaj" : "Brak znajomych"}</strong>
             ${isChats ? "Załóż grupę albo napisz do znajomego." : "Dodaj kogoś jego kodem SZF-."}</div>`}
    </div></div>`;

  const q$ = $("#q", list);
  q$.addEventListener("input", () => { S.filter = q$.value; S.listSig = null; paintList(); });
  if (focused) { q$.focus(); q$.setSelectionRange(caret, caret); }
}

/* ---------------------------- me pane ---------------------------- */

function mePane() {
  const me = S.snap.me;
  const banner = me.banner
    ? `style="background-image:url('${esc(me.banner)}')"`
    : "";
  return `
    <div class="me-pane scroll">
      <div class="card-banner" ${banner}>
        <button class="icon-btn edit" data-act="edit-banner" title="Zmień baner">${icon("edit")}</button>
      </div>
      <div class="card-id">
        <button data-act="edit-avatar" title="Zmień awatar">${avatar(me.avatar, me.name, me.pub, "xl")}</button>
        <div class="nm">${esc(me.name)}</div>
        <div class="st">${esc(me.status || "bez statusu")}</div>
        ${me.bio ? `<div class="bio">${esc(me.bio)}</div>` : ""}
      </div>
      <div class="btn-row">
        <button class="btn" data-act="edit-nick">${icon("edit")}Nazwa</button>
        <button class="btn" data-act="edit-status">${icon("smile")}Status</button>
        <button class="btn" data-act="edit-bio">${icon("edit")}Opis</button>
      </div>

      <div class="sec-label">Twój kod</div>
      <div class="code-box">${esc(me.code)}</div>
      <div class="btn-row">
        <button class="btn" data-act="copy" data-text="${esc(me.code)}">${icon("copy")}Kopiuj</button>
        <button class="btn" data-act="my-code">${icon("qr")}Pokaż QR</button>
      </div>

      <div class="sec-label">Konto</div>
      <button class="nav-row" data-act="settings" data-section="connector">
        <span class="ico">${icon("server")}</span>
        <span class="tx"><b>Connector</b><span>${esc(me.connector || "nie ustawiony")}</span></span>
        <span class="chev">${icon("chev")}</span>
      </button>
      <button class="nav-row" data-act="settings" data-section="accounts">
        <span class="ico">${icon("people")}</span>
        <span class="tx"><b>Konta</b><span>aktywne: ${esc(me.profile || "domyślne")}</span></span>
        <span class="chev">${icon("chev")}</span>
      </button>
      <button class="nav-row" data-act="settings" data-section="security">
        <span class="ico">${icon("shield")}</span>
        <span class="tx"><b>Hasło i kopia</b><span>zmień hasło, zrób backup</span></span>
        <span class="chev">${icon("chev")}</span>
      </button>
      <button class="nav-row" data-act="settings" data-section="about">
        <span class="ico">${icon("chat")}</span>
        <span class="tx"><b>O szepcie</b><span>wersja i licencja</span></span>
        <span class="chev">${icon("chev")}</span>
      </button>
    </div>`;
}

/* ---------------------------- conversation ---------------------------- */

// Repaint guards. The conversation is rebuilt only when the user switches
// chats; everything else patches the pieces that actually changed, so a sync
// burst never yanks focus out of the composer or drops half-typed text.
function msgSignature(c) {
  if (!c) return "";
  return c.msgs.map((m) =>
    [m.id, m.ack, m.deleted, m.have, m.havePrev, m.getting, JSON.stringify(m.reactions || {})].join(":")
  ).join(",");
}

const welcomeHTML = `<div class="empty" style="margin:auto;max-width:420px">
    <strong>Wybierz rozmowę</strong>
    Kliknij czat z lewej, załóż grupę przyciskiem „+”, albo dodaj znajomego jego kodem.
    Wpisz <b>/pomoc</b> w polu wiadomości, żeby zobaczyć komendy.
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

function msgHTML(m, prev, mePub, fresh) {
  if (m.kind === "info") {
    return `<div class="sys">${body(m.text)}</div>`;
  }
  const grouped = prev && prev.kind !== "info" && prev.author === m.author && m.ts - prev.ts < 300;
  const cls = ["msg", m.mine ? "mine" : "", grouped ? "grouped" : "",
    m.deleted ? "deleted" : "", fresh ? "fresh" : ""].join(" ");
  // the clock already lives in the bubble's meta line — the header only names
  // who is speaking, and only for the first message of a block
  const head = grouped || m.mine ? "" : `<div class="msg-who">
      <span class="n" style="color:${tint(m.author)}">${esc(m.name)}</span></div>`;

  const meta = m.deleted ? "" : `<span class="meta">${hhmm(m.ts)}${m.mine ? ackIcon(m.ack) : ""}</span>`;
  const text = m.text ? body(m.text) : "";

  return `<div class="${cls}" data-id="${m.id}">
      <div class="gutter">${grouped ? "" : avatar(m.avatar, m.name, m.author, "s")}</div>
      <div class="msg-col">
        ${head}
        <div class="bubble">${attachHTML(m)}${text}${meta}</div>
        ${reactionsHTML(m, mePub)}
      </div>
      ${m.deleted ? "" : `<div class="msg-tools">
        ${REACT_SET.slice(0, 3).map((e) => `<button data-act="react" data-id="${m.id}" data-emoji="${e}">${e}</button>`).join("")}
        <button data-act="react-more" data-id="${m.id}" title="Więcej">${icon("smile")}</button>
        <button data-act="hide" data-id="${m.id}" title="Schowaj u mnie">${icon("eye")}</button>
        ${m.mine ? `<button data-act="delete" data-id="${m.id}" title="Usuń dla wszystkich">${icon("trash")}</button>` : ""}
      </div>`}
    </div>`;
}

function callBar(c) {
  const call = c.call;
  const joined = call.joined;
  const peers = c.members.filter((m) => m.inCall && !m.me);
  if (!joined && peers.length === 0) return "";
  return `<div class="callbar">
      <span class="who">${joined ? "Jesteś w rozmowie" : "Trwa rozmowa"}</span>
      <div class="peers">${peers.slice(0, 6).map((m) => avatar(m.avatar, m.name, m.pub, "xs")).join("")}</div>
      <div class="spacer"></div>
      ${joined ? `
        <button class="ctl ${call.mic ? "on" : "off"}" data-act="mic" title="Mikrofon">${icon(call.mic ? "mic" : "micOff")}</button>
        <button class="ctl ${call.deaf ? "off" : ""}" data-act="deaf" title="Głośnik">${icon(call.deaf ? "deaf" : "volume")}</button>
        <button class="ctl ${call.cam ? "on" : ""}" data-act="cam" title="Kamera">${icon("cam")}</button>
        <button class="ctl ${call.screen ? "on" : ""}" data-act="screen" title="Ekran">${icon("screen")}</button>
        <button class="ctl" data-act="spatial" title="Przestrzeń dźwięku">${icon("people")}</button>
        <button class="ctl hang" data-act="hangup" title="Rozłącz">${icon("hangup")}</button>
      ` : `<button class="btn primary" data-act="join-call">${icon("phone")}Dołącz</button>`}
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
  host.innerHTML = `
    <header class="conv-head" id="conv-head"></header>
    <div id="callbar-slot"></div>
    <div class="stage" id="stage" hidden></div>
    <div class="conv-body">
      <div class="msgs" id="msgs"></div>
      <button class="jump" data-act="jump" hidden>${icon("down")}</button>
      <div class="typing" id="typing"></div>
      <div class="composer-wrap" id="composer-wrap">
        <div class="hints" id="hints" hidden></div>
        <div class="composer">
          <button class="icon-btn" data-act="attach" title="Załącz plik">${icon("clip")}</button>
          <textarea id="input" rows="1" placeholder="Napisz wiadomość albo /komendę…"></textarea>
          <button class="send" data-act="send" disabled>${icon("send")}</button>
        </div>
        <div class="drop-hint" id="drop" hidden>Upuść plik, żeby wysłać</div>
      </div>
    </div>`;
  wireComposer();
  wireMsgScroll($("#msgs"));
}

// What a relay operator can and cannot do. Shown when you turn relay-only on,
// and when a link — yours or the other side's choice — runs through a relay.
const RELAY_FACTS = `
  <p>Połączenie idzie przez przekaźnik (TURN) connectora, a nie bezpośrednio między wami.</p>
  <ul class="relay-facts">
    <li><b>Operator connectora widzi</b>, kto z kim rozmawia, kiedy, jak długo i ile danych przesyłacie. Może to zapisywać.</li>
    <li><b>Może zablokować albo zepsuć połączenie</b> w dowolnej chwili.</li>
    <li>Connector może postawić każdy, także cenzor albo policja. Jeśli nie wiesz, kto prowadzi Twój connector, zakładaj, że te dane trafiają do wroga.</li>
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
  const sub = (c.direct
    ? (c.members.find((m) => !m.me)?.online ? "online" : "offline")
    : `${c.members.length} osób · ${c.online} online`);
  const relayTag = relayed.length
    ? ` · <button class="relay-tag" data-act="relay-info" title="${esc(relayed.map((m) => `${m.name}: ${m.relay}`).join(", "))}">przez przekaźnik</button>`
    : "";
  // The whole name block is the shortcut everyone reaches for first: the peer's
  // profile in a direct chat, the member list in a group.
  const idAct = c.direct ? `data-act="profile" data-pub="${c.peer}"` : `data-act="members"`;
  $("#conv-head").innerHTML = `
    <div class="head-id" ${idAct} title="${c.direct ? "Profil" : "Osoby w czacie"}">
      ${c.direct
      ? presenceAvatar(c.avatar, c.name, c.peer, c.members.find((m) => !m.me)?.online)
      : avatar("", c.name, c.id, "sq")}
      <div class="titles">
        <div class="t">${esc(c.name)}</div>
        <div class="s">${esc(sub)}${relayTag}${c.hidden ? ` · <em>${c.hidden} schowanych</em>` : ""}</div>
      </div>
    </div>
    <button class="icon-btn" data-act="search-chat" title="Szukaj">${icon("search")}</button>
    <button class="icon-btn" data-act="invite" title="Zaproś">${icon("key")}</button>
    ${c.call.joined ? "" : `<button class="icon-btn" data-act="join-call" title="Zadzwoń">${icon("phone")}</button>`}
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

  const mePub = S.snap.me.pub;
  let out = "", lastDay = "", prev = null;
  c.msgs.forEach((m, i) => {
    const d = dayLabel(m.ts);
    if (d !== lastDay) { out += `<div class="day">${esc(d)}</div>`; lastDay = d; prev = null; }
    out += msgHTML(m, prev, mePub, !opened && i > lastKnown && !seenIds.has(m.id));
    prev = m;
  });
  seenIds = new Set(c.msgs.map((m) => m.id));
  list.innerHTML = out || `<div class="empty" style="margin:auto"><strong>Cisza</strong>Napisz pierwszą wiadomość.</div>`;

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
    if (S.builtChat !== null) { host.innerHTML = welcomeHTML; S.builtChat = null; }
    shell?.classList.remove("with-members");
    $("#members").innerHTML = "";
    return;
  }
  shell?.classList.toggle("with-members", !c.direct);

  if (rebuilt) buildConv();
  updateHead(c);
  $("#callbar-slot").innerHTML = callBar(c);
  $("#typing").innerHTML = typingHTML(c);
  paintMembers(c);
  paintStage();
}

function wireMsgScroll(list) {
  const jump = $(".jump");
  let loading = false;
  const update = () => {
    const bottom = list.scrollTop + list.clientHeight >= list.scrollHeight - 60;
    jump.hidden = bottom;
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
  if (!host || c.direct) { if (host) host.innerHTML = ""; return; }
  const online = c.members.filter((m) => m.online);
  const offline = c.members.filter((m) => !m.online);
  const row = (m) => `<button class="member ${m.online ? "" : "off"}" data-act="profile" data-pub="${m.pub}">
      ${presenceAvatar(m.avatar, m.name, m.pub, m.online, "s")}
      <span class="nm"><b>${esc(m.name)}</b><span>${esc(m.status || (m.owner ? "właściciel" : ""))}</span></span>
      ${m.inCall ? `<span class="live">${icon("phone")}</span>` : ""}
      ${m.owner ? `<span class="tag">OWNER</span>` : ""}
    </button>`;
  host.innerHTML = `
    <div class="scroll">
      <div class="sec">Online — ${online.length}</div>
      <div class="rows">${online.map(row).join("")}</div>
      ${offline.length ? `<div class="sec">Offline — ${offline.length}</div>
        <div class="rows">${offline.map(row).join("")}</div>` : ""}
    </div>`;
}

/* ---------------------------- video tiles ---------------------------- */

function paintStage() {
  const stage = $("#stage");
  if (!stage || !S.conv) return;
  const now = Date.now();
  const mine = [...S.frames.entries()].filter(([k, f]) => k.startsWith(S.chat + ".") && now - f.at < 4000);
  stage.hidden = mine.length === 0;
  if (stage.hidden) { stage.innerHTML = ""; return; }

  const want = new Set(mine.map(([k]) => k));
  for (const tile of [...stage.children]) if (!want.has(tile.dataset.key)) tile.remove();

  for (const [key, f] of mine) {
    let tile = stage.querySelector(`[data-key="${CSS.escape(key)}"]`);
    if (!tile) {
      tile = document.createElement("div");
      tile.className = "tile" + (f.kind === "screen" ? " screen" : "");
      tile.dataset.key = key;
      tile.innerHTML = `<img alt=""><span class="cap"></span>`;
      stage.append(tile);
    }
    tile.querySelector(".cap").textContent = f.name + (f.kind === "screen" ? " · ekran" : "");
    tile.querySelector("img").src = `/frame/${key}?n=${f.seq}`;
  }
}

setInterval(() => { if (S.frames.size) paintStage(); }, 1500);

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
  { n: "/rekey", d: "rotacja klucza — nowa epoka" },
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
  "/szukaj": (a) => searchDialog(a), "/search": (a) => searchDialog(a),
  "/kto": () => membersDialog(), "/who": () => membersDialog(),
  "/kick": cmdKick,
  "/rekey": () => act("Rekey", S.chat),
  "/zmien": cmdRename, "/rename": cmdRename,
  "/call": () => act("JoinCall", S.chat),
  "/rozlacz": () => act("LeaveCall", S.chat), "/leave": () => act("LeaveCall", S.chat),
  "/hangup": () => act("LeaveCall", S.chat),
  "/mic": () => act("ToggleMic", S.chat),
  "/cam": () => act("ToggleCam", S.chat),
  "/ekran": () => act("ToggleScreen", S.chat), "/screen": () => act("ToggleScreen", S.chat),
  "/wyslij": () => act("PickFiles", S.chat, ""), "/send": () => act("PickFiles", S.chat, ""),
  "/awatar": () => pickPicture("avatar"), "/avatar": () => pickPicture("avatar"),
  "/connector": cmdConnector,
  "/ja": () => openSettings("profile"), "/me": () => openSettings("profile"),
  "/lang": cmdLang, "/jezyk": cmdLang,
};

// act: fire a bound method and surface its error as a toast.
async function act(method, ...args) {
  try { await api(method, ...args); } catch (e) { fail(e); }
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
(function wireDrop() {
  let depth = 0;
  const drop = () => $("#drop");
  document.addEventListener("dragenter", (e) => {
    e.preventDefault();
    if (++depth === 1 && drop()) drop().hidden = false;
  });
  document.addEventListener("dragover", (e) => e.preventDefault());
  document.addEventListener("dragleave", () => { if (--depth <= 0) { depth = 0; if (drop()) drop().hidden = true; } });
  document.addEventListener("drop", async (e) => {
    e.preventDefault();
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
    if (!run) return toast(`Nie znam komendy ${name} — wpisz /pomoc.`, "err");
    return run(rest.join(" ").trim());
  }
  if (!S.chat) return toast("Najpierw wybierz czat.", "err");
  clearTimeout(typingTimer);
  api("Typing", S.chat, false).catch(() => {});
  try { await api("Send", S.chat, text); } catch (e) { fail(e); input.value = text; }
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
    hint: "Kod dostaniesz od drugiej osoby — u niej to „Twój kod”.", ok: "Dodaj",
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
          <p class="hint-text">Przekaż PIN innym kanałem niż kod — dopiero razem wpuszczają do grupy.</p></div>
        <div class="btn-row"><button class="btn" data-act="copy" data-text="${esc(inv.code)}">${icon("copy")}Kopiuj kod</button></div>`,
      actions: [{ label: "Gotowe", kind: "primary", value: null }],
    });
  } catch (e) { fail(e); }
}

async function searchDialog(prefill = "") {
  if (!S.chat) return toast("Najpierw wybierz czat.", "err");
  const run = async (q) => {
    const box = $("#sr");
    if (!q) { box.innerHTML = `<p class="hint-text" style="padding:0 16px">Wpisz frazę.</p>`; return; }
    try {
      const hits = await api("Search", S.chat, q);
      box.innerHTML = hits.length
        ? hits.map((m) => `<div class="list-line" style="display:block">
             <div style="font-size:12px;color:var(--muted)">${esc(m.name)} · ${hhmm(m.ts)}</div>
             <div style="user-select:text">${body(m.text)}</div></div>`).join("")
        : `<p class="hint-text" style="padding:0 16px">Nic nie znalazłem.</p>`;
    } catch (e) { fail(e); }
  };
  modal({
    title: "Szukaj w rozmowie",
    wide: true,
    body: `<div class="field"><input class="input" id="sq" value="${esc(prefill)}" placeholder="fraza…"></div>
      <div class="block" style="margin:0 16px 12px" id="sr"></div>`,
    actions: [{ label: "Zamknij", kind: "ghost", value: null }],
    onMount: (box) => {
      const q$ = $("#sq", box);
      let t = null;
      q$.addEventListener("input", () => { clearTimeout(t); t = setTimeout(() => run(q$.value.trim()), 200); });
      run(prefill.trim());
    },
  });
}

function membersDialog() {
  if (!S.conv) return toast("Najpierw wybierz czat.", "err");
  modal({
    title: `Członkowie — ${S.conv.name}`,
    body: `<div class="rows" style="padding:0 8px 8px">${S.conv.members.map((m) =>
      `<button class="member" data-act="profile" data-pub="${m.pub}">
         ${presenceAvatar(m.avatar, m.name, m.pub, m.online, "s")}
         <span class="nm"><b>${esc(m.name)}</b><span>${esc(m.status || (m.online ? "online" : "offline"))}</span></span>
         ${m.owner ? `<span class="tag">OWNER</span>` : ""}
       </button>`).join("")}</div>`,
    actions: [{ label: "Zamknij", kind: "ghost", value: null }],
  });
}

async function profileDialog(pub) {
  try {
    const p = await api("ProfileOf", pub);
    const canKick = S.conv && !S.conv.direct && S.conv.isOwner && !p.isMe;
    const hist = p.history.length > 1
      ? `<div class="sec-label">Historia nazw</div>${p.history.map((h, i) =>
          `<div class="list-line"><span class="mono">${esc(h.name)}</span>
             ${i === p.history.length - 1 ? `<span class="tag">TERAZ</span>` : ""}
             <span class="when">${new Date(h.ts * 1000).toLocaleDateString("pl-PL")}</span></div>`).join("")}`
      : "";
    modal({
      // the name is spelled out in the card 60px below — repeating it in the
      // header just makes the sheet look like it stutters
      title: "Profil",
      body: `
        <div class="card-banner" ${p.banner ? `style="background-image:url('${esc(p.banner)}')"` : ""}></div>
        <div class="card-id">
          ${avatar(p.avatar, p.name, p.pub, "xl")}
          <div class="nm">${esc(p.name || "nieznany")}</div>
          <div class="st">${esc(p.status || (p.online ? "online" : "offline"))}</div>
          ${p.bio ? `<div class="bio">${esc(p.bio)}</div>` : ""}
        </div>
        <div class="btn-row">
          ${p.isMe ? `<button class="btn" data-act="settings" data-section="profile">${icon("edit")}Edytuj profil</button>` : `
            <button class="btn primary" data-act="dm" data-pub="${p.pub}">${icon("chat")}Napisz</button>
            ${p.isFriend
              ? `<button class="btn danger" data-act="unfriend" data-pub="${p.pub}">${icon("trash")}Usuń ze znajomych</button>`
              : ""}
            ${canKick ? `<button class="btn danger" data-act="kick" data-pub="${p.pub}">${icon("logout")}Wyrzuć z czatu</button>` : ""}`}
        </div>
        ${hist}`,
      actions: [{ label: "Zamknij", kind: "ghost", value: null }],
    });
  } catch (e) { fail(e); }
}

async function chatMenu() {
  const c = S.conv;
  if (!c) return;
  modal({
    title: c.name,
    body: `
      <button class="nav-row" data-act="rename"><span class="ico">${icon("edit")}</span>
        <span class="tx"><b>Zmień nazwę</b><span>widoczna dla wszystkich</span></span></button>
      <button class="nav-row" data-act="invite"><span class="ico">${icon("key")}</span>
        <span class="tx"><b>Zaproszenie</b><span>kod + jednorazowy PIN</span></span></button>
      <button class="nav-row" data-act="rekey"><span class="ico">${icon("shield")}</span>
        <span class="tx"><b>Obróć klucz</b><span>nowa epoka — stare klucze przestają działać</span></span></button>
      ${c.hidden ? `<button class="nav-row" data-act="unhide"><span class="ico">${icon("eye")}</span>
        <span class="tx"><b>Pokaż schowane</b><span>${c.hidden} wiadomości</span></span></button>` : ""}
      <button class="nav-row" data-act="members"><span class="ico">${icon("people")}</span>
        <span class="tx"><b>Członkowie</b><span>${c.members.length} osób</span></span></button>`,
    actions: [{ label: "Zamknij", kind: "ghost", value: null }],
  });
}

/* ---------------------------- picture crop ---------------------------- */

// pickPicture: file chooser -> pan/zoom framing -> Go. The crop rectangle is
// reported in *source* pixels, which is exactly what attach.Avatar wants.
function pickPicture(kind) {
  const input = document.createElement("input");
  input.type = "file";
  input.accept = "image/*";
  input.onchange = async () => {
    const file = input.files?.[0];
    if (!file) return;
    try { await cropDialog(kind, await readAsDataURL(file)); } catch (e) { fail(e); }
  };
  input.click();
}

function cropDialog(kind, dataURL) {
  const round = kind === "avatar";
  const ratio = round ? 1 : 3;
  return modal({
    title: round ? "Wykadruj awatar" : "Wykadruj baner",
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
    if (!r) return;
    try {
      if (r === "clear") await api(round ? "ClearAvatar" : "ClearBanner");
      else await api(round ? "SetAvatar" : "SetBanner", dataURL, r);
      await afterEdit("Zapisane");
    } catch (e) { fail(e); }
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
  { id: "profile", t: "Profil", d: "nazwa, status, obrazki", i: "user" },
  { id: "connector", t: "Connector", d: "serwer spotkań i listy", i: "server" },
  { id: "voice", t: "Głos i wideo", d: "głośność, kamera", i: "volume" },
  { id: "look", t: "Wygląd", d: "kolor akcentu", i: "palette" },
  { id: "lang", t: "Język", d: "pl / en", i: "globe" },
  { id: "accounts", t: "Konta", d: "profile na tym urządzeniu", i: "people" },
  { id: "security", t: "Hasło i kopia", d: "backup, restore", i: "shield" },
  { id: "bots", t: "Boty", d: "webhook HTTP", i: "bot" },
  { id: "about", t: "O szepcie", d: "wersja", i: "chat" },
];

async function openSettings(section = "profile") {
  closeModal(null);
  S.screen = "settings";
  S.section = section;
  S.settings = await api("Settings");
  paintStatus();
  renderSettings();
}

function closeSettings() {
  S.screen = "app";
  S.builtChat = undefined;
  S.listSig = "";
  renderShell();
  paint();
}

const ACCENTS = ["8b7cf6", "5865f2", "3fbf7f", "eeb134", "ee4d55", "e879f9", "22d3ee", "f97316"];

function settingsBody() {
  const s = S.settings, me = S.snap.me;
  switch (S.section) {
    case "profile": return `
      <h2>Profil</h2><p class="lead">Wszystko tutaj jest podpisane twoim kluczem i trafia do każdego czatu, w którym jesteś.</p>
      <div class="block">
        <div class="card-banner" ${me.banner ? `style="background-image:url('${esc(me.banner)}')"` : ""}>
          <button class="icon-btn edit" data-act="edit-banner">${icon("edit")}</button>
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
          <button class="btn ghost" data-act="edit-avatar">Awatar</button>
          <button class="btn ghost" data-act="edit-banner">Baner</button>
        </div>
      </div>
      <div class="block">
        <h4>Twój kod</h4>
        <p class="hint-text">Wizytówka: klucz publiczny plus podpowiedź, przez który connector cię szukać.</p>
        <div class="code-box" style="margin:10px 16px">${esc(me.code)}</div>
        <div class="btn-row"><button class="btn" data-act="copy" data-text="${esc(me.code)}">${icon("copy")}Kopiuj</button>
          <button class="btn ghost" data-act="my-code">${icon("qr")}QR</button></div>
      </div>`;

    case "connector": return `
      <h2>Connector</h2><p class="lead">Lekki serwer spotkań: kojarzy peerów i przekazuje sygnalizację. Treści nie widzi — ruch idzie P2P i jest szyfrowany.</p>
      <div class="block">
        <h4>Aktywny</h4>
        <div class="field"><input class="input" id="s-conn" value="${esc(s.connector)}" placeholder="ws://twoj-vps:7777"></div>
        <p class="hint-text" style="padding:0 16px">Stan: ${me.connUp ? "połączony" : esc(me.connErr || "łączenie…")}</p>
        <div class="btn-row"><button class="btn primary" data-act="save-conn">Zapisz i połącz</button></div>
      </div>
      <div class="block">
        <div class="toggle"><div class="tx"><b>Ukryj mój adres IP</b><span>połączenia idą tylko przez przekaźnik connectora, rozmówcy widzą jego adres zamiast twojego. Wolniej. ${me.connRelay ? "Ten connector ma przekaźnik." : "Ten connector nie ma przekaźnika — z włączoną opcją nie połączysz się z nikim."}</span></div>
          <button class="switch ${s.relayOnly ? "on" : ""}" data-act="toggle-relay"></button></div>
        <div class="toggle"><div class="tx"><b>Ukryj listę kontaktów przed connectorem</b><span>Normalnie aplikacja co 30 s pyta connector o każdy kontakt, więc connector zna całą Twoją listę kontaktów. Z tą opcją pyta tylko o osoby z czatów otwartych albo używanych w ostatnich 15 minutach. Kropki obecności pozostałych kontaktów nie będą aktualne.</span></div>
          <button class="switch ${s.quietLookup ? "on" : ""}" data-act="toggle-quiet"></button></div>
      </div>
      <div class="block">
        <h4>Zapisane adresy</h4>
        ${s.connectors.length
          ? s.connectors.map((c) => `<div class="list-line"><span class="mono">${esc(c)}</span>
              ${c === s.connector ? `<span class="tag">AKTYWNY</span>` : `<button class="btn ghost" data-act="use-conn" data-v="${esc(c)}">Użyj</button>`}
              <button class="icon-btn" data-act="del-conn" data-v="${esc(c)}">${icon("trash")}</button></div>`).join("")
          : `<p class="hint-text" style="padding:0 16px">Lista pusta.</p>`}
        <div class="field"><label>Dodaj</label><input class="input" id="s-newconn" placeholder="ws://inny-vps:7777"></div>
        <div class="btn-row"><button class="btn" data-act="add-conn">${icon("plus")}Dodaj</button></div>
      </div>
      <div class="block">
        <h4>Dynamiczne listy</h4>
        <p class="hint-text">Adres HTTP zwracający tekst — jeden connector (ws://…) na linię.</p>
        ${s.connectorLists.map((l) => `<div class="list-line"><span class="mono">${esc(l)}</span>
            <button class="icon-btn" data-act="del-list" data-v="${esc(l)}">${icon("trash")}</button></div>`).join("")}
        <div class="field"><label>Dodaj listę</label><input class="input" id="s-newlist" placeholder="https://example.com/connectors.txt"></div>
        <div class="btn-row"><button class="btn" data-act="add-list">${icon("plus")}Dodaj</button>
          ${s.connectorLists.length ? `<button class="btn ghost" data-act="refresh-lists">Odśwież teraz</button>` : ""}</div>
      </div>`;

    case "voice": return `
      <h2>Głos i wideo</h2><p class="lead">Rozmowy idą pełną siatką P2P — każdy wysyła do każdego. Wygodnie do ok. ośmiu osób.</p>
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

    case "look": return `
      <h2>Wygląd</h2><p class="lead">Jeden kolor akcentu przechodzi przez cały interfejs: przyciski, dymki, wskaźniki.</p>
      <div class="block">
        <h4>Motyw</h4>
        <p class="hint-text">Automatyczny idzie za ustawieniem pulpitu — jasny wieczorem sam zgaśnie.</p>
        <div class="segmented">
          ${[["auto", "Automatyczny"], ["light", "Jasny"], ["dark", "Ciemny"]].map(([v, t]) =>
            `<button class="${themePref === v ? "sel" : ""}" data-act="theme" data-v="${v}">${t}</button>`).join("")}
        </div>
      </div>
      <div class="block">
        <h4>Kolor akcentu</h4>
        <div class="swatches">${ACCENTS.map((c) =>
          `<button class="swatch ${(s.accent || "8b7cf6").toLowerCase() === c ? "sel" : ""}" style="background:#${c}" data-act="accent" data-v="${c}"></button>`).join("")}</div>
        <div class="field"><label>Własny (hex)</label><input class="input" id="s-accent" value="${esc(s.accent || "8b7cf6")}" placeholder="8b7cf6"></div>
        <div class="btn-row"><button class="btn primary" data-act="save-accent">Zastosuj</button></div>
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
              ${p === s.profile ? `<span class="tag">AKTYWNE</span>`
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
        <p class="hint-text" style="padding:0 16px 8px">Przywracanie nigdy nie nadpisuje obecnych danych — kopia ląduje jako nowe konto.</p>
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
      </div>`;
  }
}

function renderSettings() {
  view.innerHTML = `
    <div class="settings">
      <nav class="set-nav">
        <div class="pane-head">
          <button class="icon-btn" data-act="close-settings" title="Wróć">${icon("x")}</button>
          <h2>Ustawienia</h2>
        </div>
        ${SECTIONS.map((x) => `<button class="nav-row ${x.id === S.section ? "active" : ""}" data-act="section" data-id="${x.id}">
            <span class="ico">${icon(x.i)}</span>
            <span class="tx"><b>${esc(x.t)}</b><span>${esc(x.d)}</span></span>
            <span class="chev">${icon("chev")}</span>
          </button>`).join("")}
      </nav>
      <div class="set-body"><div class="wrap">${settingsBody()}</div></div>
    </div>`;

  const vol = $("#s-vol");
  vol?.addEventListener("input", () => {
    $("#s-vol-l").textContent = Math.round(vol.value * 100) + "%";
    api("SetOutputVolume", parseFloat(vol.value)).catch(() => {});
  });
}

/* ============================ settings actions ============================ */

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
  const clean = /^[0-9a-fA-F]{6}$/.test(hex || "") ? hex : "8b7cf6";
  const lin = (c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
  const [r, g, b] = [0, 2, 4].map((i) => lin(parseInt(clean.slice(i, i + 2), 16) / 255));
  const pale = 0.2126 * r + 0.7152 * g + 0.0722 * b > 0.32;

  const css = document.documentElement.style;
  css.setProperty("--accent", "#" + clean);
  css.setProperty("--on-accent", pale ? "#101319" : "#fff");
  css.setProperty("--accent-fill", pale ? "#" + clean : `color-mix(in oklab, #${clean} 82%, #000)`);
}

/* ============================ actions ============================ */

async function openChat(id) {
  if (!id) return;
  S.chat = id;
  S.tab = "chats";
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

let refreshing = false, again = false;

async function refresh() {
  if (refreshing) { again = true; return; }
  refreshing = true;
  try {
    S.snap = await api("Snapshot");
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
  $(".txt", bar).textContent = me.connErr
    ? "błąd: " + me.connErr
    : me.connUp ? "połączony · " + me.connector : "łączenie z connectorem…";
  $("#tb-sub").textContent = me.profile ? "· " + me.profile : "";
}

// paint is the single repaint entry point. Each pane has its own signature so
// a background sync only touches what actually moved — settings and the
// composer are never rebuilt underneath the user's cursor.
function paint() {
  if (S.stage !== "ready") return;
  paintStatus();
  if (S.screen === "settings") return; // rendered on demand, never by a sync
  if (!$("#shell")) { renderShell(); S.builtChat = undefined; S.listSig = ""; }
  paintRail();

  const listSig = JSON.stringify([S.tab, S.filter, S.chat, S.snap.chats, S.snap.friends,
    S.tab === "me" ? S.snap.me : 0]);
  if (listSig !== S.listSig) { S.listSig = listSig; paintList(); }

  const rebuilt = S.builtChat !== S.chat && !!S.conv;
  if (rebuilt) { S.builtChat = S.chat; S.msgSig = ""; }
  const sig = msgSignature(S.conv);
  paintConv(rebuilt);
  if (S.conv && (rebuilt || sig !== S.msgSig)) { S.msgSig = sig; updateMsgs(S.conv); }
}

/* ============================ click routing ============================ */

const HANDLERS = {
  /* gate */
  "do-setup": async () => {
    const pass = $("#g-pass").value;
    // seizure of the device is in the threat model: no password has to be a
    // deliberate choice, never a skipped field
    if (!pass && !(await confirmBox("Bez hasła?",
      "Każdy, kto weźmie to urządzenie (policja na granicy, złodziej, ktoś z domowników), przeczyta cały klucz i całą historię rozmów. Hasło szyfruje jedno i drugie. Kontynuować bez hasła?"))) {
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
  tab: (el) => { S.tab = el.dataset.tab; S.filter = ""; paint(); },
  settings: (el) => openSettings(el.dataset.section || "profile"),
  "close-settings": () => closeSettings(),
  section: (el) => { S.section = el.dataset.id; renderSettings(); },
  open: (el) => openChat(el.dataset.chat),
  friend: (el) => profileDialog(el.dataset.pub),
  profile: (el) => { closeModal(null); profileDialog(el.dataset.pub); },
  dm: async (el) => {
    closeModal(null);
    const f = S.snap.friends.find((x) => x.pub === el.dataset.pub);
    try { openChat(f?.chat || await api("CreateDirect", el.dataset.pub)); } catch (e) { fail(e); }
  },
  // Only this jump glides — the automatic scroll after a new message must stay
  // instant, or the list would still be travelling when the next one lands.
  jump: () => $("#msgs").scrollTo({ top: $("#msgs").scrollHeight, behavior: "smooth" }),

  /* modals — the backdrop only dismisses when the click misses the dialog */
  "modal-dismiss": (el, e) => {
    if (el.classList.contains("backdrop") && e.target !== el) return;
    closeModal(null);
  },
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
  "search-chat": () => searchDialog(""),
  "chat-menu": () => chatMenu(),
  rename: () => { closeModal(null); cmdRename(""); },
  members: () => { closeModal(null); membersDialog(); },
  rekey: async () => {
    closeModal(null);
    if (await confirmBox("Obrócić klucz?", "Powstanie nowa epoka; peerzy dostaną nowy klucz przy najbliższym kontakcie.", false)) {
      try { await api("Rekey", S.chat); toast("Klucz obrócony", "ok"); } catch (e) { fail(e); }
    }
  },
  unhide: async () => { closeModal(null); try { await api("UnhideAll", S.chat); } catch (e) { fail(e); } },
  kick: async (el) => {
    closeModal(null);
    if (await confirmBox("Wyrzucić z czatu?", "Klucz zostanie obrócony, żeby usunięty nie czytał dalej.")) {
      try { await api("Kick", S.chat, el.dataset.pub); } catch (e) { fail(e); }
    }
  },
  unfriend: async (el) => {
    closeModal(null);
    if (await confirmBox("Usunąć ze znajomych?", "Historia rozmów zostaje — znika tylko wpis w książce adresowej.")) {
      try { await api("DelFriend", el.dataset.pub); } catch (e) { fail(e); }
    }
  },

  /* messages */
  react: async (el) => { try { await api("React", S.chat, el.dataset.id, el.dataset.emoji); } catch (e) { fail(e); } },
  "react-more": (el) => {
    modal({
      title: "Reakcja",
      body: `<div class="btn-row" style="font-size:24px">${REACT_SET.concat(["🔥", "👀", "🙏", "💜", "😅", "🤝"]).map((e) =>
        `<button class="btn ghost" data-act="react" data-id="${el.dataset.id}" data-emoji="${e}">${e}</button>`).join("")}</div>`,
      actions: [{ label: "Zamknij", kind: "ghost", value: null }],
    });
  },
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
  "join-call": () => act("JoinCall", S.chat),
  hangup: () => act("LeaveCall", S.chat),
  mic: () => act("ToggleMic", S.chat),
  deaf: () => act("ToggleDeafen", S.chat),
  cam: () => act("ToggleCam", S.chat),
  screen: () => act("ToggleScreen", S.chat),
  spatial: () => spatialDialog(),

  /* profile edits */
  "edit-nick": async () => {
    const v = await promptBox({ title: "Nazwa", label: "Jak mają cię widzieć", value: S.snap.me.name });
    if (v) { try { await api("SetNick", v); await afterEdit("Zapisane"); } catch (e) { fail(e); } }
  },
  "edit-status": async () => {
    const v = await promptBox({ title: "Status", label: "Krótka linijka", value: S.snap.me.status, placeholder: "np. koduję" });
    if (v !== null) { try { await api("SetStatus", v); await afterEdit("Zapisane"); } catch (e) { fail(e); } }
  },
  "edit-bio": async () => {
    const v = await promptBox({ title: "Opis", label: "O tobie", value: S.snap.me.bio, multiline: true });
    if (v !== null) { try { await api("SetBio", v); await afterEdit("Zapisane"); } catch (e) { fail(e); } }
  },
  "edit-avatar": () => pickPicture("avatar"),
  "edit-banner": () => pickPicture("banner"),
  copy: async (el) => {
    try { await navigator.clipboard.writeText(el.dataset.text); toast("Skopiowane", "ok"); }
    catch { toast("Nie udało się skopiować", "err"); }
  },

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
    lang: $("#s-lang")?.value ?? S.settings.lang,
  }),
  "toggle-notify": () => saveSettings({ noNotify: !S.settings.noNotify }),
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
  e.preventDefault();
  e.stopPropagation();
  fn(el, e);
});

document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    if ($("#modal-root").innerHTML) return closeModal(null);
    if ($(".lightbox")) return $(".lightbox").remove();
    if (S.screen === "settings") return closeSettings();
  }
  if (e.key === "f" && (e.ctrlKey || e.metaKey) && S.chat) { e.preventDefault(); searchDialog(""); }
  if (e.key === "k" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); $("#q")?.focus(); }
});

/* ---------------------------- spatial audio ---------------------------- */

// Drag a participant around the room to place them in the stereo field.
// Distance drops the gain, horizontal offset pans; the mapping mirrors the
// Fyne client's spatial page so both UIs sound the same.
function spatialDialog() {
  if (!S.conv) return;
  const peers = S.conv.members.filter((m) => m.inCall && !m.me);
  if (!peers.length) return toast("Nikogo jeszcze nie ma w rozmowie.");
  modal({
    title: "Przestrzeń dźwięku",
    wide: true,
    body: `<p>Przeciągnij osobę: bliżej środka = głośniej, w bok = w lewy lub prawy głośnik.</p>
      <div id="room" style="position:relative;height:280px;margin:0 16px 12px;border-radius:var(--r-m);
        background:radial-gradient(62% 80% at 50% 100%, color-mix(in srgb,var(--accent) 18%,transparent), var(--sunk));
        box-shadow:inset 0 0 0 1px var(--line)"></div>`,
    actions: [{ label: "Gotowe", kind: "ghost", value: null }],
    onMount: (box) => {
      const room = $("#room", box);
      peers.forEach((m, i) => {
        const t = document.createElement("button");
        t.style.cssText = "position:absolute;translate:-50% -50%;transition:none;cursor:grab;touch-action:none";
        t.innerHTML = avatar(m.avatar, m.name, m.pub, "s");
        const px = (i + 1) / (peers.length + 1), py = 0.5;
        place(px, py);
        room.append(t);
        function place(fx, fy) {
          t.dataset.fx = fx; t.dataset.fy = fy;
          t.style.left = fx * 100 + "%";
          t.style.top = fy * 100 + "%";
          t.style.scale = String(0.7 + (1 - fy) * 0.1 + 0.3 * (1 - Math.abs(fx - 0.5) * 2) * 0.5);
          const pan = Math.max(-1, Math.min(1, (fx - 0.5) * 2));
          const gain = Math.max(0.15, 1.25 - fy * 0.9);
          api("SetVoice", m.pub, gain, pan).catch(() => {});
        }
        let dragging = false;
        t.addEventListener("pointerdown", (e) => {
          dragging = true; t.setPointerCapture(e.pointerId);
          t.style.zIndex = "2"; t.style.cursor = "grabbing";
        });
        t.addEventListener("pointermove", (e) => {
          if (!dragging) return;
          const r = room.getBoundingClientRect();
          place(
            Math.max(0.06, Math.min(0.94, (e.clientX - r.left) / r.width)),
            Math.max(0.08, Math.min(0.92, (e.clientY - r.top) / r.height)),
          );
        });
        t.addEventListener("pointerup", () => {
          dragging = false; t.style.zIndex = ""; t.style.cursor = "grab";
        });
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

Events.On("frame", (e) => {
  const d = e.data;
  const name = S.conv?.members.find((m) => m.pub === d.from)?.name || "peer";
  S.frames.set(d.key, { seq: d.seq, kind: d.kind, name, at: Date.now() });
  if (d.chat === S.chat) paintStage();
});

Events.On("frames-cleared", (e) => {
  for (const k of [...S.frames.keys()]) if (k.startsWith(e.data + ".")) S.frames.delete(k);
  paintStage();
});

// The connector state changes without touching the DAG, so poll it gently.
setInterval(() => { if (S.stage === "ready" && !refreshing) refresh(); }, 4000);

boot().catch((e) => {
  view.innerHTML = `<div class="gate"><div class="gate-card"><div class="panel">
      <h3>szept nie wstał</h3><p class="hint-text err" style="padding:0 16px 16px">${esc(errText(e))}</p>
    </div></div></div>`;
});
