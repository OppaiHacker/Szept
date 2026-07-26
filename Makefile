BIN := bin
ANDROID_HOME ?= $(HOME)/Android/Sdk
ANDROID_NDK_HOME ?= $(ANDROID_HOME)/ndk/27.2.12479018
FYNE ?= $(HOME)/go/bin/fyne

PREFIX ?= $(HOME)/.local
APPID  := chat.szept

.PHONY: all test clean linux windows connector-windows android install uninstall

all: $(BIN)/szept-connector $(BIN)/szept $(BIN)/szept-gui

$(BIN)/szept-connector:
	go build -o $@ ./cmd/connector
$(BIN)/szept:
	go build -o $@ ./cmd/szept
$(BIN)/szept-gui:
	go build -o $@ ./cmd/szept-gui

test:
	go vet ./...
	go test ./... -timeout 180s

# Connector jest czysty Go — krzyżowa kompilacja bez zależności:
connector-windows:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o $(BIN)/szept-connector.exe ./cmd/connector

# Klienty używają cgo (audio/GUI) — potrzebny mingw-w64:
#   Arch: sudo pacman -S mingw-w64-gcc
windows:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
		go build -ldflags -H=windowsgui -o $(BIN)/szept.exe ./cmd/szept
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
		go build -ldflags -H=windowsgui -o $(BIN)/szept-gui.exe ./cmd/szept-gui

# APK: wymaga Android SDK+NDK i narzędzia fyne (go install fyne.io/tools/cmd/fyne@latest).
# Uprawnienia/manifest: cmd/szept-gui/AndroidManifest.xml
# checklinkname=0: wlynxg/anet (pion) linkuje net.zoneCache, Go >=1.23 to blokuje
# Wersja: jedyne źródło prawdy to FyneApp.toml (Version + Build). `fyne package`
# inkrementuje Build po każdym udanym pakowaniu, więc każdy build = nowa wersja.
# Manifest ma własny plik, więc fyne go NIE podmienia — wstrzykujemy sed-em:
# versionCode=Build (Android wymaga liczby rosnącej), versionName=Version.Build.
android:
	cd cmd/szept-gui && \
		V=$$(sed -n 's/^Version *= *"\(.*\)"/\1/p' FyneApp.toml) && \
		B=$$(sed -n 's/^Build *= *//p' FyneApp.toml) && \
		sed -i -e "s/android:versionCode=\"[^\"]*\"/android:versionCode=\"$$B\"/" \
		       -e "s/android:versionName=\"[^\"]*\"/android:versionName=\"$$V.$$B\"/" AndroidManifest.xml && \
		echo "szept $$V.$$B (versionCode $$B)" && \
		ANDROID_HOME=$(ANDROID_HOME) ANDROID_NDK_HOME=$(ANDROID_NDK_HOME) \
		GOFLAGS=-ldflags=-checklinkname=0 $(FYNE) package --os android
	mkdir -p $(BIN) && mv cmd/szept-gui/szept.apk $(BIN)/szept.apk

# Instalacja do menu aplikacji (bez roota, XDG user dirs).
# Ponowne `make install` = aktualizacja: nadpisuje binarkę i wpis .desktop.
# StartupWMClass musi być równe app.NewWithID w main.go, inaczej launcher nie
# sklei okna z ikoną i pokaże je jako osobny, bezimienny wpis na pasku.
install: $(BIN)/szept-gui
	install -Dm755 $(BIN)/szept-gui $(PREFIX)/bin/szept-gui
	install -Dm644 cmd/szept-gui/Icon.png \
		$(PREFIX)/share/icons/hicolor/512x512/apps/$(APPID).png
	install -d $(PREFIX)/share/applications
	printf '%s\n' \
		'[Desktop Entry]' \
		'Type=Application' \
		'Name=szept' \
		'Comment=Szyfrowany komunikator P2P' \
		'Exec=$(PREFIX)/bin/szept-gui' \
		'Icon=$(APPID)' \
		'Terminal=false' \
		'Categories=Network;InstantMessaging;' \
		'StartupWMClass=$(APPID)' \
		> $(PREFIX)/share/applications/$(APPID).desktop
	-desktop-file-validate $(PREFIX)/share/applications/$(APPID).desktop
	-update-desktop-database $(PREFIX)/share/applications
	-gtk-update-icon-cache -qtf $(PREFIX)/share/icons/hicolor
	@echo "zainstalowane: $(PREFIX)/bin/szept-gui — szept jest w menu aplikacji"

uninstall:
	rm -f $(PREFIX)/bin/szept-gui \
		$(PREFIX)/share/applications/$(APPID).desktop \
		$(PREFIX)/share/icons/hicolor/512x512/apps/$(APPID).png
	-update-desktop-database $(PREFIX)/share/applications
	-gtk-update-icon-cache -qtf $(PREFIX)/share/icons/hicolor

clean:
	rm -rf $(BIN)
