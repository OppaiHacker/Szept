#!/usr/bin/env bash
# Zrzuty ekranu klienta wails do assets/shots/ — odtwarzalne.
#
#   tools/shots-wails.sh [katalog-wyjściowy]     # domyślnie assets/shots
#
# Wymaga: Xvfb, xdotool, imagemagick. Wszystko dzieje się na wirtualnym ekranie
# :99 — pulpit użytkownika nie jest dotykany i nie trafia na żaden zrzut.
#
# Pułapki, na które ten skrypt uważa:
#   * GDK_BACKEND=x11 + brak WAYLAND_DISPLAY — inaczej GTK woli sesję Waylanda
#     użytkownika i okno otwiera się na jego prawdziwym ekranie;
#   * GTK_THEME=Adwaita:dark — interfejs idzie za motywem pulpitu, a na gołym
#     Xvfb nie ma żadnego, więc bez tego zrzuty wychodzą jasne;
#   * brak menedżera okien = brak fokusu, więc przed pisaniem trzeba jawnie
#     zrobić `xdotool windowfocus` (kliknięcia idą pod kursor i działają zawsze);
#   * HOME/XDG_* wskazują na katalog roboczy, żeby konfiguracja i profile
#     z tego przebiegu nie mieszały się z prawdziwymi;
#   * kompozycja WebKitu zostaje włączona — z WEBKIT_DISABLE_COMPOSITING_MODE
#     okno przestaje się odrysowywać po pierwszym przełączeniu widoku;
#   * connector z -path off: domyślnie słucha pod losową, tajną ścieżką.
set -euo pipefail

cd "$(dirname "$0")/.."
OUT=$(realpath "${1:-assets/shots}")
WORK=$(mktemp -d /tmp/szept-shots.XXXXXX)
DISP=:99
SIZE=1180x760
CONN=ws://127.0.0.1:7799/

for t in Xvfb xdotool import magick; do
	command -v $t >/dev/null || { echo "brak narzędzia: $t" >&2; exit 1; }
done
mkdir -p "$OUT"

cleanup() {
	stop
	[ -n "${CONN_PID:-}" ] && kill $CONN_PID 2>/dev/null || true
	[ -n "${XVFB_PID:-}" ] && kill $XVFB_PID 2>/dev/null || true
	rm -rf "$WORK"
}
trap cleanup EXIT

export DISPLAY=$DISP

# ---- narzędzia sterujące ----

app() { # app <katalog-danych|-> [dodatkowe flagi…]
	local data=$1; shift
	local args=(-connector "$CONN")
	[ "$data" != "-" ] && args+=(-data "$data")
	# Własna sesja D-Bus: inaczej ta instancja przejmuje identyfikator aplikacji
	# GTK i późniejsze uruchomienie na pulpicie tylko aktywuje okno na Xvfb.
	# XDG_SESSION_TYPE=x11 trzyma udostępnianie ekranu z dala od portalu pulpitu.
	env -u WAYLAND_DISPLAY GDK_BACKEND=x11 DISPLAY=$DISP GTK_THEME=Adwaita:dark \
		XDG_SESSION_TYPE=x11 XDG_CURRENT_DESKTOP= \
		HOME="$HOMEDIR" XDG_CONFIG_HOME="$HOMEDIR/.config" XDG_DATA_HOME="$HOMEDIR/.local/share" \
		LIBGL_ALWAYS_SOFTWARE=1 \
		dbus-run-session -- ./bin/szept-wails "${args[@]}" "$@" >"$WORK/app.log" 2>&1 &
	APP_PID=$!
	sleep 7
	# Bez menedżera okien nic nie dostaje fokusu samo, a klient wystawia kilka
	# okien pomocniczych — to prawdziwe poznajemy po rozmiarze ekranu.
	local id
	for id in $(xdotool search --name '^szept-wails$'); do
		if xdotool getwindowgeometry "$id" | grep -q "Geometry: $SIZE"; then
			xdotool windowfocus "$id"
			return
		fi
	done
	echo "nie znalazłem okna aplikacji" >&2; exit 1
}

# Tylko nasza instancja — pkill -x zabiłby też prawdziwego klienta użytkownika.
stop() {
	[ -n "${APP_PID:-}" ] || return 0
	pkill -P "$APP_PID" 2>/dev/null || true
	APP_PID=; sleep 2
}
click() { xdotool mousemove "$1" "$2" click 1; sleep "${3:-1}"; }
park() { xdotool mousemove 1170 754; sleep .4; } # kursor poza wszystkim, bez podświetleń
shot() { park; import -window root "$OUT/$1.png"; echo "  $1"; }

