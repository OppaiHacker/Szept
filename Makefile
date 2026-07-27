BIN := bin
ANDROID_HOME ?= $(HOME)/Android/Sdk
ANDROID_NDK_HOME ?= $(ANDROID_HOME)/ndk/27.2.12479018
FYNE ?= $(HOME)/go/bin/fyne

PREFIX ?= $(HOME)/.local
APPID  := chat.szept

# Binaries are intentionally .PHONY: rules have no source file lists, so without this `make`
# after code edits does nothing and leaves the old binary. `go build` caches anyway.
.PHONY: all test clean linux windows connector-windows android install uninstall \
	$(BIN)/szept-connector $(BIN)/szept $(BIN)/szept-gui

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

# Connector is pure Go — cross-compilation with no dependencies:
connector-windows:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o $(BIN)/szept-connector.exe ./cmd/connector

# Clients use cgo (audio/GUI) — mingw-w64 is required:
#   Arch: sudo pacman -S mingw-w64-gcc
windows:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
		go build -ldflags -H=windowsgui -o $(BIN)/szept.exe ./cmd/szept
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
		go build -ldflags -H=windowsgui -o $(BIN)/szept-gui.exe ./cmd/szept-gui

# APK: requires Android SDK+NDK and fyne tool (go install fyne.io/tools/cmd/fyne@latest).
# Permissions/manifest: cmd/szept-gui/AndroidManifest.xml
# checklinkname=0: wlynxg/anet (pion) links net.zoneCache, Go >=1.23 blocks this
# Version: single source of truth is FyneApp.toml (Version + Build). `fyne package`
# increments Build after every successful packaging, so every build = new version.
# Manifest has its own file, so fyne does NOT substitute it — we inject with sed:
# versionCode=Build (Android requires strictly increasing integer), versionName=Version.Build.
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

# Installation to app menu (without root, XDG user dirs).
# Re-running `make install` = update: overwrites binary and .desktop entry.
# StartupWMClass must match app.NewWithID in main.go, otherwise launcher won't
# associate window with icon and displays it as a separate unnamed taskbar item.
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
