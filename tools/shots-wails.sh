#!/usr/bin/env bash
# Zrzuty ekranu klienta wails do assets/shots/ — odtwarzalne, jak TestShots dla
# klienta fyne (cmd/szept-gui/shot_dev_test.go, on robi zrzuty mobile-* i
# spatial-*, bo scena 3D żyje tylko w kliencie na Androida).
#
#   tools/shots-wails.sh [katalog-wyjściowy]     # domyślnie assets/shots
#
# Wymaga: Xvfb, xdotool, imagemagick. Wszystko dzieje się na wirtualnym ekranie
# :99 — pulpit użytkownika nie jest dotykany i nie trafia na żaden zrzut.
#
# Pułapki, na które ten skrypt uważa:
#   * GDK_BACKEND=x11 + brak WAYLAND_DISPLAY — inaczej GTK woli sesję Waylanda
#     użytkownika i okno otwiera się na jego prawdziwym ekranie;
#   * brak menedżera okien = brak fokusu, więc przed pisaniem trzeba jawnie
#     zrobić `xdotool windowfocus` (kliknięcia idą pod kursor i działają zawsze);
#   * HOME/XDG_* wskazują na katalog roboczy, żeby konfiguracja i profile
#     z tego przebiegu nie mieszały się z prawdziwymi.
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
	pkill -x szept-wails 2>/dev/null || true
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
	env -u WAYLAND_DISPLAY GDK_BACKEND=x11 DISPLAY=$DISP \
		HOME="$HOMEDIR" XDG_CONFIG_HOME="$HOMEDIR/.config" XDG_DATA_HOME="$HOMEDIR/.local/share" \
		WEBKIT_DISABLE_COMPOSITING_MODE=1 LIBGL_ALWAYS_SOFTWARE=1 \
		./bin/szept-wails "${args[@]}" "$@" >"$WORK/app.log" 2>&1 &
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

stop() { pkill -x szept-wails || true; sleep 2; }
click() { xdotool mousemove "$1" "$2" click 1; sleep "${3:-1}"; }
park() { xdotool mousemove 1170 754; sleep .4; } # kursor poza wszystkim, bez podświetleń
shot() { park; import -window root "$OUT/$1.png"; echo "  $1"; }

# pick <ścieżka> — wybór pliku w oknie GTK otwartym przez <input type=file>.
# Enter działa dopiero, gdy podpowiedzi ścieżki się doczytają, więc powtarzamy
# go aż okno zniknie — inaczej reszta klików leci w niewidzialny dialog.
pick() {
	sleep 2
	local ch; ch=$(xdotool search --name '^Select File$' | head -1)
	[ -n "$ch" ] || { echo "nie otworzył się wybór pliku" >&2; exit 1; }
	xdotool windowfocus "$ch"; sleep 1
	xdotool key ctrl+l; sleep 1
	xdotool type --delay 25 "$1"; sleep 2
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
SZEPT_SEED="$WORK/data" SZEPT_SHOTS="$WORK/junk" \
	go test ./cmd/szept-gui -run TestShotsDesktop >/dev/null
magick -size 900x900 gradient:'#6f4bd8'-'#c25bb0' "$WORK/baner.png"

Xvfb $DISP -screen 0 ${SIZE}x24 -nolisten tcp >/dev/null 2>&1 & XVFB_PID=$!
./bin/szept-connector -listen 127.0.0.1:7799 >"$WORK/conn.log" 2>&1 & CONN_PID=$!
sleep 2

# ---- przebieg 1: normalna praca (katalog danych podany z palca) ----

echo "== rozmowa, profil, ustawienia =="
HOMEDIR="$WORK/home1"; mkdir -p "$HOMEDIR"
app "$WORK/data"

click 34 71                 # zakładka rozmów
click 223 219 2             # czat z bobem
shot desktop-main

click 460 60 2              # nagłówek rozmowy = karta profilu
shot profile-card
xdotool key Escape; sleep 1

click 1112 67 3             # słuchawka
shot desktop-call
click 1135 133 2            # rozłącz

click 34 727 2              # ustawienia
shot settings-home

click 130 183 2; shot settings-connectors
click 130 245 2; shot settings-voice
click 130 488 2; shot settings-pass

click 130 549 2             # Boty
click 918 189               # włącz webhook
click 366 392 2             # nowy token
shot settings-bots

# hasło na tożsamości — potrzebne do zrzutu ekranu logowania w przebiegu 2
click 130 488 2             # Hasło i kopia
click 626 304; xdotool type --delay 25 "tajne-haslo"; sleep 1
click 366 355 3             # Zmień hasło

# kadrowanie na koniec: jedyny krok, który zależy od okna spoza aplikacji
click 130 122 2             # Profil
click 721 430               # Baner
pick "$WORK/baner.png"
shot crop-banner
click 710 609 1             # Anuluj (Escape zamknąłby całe ustawienia)
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
click 590 445               # pole hasła
xdotool type --delay 25 "tajne-haslo"; sleep 1
click 590 497 5             # Odblokuj

click 34 727 2              # ustawienia
click 130 427 2             # Konta
shot settings-accounts
stop

# ---- przebieg 3: pierwsze uruchomienie ----

echo "== pierwsze uruchomienie =="
HOMEDIR="$WORK/home3"; mkdir -p "$HOMEDIR"
app -
shot first-run
stop

echo "gotowe: $OUT"
