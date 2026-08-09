package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"szept/internal/core"
	"szept/internal/identity"
)

func walk(o fyne.CanvasObject, f func(fyne.CanvasObject)) {
	f(o)
	if c, ok := o.(*fyne.Container); ok {
		for _, x := range c.Objects {
			walk(x, f)
		}
		return
	}
	if w, ok := o.(fyne.Widget); ok {
		for _, x := range test.WidgetRenderer(w).Objects() {
			walk(x, f)
		}
	}
}

// TestMobileSetupFlow: pusta piaskownica -> ekran pierwszego uruchomienia ->
// wypełnienie formularza -> "Zaczynamy" musi utworzyć tożsamość i wejść dalej.
func TestMobileSetupFlow(t *testing.T) {
	mobileUI = true
	a := test.NewTempApp(t)
	a.Settings().SetTheme(szTheme{base: theme.DefaultTheme()})
	dir := t.TempDir()
	w := test.NewWindow(nil)
	w.Resize(fyne.NewSize(411, 820))
	defer w.Close()

	u := &ui{a: a, w: w, cfg: &core.Config{DataDir: dir}, mobile: true, baseDir: dir}
	entered := false
	u.enter = func(id *identity.Identity) { entered = true }

	u.login()

	var entries []*widget.Entry
	var btn *widget.Button
	walk(w.Content(), func(o fyne.CanvasObject) {
		switch v := o.(type) {
		case *widget.Entry:
			entries = append(entries, v)
		case *widget.Button:
			if v.Text == "Zaczynamy" {
				btn = v
			}
		}
	})
	t.Logf("entries=%d btn=%v", len(entries), btn != nil)
	if len(entries) < 3 || btn == nil {
		t.Fatalf("ekran setupu niekompletny: entries=%d btn=%v", len(entries), btn != nil)
	}
	entries[0].SetText("ala")
	entries[1].SetText("ws://1.2.3.4:7777")

	btn.OnTapped()
	if !entered {
		var texts []string
		for _, ov := range w.Canvas().Overlays().List() {
			walk(ov, func(o fyne.CanvasObject) {
				if l, ok := o.(*widget.Label); ok {
					texts = append(texts, l.Text)
				}
			})
		}
		t.Fatalf("nie weszliśmy do aplikacji; dialogi: %q", texts)
	}
}
