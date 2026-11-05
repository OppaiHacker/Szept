BIN    := bin
PREFIX ?= $(HOME)/.local
APPID  := chat.szept

ANDROID_HOME     ?= $(HOME)/Android/Sdk
ANDROID_NDK_HOME ?= $(ANDROID_HOME)/ndk/27.2.12479018
KEYSTORE         ?= $(HOME)/.android/szept-debug.keystore
# Release APKs: pass your own keystore, e.g.
#   make android KEYSTORE=~/keys/szept-release.jks KS_PASS=... KS_ALIAS=szept
KS_PASS          ?= android
KS_ALIAS         ?= androiddebugkey
WINCC            ?= x86_64-w64-mingw32-gcc
WINDRES          ?= x86_64-w64-mingw32-windres

# Wails v3 targets GTK4 + webkitgtk-6.0. Distros still shipping only GTK3 +
# webkit2gtk-4.1 need the gtk3 build tag, and without it every `go build ./...`
# in the module fails at pkg-config — hence the probe rather than a fixed value.
#   Arch:   sudo pacman -S webkit2gtk-4.1     (GTK3)  |  webkitgtk-6.0 (GTK4)
#   Debian: libwebkit2gtk-4.1-dev             (GTK3)  |  libwebkitgtk-6.0-dev
comma := ,
WAILS_TAGS ?= $(shell pkg-config --exists webkitgtk-6.0 2>/dev/null || echo gtk3)
# production: Wails' debug mode would force the webview inspector on; with the
# tag it opens only in developer mode (settings) or with -devtools.
GOTAGS     := -tags $(subst $(eval) ,$(comma),$(strip production $(WAILS_TAGS)))
# Release tag stamped into szept-wails; only stamped builds update themselves
# (internal/update). Defaults to the tag on HEAD, empty between releases.
VERSION    ?= $(shell git describe --tags --exact-match 2>/dev/null)
STAMP      := -X szept/internal/version.V=$(VERSION)

# Binary name -> package. Every build rule below derives the package from the
# target it is building, so a new client costs one line here and one in BINS.
pkg-szept-wails     := ./cmd/szept-wails
pkg-szept-connector := ./cmd/connector

BINS    := szept-wails szept-connector
NATIVE  := $(addprefix $(BIN)/,$(BINS))
WINDOWS := $(addprefix $(BIN)/,szept-wails.exe)

# Every build starts from a clean slate: a failed `go build` must not leave the
# previous binary sitting in bin/ looking freshly built.
fresh = mkdir -p $(@D) && rm -f $@

# Build rules are .PHONY on purpose: they list no sources, so without this
# `make` after a code edit does nothing and keeps the old binary. Go's own
# build cache is what keeps repeated builds cheap.
.PHONY: all test clean windows connector-windows android install uninstall release sign-release windows-installer \
	frontend $(NATIVE) $(WINDOWS) $(BIN)/szept-connector.exe $(BIN)/szept.apk

all: $(NATIVE)

# The client's UI: Vue + TypeScript, type-checked by vue-tsc and bundled by
# Vite into frontend/dist, which main.go embeds. Built with Bun (no Node.js
# needed: --bun runs vite and vue-tsc on Bun too). bun install runs only when
# node_modules is missing or older than the lock file.
FRONT := cmd/szept-wails/frontend
frontend:
	cd $(FRONT) && { [ node_modules/.bun-tag -nt bun.lock ] || { bun install --frozen-lockfile --no-summary && touch node_modules/.bun-tag; }; } && bun --bun run --silent build

$(BIN)/szept-wails $(WINDOWS) $(BIN)/szept.apk: frontend

$(NATIVE): $(BIN)/%:
	@$(fresh)
	go build $(GOTAGS) -ldflags "$(STAMP)" -o $@ $(pkg-$*)

test:
	go vet $(GOTAGS) ./...
	go test $(GOTAGS) ./... -timeout 180s

# Clients use cgo (audio/GUI) — mingw-w64 is required:
#   Arch: sudo pacman -S mingw-w64-gcc
# The wails client renders through WebView2, which ships with Windows 11 and is
# a small runtime install on 10 — no GTK/webkit story there.
windows: $(WINDOWS)

# Icon: a .syso next to main.go, which go build links into the .exe (Explorer,
# taskbar). 32512 is IDI_APPLICATION, the ID Wails loads as its window icon.
$(WINDOWS): $(BIN)/%.exe:
	@$(fresh)
	magick cmd/szept-wails/Icon.png -define icon:auto-resize=256,64,48,32,16 $(BIN)/szept.ico
	echo '32512 ICON "$(BIN)/szept.ico"' | $(WINDRES) -O coff -o $(pkg-$*)/rsrc_windows_amd64.syso
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=$(WINCC) \
		go build -tags production -ldflags "$(STAMP) -H=windowsgui" -o $@ $(pkg-$*)

