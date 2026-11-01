BIN    := bin
PREFIX ?= $(HOME)/.local
APPID  := chat.szept

ANDROID_HOME     ?= $(HOME)/Android/Sdk
ANDROID_NDK_HOME ?= $(ANDROID_HOME)/ndk/27.2.12479018
FYNE             ?= $(HOME)/go/bin/fyne
ZIPALIGN         ?= $(lastword $(sort $(wildcard $(ANDROID_HOME)/build-tools/*/zipalign)))
AAPT2            ?= $(dir $(ZIPALIGN))aapt2
APKSIGNER        ?= $(dir $(ZIPALIGN))apksigner
ANDROID_JAR      ?= $(lastword $(sort $(wildcard $(ANDROID_HOME)/platforms/*/android.jar)))
KEYSTORE         ?= $(HOME)/.android/szept-debug.keystore
# Release APKs: pass your own keystore, e.g.
#   make android KEYSTORE=~/keys/szept-release.jks KS_PASS=... KS_ALIAS=szept
KS_PASS          ?= android
KS_ALIAS         ?= androiddebugkey
APKWORK          := $(BIN)/apkres
WINCC            ?= x86_64-w64-mingw32-gcc

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

# Binary name -> package. Every build rule below derives the package from the
# target it is building, so a new client costs one line here and one in BINS.
pkg-szept           := ./cmd/szept
pkg-szept-gui       := ./cmd/szept-gui
pkg-szept-wails     := ./cmd/szept-wails
pkg-szept-connector := ./cmd/connector

BINS    := szept szept-gui szept-wails szept-connector
NATIVE  := $(addprefix $(BIN)/,$(BINS))
WINDOWS := $(addprefix $(BIN)/,szept.exe szept-gui.exe szept-wails.exe)

# Every build starts from a clean slate: a failed `go build` must not leave the
# previous binary sitting in bin/ looking freshly built.
fresh = mkdir -p $(@D) && rm -f $@

# Build rules are .PHONY on purpose: they list no sources, so without this
# `make` after a code edit does nothing and keeps the old binary. Go's own
# build cache is what keeps repeated builds cheap.
.PHONY: all test clean windows connector-windows android install uninstall release \
	$(NATIVE) $(WINDOWS) $(BIN)/szept-connector.exe $(BIN)/szept.apk

all: $(NATIVE)

$(NATIVE): $(BIN)/%:
	@$(fresh)
	go build $(GOTAGS) -o $@ $(pkg-$*)

test:
	go vet $(GOTAGS) ./...
	go test $(GOTAGS) ./... -timeout 180s

# Clients use cgo (audio/GUI) — mingw-w64 is required:
#   Arch: sudo pacman -S mingw-w64-gcc
# The wails client renders through WebView2, which ships with Windows 11 and is
# a small runtime install on 10 — no GTK/webkit story there.
windows: $(WINDOWS)

$(WINDOWS): $(BIN)/%.exe:
	@$(fresh)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=$(WINCC) \
		go build -ldflags -H=windowsgui -o $@ $(pkg-$*)

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
REPRO    := -trimpath -buildvcs=false -ldflags="-s -w -buildid="
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

# APK: requires Android SDK+NDK and fyne tool (go install fyne.io/tools/cmd/fyne@latest).
# Permissions/manifest: cmd/szept-gui/AndroidManifest.xml
# checklinkname=0: wlynxg/anet (pion) links net.zoneCache, Go >=1.23 blocks this
# Version: single source of truth is FyneApp.toml (Version + Build). `fyne package`
# increments Build after every successful packaging, so every build = new version.
# Manifest has its own file, so fyne does NOT substitute it — we inject with sed:
# versionCode=Build (Android requires strictly increasing integer), versionName=Version.Build.
#
# fyne then compiles that manifest with its own binres, which sorts attributes
# alphabetically by name — Android looks them up with a binary search over
# resource IDs. With allowBackup in <application> the order broke and the system
# silently dropped label and icon: the app installed as "chat.szept" with the
# default robot icon. So the packaged APK gets its manifest and resource table
# replaced with ones built by aapt2, which orders attributes correctly and
# understands modern attributes (targetSdk 34, maxSdkVersion, fullBackupContent).
android: $(BIN)/szept.apk

