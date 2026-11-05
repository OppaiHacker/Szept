// Slash commands typed into the composer, Polish and English names side by side.
import { api } from "@/api/wails";
import type { Settings } from "@/api/types";
import { pickFiles } from "@/lib/files";
import { confirmBox } from "@/ui/dialogs";
import { fail, toast } from "@/ui/toast";
import { S } from "./state";
import { callKey } from "./getters";
import { act, openChat, searchChat } from "./actions";
import { joinVoice, startVideo } from "./calls";
import {
  addFriendDialog, helpDialog, inviteDialog, joinDialog, membersDialog, myCodeDialog,
  newGroupDialog, pickPicture, renameDialog,
} from "./dialogs";
import { openSettings } from "./settings";

export interface Command { n: string; a?: string; d: string }

export const COMMANDS: Command[] = [
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

async function dm(arg: string) {
  const f = S.snap!.friends.find((x) => x.name.toLowerCase() === arg.toLowerCase());
  if (!f) return toast("Nie znam takiego znajomego: " + arg, "err");
  try { openChat(f.chat || await api<string>("CreateDirect", f.pub)); } catch (e) { fail(e); }
}

async function kick(arg: string) {
  const m = S.conv?.members.find((x) => x.name.toLowerCase() === arg.toLowerCase());
  if (!m) return toast("Nie ma tu nikogo o nazwie " + arg, "err");
  if (await confirmBox("Usunąć członka", `Usunąć ${m.name}? Klucz czatu zostanie obrócony.`)) {
    try { await api("Kick", S.chat, m.pub); toast("Usunięty"); } catch (e) { fail(e); }
  }
}

// a setting changed right from the composer
async function setting(patch: Partial<Settings>, said: string) {
  const s = await api<Settings>("Settings");
  try { await api("SaveSettings", { ...s, ...patch }); toast(said, "ok"); } catch (e) { fail(e); }
}

function connector(arg: string) {
  if (!arg) return toast("Connector: " + (S.snap!.me.connector || "brak"));
  return setting({ connector: arg }, "Connector: " + arg);
}

function lang(arg: string) {
  const l = arg.toLowerCase();
  if (l !== "pl" && l !== "en") return toast("Użycie: /lang pl albo /lang en", "err");
  return setting({ lang: l }, "Język: " + l);
}

// the composer stages chosen files instead of sending them
async function attach() {
  if (!S.chat) return toast("Najpierw wybierz czat.", "err");
  const files = await pickFiles({ multiple: true });
  S.files = [...S.files, ...files];
}

const leaveCall = () => act("LeaveCall", callKey());

const CMD: Record<string, (arg: string) => unknown> = {
  "/pomoc": helpDialog, "/help": helpDialog,
  "/nowy": newGroupDialog, "/new": newGroupDialog,
  "/dm": dm,
  "/kod": myCodeDialog, "/code": myCodeDialog,
  "/znajomy": addFriendDialog, "/friend": addFriendDialog,
  "/dolacz": joinDialog, "/join": joinDialog,
  "/zapros": inviteDialog, "/invite": inviteDialog,
  "/szukaj": searchChat, "/search": searchChat,
  "/kto": membersDialog, "/who": membersDialog,
  "/kick": kick,
  "/rekey": () => act("Rekey", S.chat),
  "/zmien": renameDialog, "/rename": renameDialog,
  "/call": () => S.chat && joinVoice(S.chat, ""),
  "/rozlacz": leaveCall, "/leave": leaveCall, "/hangup": leaveCall,
  "/mic": () => act("ToggleMic", callKey()),
  "/cam": () => startVideo(null, "cam", callKey()),
  "/ekran": () => startVideo(null, "screen", callKey()), "/screen": () => startVideo(null, "screen", callKey()),
  "/wyslij": attach, "/send": attach,
  "/awatar": () => pickPicture("avatar"), "/avatar": () => pickPicture("avatar"),
  "/connector": connector,
  "/ja": () => openSettings("profile"), "/me": () => openSettings("profile"),
  "/lang": lang, "/jezyk": lang,
};

export function runCommand(text: string) {
  const [name, ...rest] = text.split(" ");
  const fn = CMD[name.toLowerCase()];
  if (!fn) return toast(`Nie znam komendy ${name}. Wpisz /pomoc`, "err");
  fn(rest.join(" ").trim());
}

export { attach as attachFiles };