# Installer: NSIS (Arch: yay -S nsis, Debian: apt install nsis), per-user, no
# admin. Bundles Microsoft's WebView2 bootstrapper for bare Windows 10 boxes;
# it runs only when the runtime is missing.
windows-installer: $(BIN)/szept-setup.exe

$(BIN)/szept-setup.exe: $(BIN)/szept-wails.exe
	@$(fresh)
	test -s $(BIN)/MicrosoftEdgeWebview2Setup.exe || curl -fsSL -o $(BIN)/MicrosoftEdgeWebview2Setup.exe \
		https://go.microsoft.com/fwlink/p/?LinkId=2124703
	makensis -V2 -DVERSION=$(or $(VERSION),dev) -DEXE=$(CURDIR)/$(BIN)/szept-wails.exe \
		-DICON=$(CURDIR)/$(BIN)/szept.ico -DWEBVIEW=$(CURDIR)/$(BIN)/MicrosoftEdgeWebview2Setup.exe \
		-DOUT=$(CURDIR)/$@ cmd/szept-wails/installer.nsi

# Connector is pure Go — cross-compilation with no dependencies:
connector-windows: $(BIN)/szept-connector.exe

$(BIN)/szept-connector.exe:
	@$(fresh)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o $@ ./cmd/connector

# Release: reproducible connector binaries for every platform people run one
# on (servers, Raspberry Pi, OpenWrt routers), plus SHA256SUMS signed with an
# SSH key. -trimpath, no VCS stamp and an empty build ID make the connector
# byte-identical for anyone building the same commit with the same Go version
# (written to GOVERSION, e.g. GOTOOLCHAIN=go1.27.3 make release), so a user
# can rebuild it and compare hashes. Clients use cgo, so
# theirs match only with the same C toolchain; build them and the APK
# separately and put them in $(REL) before signing.
#   make release SIGNKEY=~/.ssh/szept_release
# Verify (allowed_signers holds "szept <public key>"):
#   ssh-keygen -Y verify -f allowed_signers -I szept -n szept-release \
#     -s SHA256SUMS.sig < SHA256SUMS && sha256sum -c SHA256SUMS
REL      := $(BIN)/release
REPRO    = -trimpath -buildvcs=false -ldflags="-s -w -buildid= $(STAMP)"
PLATFORMS := linux/amd64 linux/arm64 linux/arm linux/mips linux/mipsle windows/amd64 darwin/arm64

release:
	rm -rf $(REL) && mkdir -p $(REL)
	for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=$$([ $$os = windows ] && echo .exe); \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build $(REPRO) \
			-o $(REL)/szept-connector-$$os-$$arch$$ext ./cmd/connector || exit 1; \
	done
	go version > $(REL)/GOVERSION
	cd $(REL) && sha256sum GOVERSION szept-* > SHA256SUMS
	$(if $(SIGNKEY),ssh-keygen -Y sign -f $(SIGNKEY) -n szept-release $(REL)/SHA256SUMS,@echo "SIGNKEY not set: SHA256SUMS left unsigned")

# Desktop clients update themselves only from releases whose SHA256SUMS is
# signed with this key (public half in internal/update). CI publishes the
# release unsigned; check it, then sign it from your machine:
#   make sign-release TAG=v0.2.0
sign-release: SIGNKEY ?= $(HOME)/.ssh/szept_release
sign-release:
	@test -n "$(TAG)" || { echo "TAG=vX.Y.Z required"; exit 1; }
	rm -rf $(BIN)/sign && mkdir -p $(BIN)/sign
	gh release download $(TAG) -p SHA256SUMS -D $(BIN)/sign
	ssh-keygen -Y sign -f $(SIGNKEY) -n szept-release $(BIN)/sign/SHA256SUMS
	gh release upload $(TAG) $(BIN)/sign/SHA256SUMS.sig --clobber

