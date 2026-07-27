package core

import (
	"strings"
	"testing"

	"szept/internal/event"
	"szept/internal/identity"
)

// Karta profilu (status, opis, baner) jedzie w podpisanej historii czatu, więc
// musi wylądować i w indeksie, i w zwiniętym stanie członka.
func TestProfileCard(t *testing.T) {
	c := newCore(t, "ala", "ws://127.0.0.1:9/")
	chat, err := c.CreateChat("ekipa")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetStatus("gram, wrócę wieczorem"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetBio("dwie linie\no mnie"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetBanner(testImagePNG(t)); err != nil {
		t.Fatal(err)
	}

	status, bio := c.StatusOf(c.ID.Pub)
	if status != "gram, wrócę wieczorem" || bio != "dwie linie\no mnie" {
		t.Fatalf("indeks: %q %q", status, bio)
	}
	if data, mime, hash := c.BannerOf(c.ID.Pub); len(data) == 0 || mime != "image/png" || len(hash) != 32 {
		t.Fatalf("baner: %d B %s %x", len(data), mime, hash)
	}

	st, err := c.ChatState(chat)
	if err != nil {
		t.Fatal(err)
	}
	me := st.Members[string(c.ID.Pub)]
	if me.Status != status || me.Bio != bio || len(me.Banner) != 32 {
		t.Fatalf("stan czatu: %+v", me)
	}

	// karta z sieci: indeksowana bez foldowania DAG, ale dopiero po czyszczeniu
	peer := identity.Create()
	keys, err := c.St.ChatKeys(chat)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := event.New(peer.Priv, chat, event.KindProfile, event.Body{
		Text: "na\x1browerze", Bio: strings.Repeat("y", MaxBio+10),
	}, 9, nil, st.Epoch, keys[st.Epoch])
	if err != nil {
		t.Fatal(err)
	}
	c.noteAttachment(chat, ev)
	ps, pb := c.StatusOf(peer.Pub)
	if ps != "narowerze" {
		t.Fatalf("status z sieci nieoczyszczony: %q", ps)
	}
	if len(pb) != MaxBio {
		t.Fatalf("opis z sieci %d znaków, limit %d", len(pb), MaxBio)
	}

	// pusty status kasuje linijkę, baner zostaje
	if err := c.SetStatus(""); err != nil {
		t.Fatal(err)
	}
	if s, _ := c.StatusOf(c.ID.Pub); s != "" {
		t.Fatalf("status nie skasowany: %q", s)
	}
	if data, _, _ := c.BannerOf(c.ID.Pub); len(data) == 0 {
		t.Fatal("baner zniknął razem ze statusem")
	}
}

// Opis przychodzi z sieci — sekwencje sterujące i przepełnienie muszą odpaść
// zanim trafią do bazy i na ekran.
func TestSafeBio(t *testing.T) {
	// ESC wypada, reszta zostaje zwykłym tekstem; nowa linia przeżywa
	if got := safeBio("a\x1b[2Jb\nc"); got != "a[2Jb\nc" {
		t.Fatalf("znak sterujący przeszedł: %q", got)
	}
	if got := safeBio(strings.Repeat("x", MaxBio+50)); len(got) != MaxBio {
		t.Fatalf("długość %d, limit %d", len(got), MaxBio)
	}
}
