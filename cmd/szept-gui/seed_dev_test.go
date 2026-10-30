package main

// Developer tool (to be removed together with shot_dev_test.go): builds
// data directory with two chats and messages from several peers for
// screenshot generation. Peer events are inserted directly into store —
// real sync would require connector and second process.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"szept/internal/boot"
	"szept/internal/core"
	"szept/internal/event"
	"szept/internal/identity"
	"szept/internal/store"
)

type seedPerson struct {
	name string
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

func newPerson(name string) seedPerson {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	return seedPerson{name, pub, priv}
}

// seedLine: single message to insert — who, text, and minutes ago.
type seedLine struct {
	who  *seedPerson // nil = me
	text string
	ago  time.Duration
}

func buildSeed(t *testing.T, dir string) {
	t.Helper()
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
	kasia := newPerson("kasia-z-drugiej-klatki")
	for _, p := range []seedPerson{bob, kasia} {
		if err := c.St.PutFriend(store.Friend{Pub: p.pub, Name: p.name}); err != nil {
			t.Fatal(err)
		}
	}

	dm, err := c.CreateDirect(store.Friend{Pub: bob.pub, Name: bob.name})
	if err != nil {
		t.Fatal(err)
	}
	seedMsgs(t, c, id, dm, []seedLine{
		{&bob, "siema", 50 * time.Hour},
		{nil, "no siema, co tam", 49 * time.Hour},
		{&bob, "nic, siedzę i patrzę jak ten komunikator się buduje", 48 * time.Hour},
		{nil, "ok", 90 * time.Minute},
		{&bob, "Długa wiadomość, żeby było widać zawijanie w dymku: kiedy tekst " +
			"nie mieści się w jednej linii, dymek ma zająć rozsądną szerokość, " +
			"a nie cały ekran od krawędzi do krawędzi.", 80 * time.Minute},
		{nil, "zgadza się\ni jeszcze druga linia", 70 * time.Minute},
		{nil, "tak", 69 * time.Minute},
		{&bob, "👍", 5 * time.Minute},
	})

	// karta profilu: status, opis i baner — żeby zrzuty pokazywały je wypełnione
	if err := c.SetStatus("gram, wrócę wieczorem"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetBio("Piszę szepta po godzinach.\nNajchętniej offline, ale kod sam się nie napisze."); err != nil {
		t.Fatal(err)
	}
	if err := c.SetBanner(seedBanner(t), image.Rectangle{}); err != nil {
		t.Fatal(err)
	}
	putEvent(t, c, dm, bob.priv, event.KindProfile,
		event.Body{Text: "na rowerze", Bio: "bob, sąsiad z 3 piętra", TS: time.Now().Unix()})

	grp, err := c.CreateChat("ekipa-z-osiedla", false)
	if err != nil {
		t.Fatal(err)
	}
	addMember(t, c, id, grp, bob)
	addMember(t, c, id, grp, kasia)
	seedMsgs(t, c, id, grp, []seedLine{
		{&kasia, "gramy wieczorem?", 26 * time.Hour},
		{&bob, "ja mogę po 20", 25 * time.Hour},
		{&bob, "albo wcześniej, zobaczę", 25*time.Hour - time.Minute},
		{nil, "wchodzę w to", 24 * time.Hour},
		{&kasia, "to do zobaczenia", 30 * time.Minute},
		{nil, "spoko", 2 * time.Minute},
	})
}

// seedBanner: pasek gradientu — realny obrazek, bez pliku na dysku.
func seedBanner(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 720, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 720; x++ {
			img.Set(x, y, color.RGBA{uint8(60 + x/8), uint8(40 + y/3), 0xb0, 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func addMember(t *testing.T, c *core.Core, id *identity.Identity, chat []byte, p seedPerson) {
	t.Helper()
	putEvent(t, c, chat, id.Priv, event.KindAdd,
		event.Body{Pub: p.pub, Name: p.name, TS: time.Now().Add(-72 * time.Hour).Unix()})
}

func seedMsgs(t *testing.T, c *core.Core, id *identity.Identity, chat []byte, lines []seedLine) {
	t.Helper()
	for _, l := range lines {
		priv := id.Priv
		if l.who != nil {
			priv = l.who.priv
		}
		putEvent(t, c, chat, priv, event.KindMsg,
			event.Body{Text: l.text, TS: time.Now().Add(-l.ago).Unix()})
	}
}

func putEvent(t *testing.T, c *core.Core, chat []byte, priv ed25519.PrivateKey,
	kind string, body event.Body) {
	t.Helper()
	keys, err := c.St.ChatKeys(chat)
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.ChatState(chat)
	if err != nil {
		t.Fatal(err)
	}
	e, err := event.New(priv, chat, kind, body,
		c.St.NextLamport(chat), c.St.Heads(chat), st.Epoch, keys[st.Epoch])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.St.AddEvent(chat, e); err != nil {
		t.Fatal(err)
	}
}