# APK: the same Wails client as the desktop, in an Android WebView. Go is built
# as libwails.so and loaded by the Gradle host in cmd/szept-wails/android
# (MainActivity, WailsBridge, SzeptHost). Needs the SDK + NDK and a JDK.
# The package id stays chat.szept and the data stays where the Fyne build kept
# it (platform_android.go), so this installs over the old app as an update,
# provided it is signed with the same key.
# checklinkname=0: wlynxg/anet (pion) links net.zoneCache, Go >=1.23 blocks this
# versionCode must only grow; 100 + commit count is above every Fyne build (<= 27).
NDK_CC   := $(ANDROID_NDK_HOME)/toolchains/llvm/prebuilt/linux-x86_64/bin/aarch64-linux-android26-clang
APK_CODE ?= $(shell echo $$((100 + $$(git rev-list --count HEAD))))
APKHOST  := cmd/szept-wails/android
android: $(BIN)/szept.apk

$(BIN)/szept.apk: | $(KEYSTORE)
	@$(fresh)
	CGO_ENABLED=1 GOOS=android GOARCH=arm64 CC=$(NDK_CC) \
		go build -buildmode=c-shared -tags production,android -trimpath -buildvcs=false \
		-ldflags "-s -w -checklinkname=0 $(STAMP)" \
		-o $(APKHOST)/app/src/main/jniLibs/arm64-v8a/libwails.so $(pkg-szept-wails)
	cd $(APKHOST) && ANDROID_HOME=$(ANDROID_HOME) \
		ANDROID_KEYSTORE_FILE=$(KEYSTORE) ANDROID_KEYSTORE_PASSWORD=$(KS_PASS) \
		ANDROID_KEY_ALIAS=$(KS_ALIAS) ANDROID_KEY_PASSWORD=$(KS_PASS) \
		./gradlew -q assembleRelease -PszeptVersionCode=$(APK_CODE) -PszeptVersionName=$(or $(VERSION),dev)
	cp $(APKHOST)/app/build/outputs/apk/release/app-release.apk $@
	@echo "szept $(or $(VERSION),dev) (versionCode $(APK_CODE)) -> $@"

# One local signing keystore, generated on first use. Android only accepts an
# update signed with the key the installed app was signed with, so it must stay
# put: a new key means uninstalling the app on the phone, and with it the data.
$(KEYSTORE):
	keytool -genkeypair -v -keystore $@ -storepass android -keypass android \
		-alias androiddebugkey -keyalg RSA -keysize 2048 -validity 10950 \
		-dname "CN=szept debug,O=szept,C=PL"

# Installation to app menu (without root, XDG user dirs).
# Re-running `make install` = update: overwrites binary and .desktop entry.
# StartupWMClass must match the WM class the toolkit reports (for wails on Linux
# that is the executable name), otherwise the launcher won't associate the
# window with the icon and shows it as a separate unnamed taskbar item.
install: $(BIN)/szept-wails
	install -Dm755 $(BIN)/szept-wails $(PREFIX)/bin/szept-wails
	install -Dm644 cmd/szept-wails/Icon.png \
		$(PREFIX)/share/icons/hicolor/512x512/apps/$(APPID).png
	install -d $(PREFIX)/share/applications
	printf '%s\n' \
		'[Desktop Entry]' \
		'Type=Application' \
		'Name=szept' \
		'Comment=Szyfrowany komunikator P2P' \
		'Exec=$(PREFIX)/bin/szept-wails' \
		'Icon=$(APPID)' \
		'Terminal=false' \
		'Categories=Network;InstantMessaging;' \
		'StartupWMClass=szept-wails' \
		> $(PREFIX)/share/applications/$(APPID).desktop
	-desktop-file-validate $(PREFIX)/share/applications/$(APPID).desktop
	-update-desktop-database $(PREFIX)/share/applications
	-gtk-update-icon-cache -qtf $(PREFIX)/share/icons/hicolor
	@echo "zainstalowane: $(PREFIX)/bin/szept-wails — szept jest w menu aplikacji"

uninstall:
	rm -f $(PREFIX)/bin/szept-wails $(PREFIX)/bin/szept-gui \
		$(PREFIX)/share/applications/$(APPID).desktop \
		$(PREFIX)/share/icons/hicolor/512x512/apps/$(APPID).png
	-update-desktop-database $(PREFIX)/share/applications
	-gtk-update-icon-cache -qtf $(PREFIX)/share/icons/hicolor

# `go build ./cmd/...` with no -o drops the binary in the repo root, so clean
# sweeps the strays too — otherwise a months-old copy sits next to the sources.
clean:
	rm -rf $(BIN)
	rm -f cmd/*/rsrc_windows_amd64.syso
	rm -f $(BINS) $(addsuffix .exe,$(BINS)) connector connector.exe \
		cmd/szept-gui/szept.apk
	find $(FRONT)/dist -mindepth 1 ! -name .gitkeep -delete
