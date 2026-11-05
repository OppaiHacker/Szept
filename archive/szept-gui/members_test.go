package main

import (
	"testing"

	"fyne.io/fyne/v2/test"

	"szept/internal/boot"
	"szept/internal/core"
	"szept/internal/store"
)

// Kolumna członków: podział online/offline i strażnik sygnatury (bez niego
// każdy event z sieci przerysowywałby awatary całej listy).
func TestRenderMembers(t *testing.T) {
	dir := t.TempDir()
	id, err := boot.CreateIdentity(dir, "ala", nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := core.Open(core.Config{DataDir: dir, Connector: "ws://127.0.0.1:9/"}, id)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	bob := newPerson("bob")
	if _, err := c.CreateDirect(store.Friend{Pub: bob.pub, Name: bob.name}); err != nil {
		t.Fatal(err)
	}

	mobileUI = false
	u := &ui{
		a: test.NewTempApp(t), w: test.NewWindow(nil), c: c,
		cfg: &core.Config{DataDir: dir}, decoded: map[string]any{},
	}
	defer u.w.Close()
	u.buildConv()

	ch := u.c.Chats()[0]
	u.renderMembers(&ch)
	// ONLINE + ja, OFFLINE + bob (peer bez połączenia)
	if n := len(u.memberBox.Objects); n != 4 {
		t.Fatalf("wierszy w kolumnie: %d, chciane 4", n)
	}
	first := u.memberBox.Objects[0]

	u.renderMembers(&ch)
	if u.memberBox.Objects[0] != first {
		t.Fatal("ta sama lista przebudowana mimo niezmienionej sygnatury")
	}
}