$(BIN)/szept.apk: | $(KEYSTORE)
	@$(fresh)
	rm -rf cmd/szept-gui/szept.apk $(APKWORK)
	cd cmd/szept-gui && \
		V=$$(sed -n 's/^Version *= *"\(.*\)"/\1/p' FyneApp.toml) && \
		B=$$(sed -n 's/^Build *= *//p' FyneApp.toml) && \
		sed -i -e "s/android:versionCode=\"[^\"]*\"/android:versionCode=\"$$B\"/" \
		       -e "s/android:versionName=\"[^\"]*\"/android:versionName=\"$$V.$$B\"/" AndroidManifest.xml && \
		echo "szept $$V.$$B (versionCode $$B)" && \
		ANDROID_HOME=$(ANDROID_HOME) ANDROID_NDK_HOME=$(ANDROID_NDK_HOME) \
		GOFLAGS=-ldflags=-checklinkname=0 $(FYNE) package --os android
	# The launcher icon comes back out of fyne's own APK already scaled — no
	# second copy of Icon.png to keep in sync, and no image tooling in the build.
	mkdir -p $(APKWORK)/res/mipmap-xxxhdpi
	unzip -p cmd/szept-gui/szept.apk res/mipmap-xxxhdpi-v4/icon.png \
		> $(APKWORK)/res/mipmap-xxxhdpi/icon.png
	$(AAPT2) compile --dir $(APKWORK)/res -o $(APKWORK)/res.zip
	# android:icon lands here rather than in the source manifest: `fyne package`
	# refuses to build one that declares the icon by hand.
	sed -e 's|<application android:label="szept"|&\n\t\tandroid:icon="@mipmap/icon"|' \
		cmd/szept-gui/AndroidManifest.xml > $(APKWORK)/AndroidManifest.xml
	$(AAPT2) link -I $(ANDROID_JAR) --manifest $(APKWORK)/AndroidManifest.xml \
		--min-sdk-version 21 --target-sdk-version 34 \
		-o $(APKWORK)/res.apk $(APKWORK)/res.zip
	cd $(APKWORK) && unzip -oq res.apk AndroidManifest.xml resources.arsc 'res/*'
	zip -qd cmd/szept-gui/szept.apk AndroidManifest.xml resources.arsc 'res/*' 'META-INF/*'
	# -0 (stored): Android maps resources.arsc straight out of the file, so it has
	# to stay uncompressed for zipalign to be able to align it at all.
	cd $(APKWORK) && zip -qX0 $(CURDIR)/cmd/szept-gui/szept.apk \
		AndroidManifest.xml resources.arsc res/*/*
	# align first, sign second: apksigner's v2/v3 signatures cover the whole file,
	# so zipalign afterwards would invalidate them.
	$(ZIPALIGN) -p -f 4 cmd/szept-gui/szept.apk $@
	$(APKSIGNER) sign --ks $(KEYSTORE) --ks-pass pass:$(KS_PASS) --key-pass pass:$(KS_PASS) \
		--ks-key-alias $(KS_ALIAS) $@
	rm -rf cmd/szept-gui/szept.apk $@.idsig $(APKWORK)

# Rebuilding the manifest means re-signing, so the APK no longer carries fyne's
# built-in debug key. One local debug keystore, generated on first use — swapping
# keys means uninstalling the app on the phone first, so it must stay put.
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

# `go build ./cmd/szept` with no -o drops the binary in the repo root, so clean
# sweeps the strays too — otherwise a months-old copy sits next to the sources.
clean:
	rm -rf $(BIN)
	rm -f $(BINS) $(addsuffix .exe,$(BINS)) connector connector.exe \
		cmd/szept-gui/szept.apk
