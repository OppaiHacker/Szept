package main

import (
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/test"

	"szept/internal/boot"
	"szept/internal/core"
)

func TestProfileDir(t *testing.T) {
	u := &ui{cfg: &core.Config{}, baseDir: "/base"}
	if got := u.profileDir(); got != boot.ProfileDir("") {
		t.Fatalf("domyślne konto: %s", got)
	}
	u.cfg.Profile = "praca"
	if got := u.profileDir(); got != boot.ProfileDir("praca") {
		t.Fatalf("konto praca: %s", got)
	}
	// -data i Android biją wybór konta
	u.dataFlag, u.baseDir = "/custom", "/custom"
	if got := u.profileDir(); got != "/custom" {
		t.Fatalf("-data: %s", got)
	}
	u.dataFlag, u.baseDir = "", "/sandbox"
	u.mobile = true
	if got := u.profileDir(); got != "/sandbox" {
		t.Fatalf("mobile: %s", got)
	}
}

// Na ekranie logowania przełączenie konta nie restartuje aplikacji, tylko
// przechodzi login() jeszcze raz — i musi zapamiętać wybór w config.json.
func TestSwitchProfileAtLogin(t *testing.T) {
	dir := t.TempDir()
	u := &ui{
		a: test.NewTempApp(t), w: test.NewWindow(nil),
		cfg: &core.Config{}, cfgFile: filepath.Join(dir, "config.json"),
		baseDir: dir,
	}
	defer u.w.Close()

	u.switchProfile("praca")

	if u.cfg.DataDir != boot.ProfileDir("praca") {
		t.Fatalf("login() nie przestawił katalogu danych: %s", u.cfg.DataDir)
	}
	saved, err := boot.LoadConfig(u.cfgFile)
	if err != nil || saved.Profile != "praca" {
		t.Fatalf("config nie pamięta konta: %v %+v", err, saved)
	}
	if u.w.Content() == nil {
		t.Fatal("brak ekranu startowego po przełączeniu")
	}
}
