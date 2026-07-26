# szept

Komunikator **peer-to-peer** z pełnym szyfrowaniem end-to-end, napisany w Go.
TUI (terminal) + GUI (Fyne, Linux/Windows/Android). Bez centralnego serwera treści —
jedyny element infrastruktury to **connector**: lekki tracker (jak w torrentach),
który mówi peerom, jak się nawzajem znaleźć. Nigdy nie widzi treści.

## Możliwości

- znajomi (wymiana kodów/QR — kod **zawiera klucz publiczny**, więc nie ma MITM),
- czaty 1:1 i **grupowe kanały**; zaproszenia wystawia **tylko założyciel** czatu,
- **profile**: własna nazwa i awatar (zmiana rozchodzi się jako podpisane
  zdarzenie do wszystkich czatów), podgląd profilu znajomego i członka grupy,
- **dowolne pliki** w czacie — zdjęcia, GIF-y i filmy z podglądem, reszta jako
  kafelek do zapisania. Powyżej **10 MB** replikuje się tylko miniatura zrobiona
  u nadawcy; pełne bajty wędrują dopiero po naciśnięciu „pobierz” i wymagają,
  żeby ktoś, kto je ma, był online,
- **awatary** — także animowane GIF-y,
- **„ukryj u mnie”** (soft delete): wiadomość znika z tego urządzenia, a historia
  u pozostałych zostaje nietknięta — DAG jest podpisany, więc inaczej być nie może,
- **rozmowy głosowe**, **kamera** i **udostępnianie ekranu** — także w grupach (mesh),
- zaproszenia **jednorazowym kodem / QR**,
- każdy czat to replikowana, podpisana, szyfrowana baza zdarzeń (DAG z łańcuchem
  hashy — „jak blockchain”); po dołączeniu nowego członka historia synchronizuje
  się do niego automatycznie, a każde urządzenie trzyma pełną kopię,
- wszystko end-to-end: transport = WebRTC DTLS z **podpisywanym sygnalingiem**,
  treści = XChaCha20-Poly1305 kluczami epokowymi czatu, zdarzenia podpisane Ed25519,
- **obfuskacja ruchu**: każdy datagram peer↔peer jest szyfrowany strumieniem
  ChaCha20 kluczem z podpisanej oferty, więc na łączu to szum bez nagłówków
  STUN/DTLS i o zmiennej długości — DPI nie rozpozna WebRTC ani nie zablokuje
  połączenia po sygnaturze. Pakiety do publicznych serwerów STUN pozostają jawne
  (muszą się parsować jako STUN); mDNS wyłączony, by nic jawnego nie wyciekało.

## Budowanie (Linux)

Wymagania: Go ≥ 1.22, gcc, nagłówki X11/GL (dla GUI: `mesa`, `libx11`, `libxcursor`,
`libxrandr`, `libxinerama`, `libxi`), opcjonalnie `ffmpeg` (fallback kamery).

```sh
make          # bin/szept-connector, bin/szept (TUI), bin/szept-gui
make test
```

## Szybki start

1. **Connector** — na maszynie z publicznym adresem (VPS itp.):

   ```sh
   ./bin/szept-connector --listen :7777
   # TLS: --cert cert.pem --key key.pem (wtedy klienci używają wss://)
   ```

   Zapora musi przepuścić ten port — inaczej klient zobaczy `dial tcp …:7777:
   i/o timeout` (z samej maszyny `curl` zadziała, bo idzie po loopbacku).
   Test z innego urządzenia: `http://<adres>:7777` w przeglądarce.
   ufw: `sudo ufw allow from 192.168.0.0/24 to any port 7777 proto tcp`.

2. **Klient** (TUI albo GUI): przy pierwszym starcie podajesz nazwę, adres
   connectora (`ws://adres:7777` lub `wss://…`) i opcjonalne hasło do klucza.
   Tożsamość (Ed25519) ląduje zaszyfrowana w `~/.local/share/szept/identity.key`.

3. **Znajomi**: `/mojkod` (TUI) albo Znajomi→„Twój kod” (GUI) → wyślij komuś
   kod/QR dowolnym kanałem → druga strona `/znajomy <kod>`. Obie strony dodają
   się nawzajem, potem `/dm <nazwa>` otwiera czat.

4. **Grupa**: `/nowy nazwa` → `/zapros` generuje **jednorazowy** kod + QR →
   nowa osoba `/dolacz <kod>` (zapraszający musi być online — to on wydaje
   klucze i dodaje członka; historia dosynchronizuje się sama).

5. **Rozmowa**: w czacie `/call` (głos), `/cam` (kamera), `/ekran` (pulpit),
   `/mic` mute/unmute, `/rozlacz`. W GUI — przyciski na górnym pasku.
   Grupowo działa jako pełny mesh (sensownie do ~8 osób).

