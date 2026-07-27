package main

// Developer tool (to be removed): renders mobile layout using software
// fyne renderer and saves PNGs. Testing UI on APK requires full packaging
// cycle, and Xvfb gives no GLFW GL context — software canvas provides both
// for free.
//
//	SZEPT_SEED=/tmp/seed SZEPT_SHOTS=/tmp/shots go test ./cmd/szept-gui -run TestShots -v

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"szept/internal/boot"
	"szept/internal/core"
)

func TestShots(t *testing.T) {
	u, shot, done := shotSetup(t, true, fyne.NewSize(411, 820))
	defer done()

	shot("01-czaty")
	if len(u.rows) > 0 {
		u.sel = 0
		u.openConv()
		shot("02-rozmowa")
		// scrolled to middle of history: both edges have content behind them,
		// i.e., exact moment when container.Scroll rendered its shadows
		u.msgScr.ScrollToOffset(fyne.NewPos(0, 120))
		shot("02b-rozmowa-przewinieta")
		u.popPage()
	}
	if len(u.rows) > 1 {
		u.sel = 1
		u.openConv()
		shot("03-grupa")
		u.popPage()
	}
	u.selectTab(1)
	shot("04-znajomi")
	u.selectTab(2)
	shot("05-ty")

	// panel rozmowy + scena 3D (dołączenie bez peerów: brak urządzeń audio
	// tylko loguje błąd, stan Joined i tak się ustawia)
	if len(u.rows) > 0 {
		u.selectTab(0)
		u.sel = 0
		u.openConv()
		ch := u.cur()
		if err := u.c.JoinCall(ch.ID); err == nil {
			u.refresh()
			shot("06-panel-rozmowy")
			u.spatialPage()
			shot("07-scena-3d-pusta")
			// pionki innych: prawdziwe dołączenie wymaga drugiego procesu,
			// więc do zrzutu wstawiamy je wprost na scenę
			for i, n := range []string{"bob", "kasia-z-drugiej-klatki", "michał", "zosia"} {
				pub := []byte{byte(i + 7), byte(i)}
				tok := newToken(u.sp, pub, n, false)
				u.sp.arrangeOne(tok, i, 4)
				u.sp.gains[string(pub)] = 1
				u.sp.toks[string(pub)] = tok
			}
			u.sp.panelHint = mutedWrap(u.sp.hintText())
			u.sp.selectTok(nil)
			for _, tok := range u.sp.toks {
				u.sp.applyAudio(tok)
			}
			u.sp.Refresh()
			shot("07-scena-3d")
			for _, tok := range u.sp.toks {
				if tok.name == "michał" {
					u.sp.selectTok(tok)
				}
			}
			shot("08-scena-3d-wybrana-osoba")
			u.popPage()
			u.c.LeaveCall(ch.ID)
		}
	}
}

// Desktop shares all building blocks with mobile, so a layout tweak on mobile
// can break desktop layout — hence same screenshot in second variant.
func TestShotsDesktop(t *testing.T) {
	u, shot, done := shotSetup(t, false, fyne.NewSize(1060, 680))
	defer done()
	if len(u.rows) > 0 {
		u.sel = 0
		u.refresh()
	}
	shot("10-pulpit")

	if ch := u.cur(); ch != nil {
		if err := u.c.JoinCall(ch.ID); err == nil {
			u.refresh()
			shot("14-pulpit-panel-rozmowy")
			u.c.LeaveCall(ch.ID)
			u.refresh()
		}
	}

	u.confirm("Usunąć kasia-z-drugiej-klatki z czatu i zrotować klucz?", func() {})
	shot("11-potwierdzenie")
	u.w.Canvas().Overlays().Remove(u.w.Canvas().Overlays().Top())

	u.fail(errors.New("brak klucza epoki 2"))
	shot("12-blad")
	u.w.Canvas().Overlays().Remove(u.w.Canvas().Overlays().Top())

	u.page("Zaproszenie", container.NewVBox(
		widget.NewLabel("Kod jednorazowy · wygasa po 10 minutach."),
		bigButton("Kopiuj", theme.ContentCopyIcon(), nil)))
	shot("13-strona")
	u.w.Canvas().Overlays().Remove(u.w.Canvas().Overlays().Top())

	// każda strona ustawień choć raz narysowana — sam zrzut jest testem, bo
	// ujemny rozmiar w układzie wywraca painter
	u.openSettings()
	shot("19-ustawienia")
	for _, s := range []struct {
		name string
		open func()
	}{
		{"20-backup", u.backupPage},
		{"21-connectory", u.connectorsPage},
		{"22-glos", u.voicePage},
		{"23-hasla", u.passPage},
		{"24-konta", u.accountsPage},
		{"25-system", u.systemPage},
		{"26-wyglad", u.appearancePage},
		{"26b-status", u.statusPage},
		{"26c-baner", u.bannerPage},
		{"27-boty", u.botsPage},
		{"28-wkrotce", u.comingSoonPage("Pluginy", "Rozszerzenia pisane przez społeczność.")},
	} {
		s.open()
		shot(s.name)
	}
	u.closeSettings()

	// karta profilu (baner + status + opis) — pionowy układ łatwo psuje się
	// przy zmianach w nagłówku, więc zrzut zostaje jako test
	u.profilePage(u.c.ID.Pub, u.c.ID.Name, false, u.cur())
	shot("31-profil")
	u.w.Canvas().Overlays().Remove(u.w.Canvas().Overlays().Top())

	// ekrany startowe stoją poza normalnym przepływem aplikacji, więc ich
	// układ psuje się niezauważony — zrzut jest tu całym testem
	u.setupScreen()
	shot("29-pierwsze-uruchomienie")
	u.passScreen(u.cfg.DataDir)
	shot("30-logowanie")
}

func shotSetup(t *testing.T, mobile bool, size fyne.Size) (*ui, func(string), func()) {
	t.Helper()
	seed, out := os.Getenv("SZEPT_SEED"), os.Getenv("SZEPT_SHOTS")
	if seed == "" || out == "" {
		t.Skip("SZEPT_SEED / SZEPT_SHOTS nie ustawione")
	}
	if err := os.MkdirAll(out, 0o700); err != nil {
		t.Fatal(err)
	}
	if !boot.HasIdentity(seed) {
		buildSeed(t, seed)
	}
	mobileUI = mobile
	a := test.NewTempApp(t)
	a.Settings().SetTheme(szTheme{base: theme.DefaultTheme()})

	id, err := boot.LoadIdentity(seed, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := core.Open(core.Config{DataDir: seed, Connector: "ws://127.0.0.1:9/"}, id)
	if err != nil {
		t.Fatal(err)
	}
	w := test.NewWindow(nil)
	w.Resize(size)
	u := &ui{
		a: a, w: w, c: c, cfg: &core.Config{DataDir: seed}, mobile: mobile,
		vidImgs: map[string]*canvas.Image{}, decoded: map[string]any{},
		seen: map[string]int{}, unread: map[string]int{},
		prev: map[string]chatPrev{}, sys: map[string][]sysMsg{},
	}
	u.buildMain()

	shot := func(name string) {
		f, err := os.Create(filepath.Join(out, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, w.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
		t.Logf("zapisane %s", name)
	}
	return u, shot, func() { w.Close(); c.Close() }
}
