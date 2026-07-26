package main

// Narzędzie deweloperskie (do usunięcia): rysuje mobilny układ programowym
// rendererem fyne i zapisuje PNG-i. Sprawdzanie wyglądu na APK to pełny cykl
// pakowania, a Xvfb nie da glfw kontekstu GL — software canvas daje jedno i drugie
// za darmo.
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
		// przewinięte na środek historii: obie krawędzie mają treść za sobą,
		// czyli to moment, w którym container.Scroll rysował swoje cienie
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
}

// Pulpit dzieli z telefonem wszystkie klocki, więc poprawka wyglądu na telefonie
// umie zepsuć układ na pulpicie — stąd ten sam zrzut w drugim wariancie.
func TestShotsDesktop(t *testing.T) {
	u, shot, done := shotSetup(t, false, fyne.NewSize(1060, 680))
	defer done()
	if len(u.rows) > 0 {
		u.sel = 0
		u.refresh()
	}
	shot("10-pulpit")

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
