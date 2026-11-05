import type { IconName } from "@/lib/icons";

// The settings menu. d: extra words the search box matches.
export type NavItem = { g: string } | { id: string; t: string; d: string; i: IconName };

export const SECTIONS: NavItem[] = [
  { g: "Ustawienia użytkownika" },
  { id: "profile", t: "Profil", d: "nazwa status awatar baner opis kod", i: "user" },
  { id: "devices", t: "Urządzenia", d: "telefon komputer połącz kod qr odłącz", i: "device" },
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

// sections that make no sense on the phone build
export const PHONE_OFF = new Set(["accounts", "bots"]);