6. **Zdjęcia, GIF-y, filmy**: GUI — spinacz przy polu wiadomości (tekst w polu
   staje się podpisem); TUI — `/wyslij <ścieżka> [podpis]`, `/zapisz <nr>`.
   W DAG-u ląduje sam skrót pliku, treść odbiorca dociąga sam i sprawdza —
   nadawca (albo ktokolwiek, kto już plik ma) musi być w tym momencie online.
   Limit 32 MB. Filmy otwiera systemowy odtwarzacz.

7. **Awatar**: GUI — Ustawienia → Awatar; TUI — `/awatar <ścieżka>`
   (`/awatar -` usuwa). Działa animowany GIF. Obrazek jest przycinany do
   kwadratu 256 px i **kodowany od nowa**, więc nic poza samą grafiką nie
   przechodzi dalej.

## Komendy TUI

`/pomoc` — pełna lista. Najważniejsze: `/nowy` `/dm` `/zapros` `/dolacz`
`/znajomy` `/mojkod` `/kto` `/kick` `/rekey` `/zmien` `/call` `/mic` `/cam`
`/ekran` `/rozlacz` `/wyslij` `/zapisz` `/awatar` `/quit`. Tab przełącza czaty.

## Windows

- connector: `make connector-windows` (czysty Go, zero zależności),
- klienty (cgo: audio + GUI): `sudo pacman -S mingw-w64-gcc && make windows`
  albo [fyne-cross](https://github.com/fyne-io/fyne-cross). Kamera na Windows
  wymaga `ffmpeg` w PATH (przechwytywanie dshow).

## Android

- wymagania: Android SDK (build-tools, platforma) + NDK + narzędzie fyne:
  `go install fyne.io/tools/cmd/fyne@latest`; ścieżki w Makefile
  (`ANDROID_HOME`, `ANDROID_NDK_HOME`) — domyślnie `~/Android/Sdk`,
- `make android` → `bin/szept.apk` (sideload: `adb install -r bin/szept.apk`),
- wersja: jedyne źródło to `cmd/szept-gui/FyneApp.toml` (`Version` + `Build`);
  `fyne package` podbija `Build` po każdym pakowaniu, a `make android` wpisuje
  `versionCode=Build` / `versionName=Version.Build` do manifestu — więc każdy
  build ma nową wersję (Ustawienia telefonu → Aplikacje → szept, oraz w
  aplikacji: zębatka → Ustawienia → Wersja),
- GUI to ten sam `cmd/szept-gui`, ale Android jest platformą pierwszą: dolny
  pasek **Czaty | Znajomi | Ty**, rozmowa i wszystkie okna (zaproszenie,
  członkowie, profil, ustawienia, załącznik) jako **pełnoekranowe strony**
  z paskiem tytułu i obsługą Wstecz; długie przytrzymanie dymka daje akcje
  wiadomości; dane w prywatnym katalogu aplikacji,
- załącznik: siatka miniatur z folderu multimediów (domyślnie `/sdcard/DCIM`,
  zmiana w Ty → Folder z multimediami) plus systemowy wybór zdjęć/filmów albo
  dowolnego pliku. Odczyt `/sdcard` wymaga uprawnienia **Pliki i multimedia** —
  aplikacja fyne nie umie o nie poprosić w czasie działania, więc włącza się je
  ręcznie (Ustawienia → Aplikacje → szept → Uprawnienia); systemowy wybór
  działa bez niego,
- mikrofon: uprawnienie jest w manifeście, ale bez promptu w aplikacji — jeśli
  głos nie działa, nadaj ręcznie: Ustawienia → Aplikacje → szept → Uprawnienia,
- kamera i udostępnianie ekranu na Androidzie nie działają (V4L2/X11 nie
  istnieją — przyciski zwrócą błąd); tekst + głos + odbiór wideo działają.

## Ograniczenia (świadome)

- obie strony muszą być online, żeby wiadomość dotarła (czysty P2P, brak
  skrzynki na serwerze); DAG dosynchronizuje wszystko przy następnym spotkaniu,
- brak TURN: przy „symetrycznych” NAT-ach po obu stronach połączenie może nie
  przejść (STUN wystarcza w większości sieci domowych),
- udostępnianie ekranu na Wayland wymaga XWayland,
- głos to surowe PCM 16 kHz (≈32 kB/s na strumień) — Opus to naturalny upgrade,
- wideo to MJPEG po niezawodnych-nie kanałach danych — proste i wszędzie działa,
- sufit jednego załącznika to 32 MB (blob leży w bazie jako jedna wartość);
  powyżej 10 MB rozchodzi się tylko podgląd, a miniatury nie ma dla filmów
  i archiwów — bez dekodera nie ma z czego jej zrobić,
- „ukryj u mnie” jest lokalne i tylko na jednym urządzeniu; drugie urządzenie
  z tą samą tożsamością pokaże wiadomość dalej.

Szczegóły bezpieczeństwa: [SECURITY.md](SECURITY.md).

## Licencja

[GNU General Public License v3.0](LICENSE) (GPL-3.0-or-later).