# pick <x> <y> <ścieżka> — klik w (x,y) otwiera <input type=file>, potem wybór pliku w oknie GTK otwartym przez <input type=file>.
# Enter działa dopiero, gdy podpowiedzi ścieżki się doczytają, więc powtarzamy
# go aż okno zniknie — inaczej reszta klików leci w niewidzialny dialog.
pick() {
	# Pierwszy klik bywa połknięty, gdy portal na prywatnej sesji D-Bus dopiero
	# startuje, więc co 8 s klikamy jeszcze raz, aż pojawi się okno.
	local ch= i
	for i in $(seq 24); do
		[ $((i % 8)) = 1 ] && { xdotool mousemove "$1" "$2" click 1; park; }
		sleep 1
		ch=$(xdotool search --name '^Select File$' | head -1) && [ -n "$ch" ] && break
	done
	[ -n "$ch" ] || { echo "nie otworzył się wybór pliku" >&2; exit 1; }
	xdotool windowfocus "$ch"; sleep 1
	xdotool key ctrl+l; sleep 1
	xdotool type --delay 25 "$3"; sleep 2
	for _ in 1 2 3 4 5; do
		xdotool key Return; sleep 2
		xdotool search --name '^Select File$' | grep -q . || return 0
	done
	echo "wybór pliku nie chce się zamknąć" >&2; exit 1
}

# ---- przygotowanie ----

echo "== budowanie =="
make -s bin/szept-wails bin/szept-connector

echo "== dane demonstracyjne =="
SZEPT_SEED="$WORK/data" go test ./cmd/szept-wails -run TestSeed >/dev/null
magick -size 900x900 -define gradient:angle=135 gradient:'#1b3a4b'-'#3a9188' "$WORK/baner.png"

Xvfb $DISP -screen 0 ${SIZE}x24 -nolisten tcp >/dev/null 2>&1 & XVFB_PID=$!
./bin/szept-connector -listen 127.0.0.1:7799 -path off -mesh=false >"$WORK/conn.log" 2>&1 & CONN_PID=$!
sleep 2

# ---- przebieg 1: normalna praca (katalog danych podany z palca) ----

echo "== rozmowa, profil, ustawienia =="
HOMEDIR="$WORK/home1"; mkdir -p "$HOMEDIR"
app "$WORK/data"

click 36 132 3              # serwer ekipa-z-osiedla, kanał #czat
shot desktop-main

click 1013 106 2            # siebie na liście członków = karta profilu
shot profile-card
xdotool key Escape; sleep 1

click 148 212 4             # kanał głosowy Rozmowa
shot desktop-call
click 866 716 2             # rozłącz

click 280 726 2             # ustawienia (otwierają się na profilu)
shot settings-home

click 200 362 2; shot settings-connectors
click 200 434 2; shot settings-voice
click 200 324 2; shot settings-pass

click 200 548 2             # Boty
click 1037 231              # włącz webhook
click 474 457 2             # nowy token
shot settings-bots
click 1065 714 2            # "Zapisz zmiany" — pasek niezapisanych zmian blokuje nawigację

# hasło na tożsamości — potrzebne do zrzutu ekranu logowania w przebiegu 2
click 200 324 2             # Hasło i kopia
click 730 367; xdotool type --delay 25 "tajne-haslo"; sleep 1
click 476 424 3             # Zmień hasło

# kadrowanie na koniec: jedyny krok, który zależy od okna spoza aplikacji
click 200 248 2             # Profil
pick 700 230 "$WORK/baner.png"   # baner
shot crop-banner
click 695 617 1             # Anuluj (Escape zamknąłby całe ustawienia)
stop

# ---- przebieg 2: profile (bez -data) ----

echo "== konta i ekran logowania =="
HOMEDIR="$WORK/home2"
mkdir -p "$HOMEDIR/.config/szept" "$HOMEDIR/.local/share/szept/profiles"
cp -r "$WORK/data/." "$HOMEDIR/.local/share/szept/"
cp -r "$WORK/data" "$HOMEDIR/.local/share/szept/profiles/praca"
printf '{"connector":"%s"}\n' "$CONN" > "$HOMEDIR/.config/szept/config.json"

app -                       # tożsamość ma hasło, więc najpierw brama z listą kont
shot login-accounts
click 590 438               # pole hasła
xdotool type --delay 25 "tajne-haslo"; sleep 1
click 590 494 6             # Odblokuj

click 280 726 2             # ustawienia
click 200 286 2             # Konta
shot settings-accounts
stop

# ---- przebieg 3: pierwsze uruchomienie ----

echo "== pierwsze uruchomienie =="
HOMEDIR="$WORK/home3"; mkdir -p "$HOMEDIR"
app -
shot first-run
stop

echo "gotowe: $OUT"
