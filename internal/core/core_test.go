package core

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"math/rand"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"szept/internal/attach"
	"szept/internal/connector"
	"szept/internal/identity"
)

func waitFor(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timeout: %s", what)
}

func newCore(t *testing.T, name, url string) *Core {
	t.Helper()
	id := identity.Create()
	id.Name = name
	c, err := Open(Config{Connector: url, DataDir: t.TempDir()}, id)
	if err != nil {
		t.Fatal(err)
	}
	c.Start()
	t.Cleanup(c.Close)
	return c
}

func TestInviteSyncKick(t *testing.T) {
	srv := httptest.NewServer(connector.NewServer())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	a := newCore(t, "ala", url)
	b := newCore(t, "bartek", url)

	chat, err := a.CreateChat("tajna-grupa")
	if err != nil {
		t.Fatal(err)
	}
	code, pin, err := a.NewInvite(chat)
	if err != nil {
		t.Fatal(err)
	}
	// wrong PIN must not grant membership (and must not burn the invite)
	wrongPin := "000000"
	if pin == wrongPin {
		wrongPin = "000001"
	}
	evil := newCore(t, "ewa", url)
	if err := evil.Redeem(code, wrongPin); err != nil {
		t.Fatal(err) // redeem queues locally; the inviter rejects the PIN
	}
	time.Sleep(2 * time.Second)
	if st, err := evil.ChatState(chat); err == nil && st.IsMember(evil.ID.Pub) {
		t.Fatal("zły PIN dał członkostwo")
	}
	if err := b.Redeem(code, pin); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "b widzi czat po zaproszeniu", 20*time.Second, func() bool {
		st, err := b.ChatState(chat)
		return err == nil && st.IsMember(b.ID.Pub) && st.Name == "tajna-grupa"
	})

	if err := a.Send(chat, "czesc bartek"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "wiadomość a→b", 10*time.Second, func() bool {
		for _, m := range b.Messages(chat) {
			if m.Text == "czesc bartek" {
				return true
			}
		}
		return false
	})

	if err := b.Send(chat, "czesc ala"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "wiadomość b→a", 10*time.Second, func() bool {
		for _, m := range a.Messages(chat) {
			if m.Text == "czesc ala" {
				return true
			}
		}
		return false
	})

	// one-time: the same code must not work again
	c2 := newCore(t, "celina", url)
	if err := c2.Redeem(code, pin); err != nil {
		t.Fatal(err) // redeem itself queues; grant must never arrive
	}
	time.Sleep(2 * time.Second)
	if st, err := c2.ChatState(chat); err == nil && st.IsMember(c2.ID.Pub) {
		t.Fatal("zużyty kod zadziałał drugi raz")
	}

	// kick rotates the key
	stBefore, _ := a.ChatState(chat)
	if err := a.Kick(chat, b.ID.Pub); err != nil {
		t.Fatal(err)
	}
	stAfter, err := a.ChatState(chat)
	if err != nil {
		t.Fatal(err)
	}
	if stAfter.Epoch != stBefore.Epoch+1 || stAfter.IsMember(b.ID.Pub) {
		t.Fatalf("kick: epoch %d->%d member=%v", stBefore.Epoch, stAfter.Epoch, stAfter.IsMember(b.ID.Pub))
	}
	// b must not receive post-kick messages
	if err := a.Send(chat, "sekret bez bartka"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	for _, m := range b.Messages(chat) {
		if m.Text == "sekret bez bartka" {
			t.Fatal("wyrzucony członek dostał nową wiadomość")
		}
	}
}

// Załącznik jedzie osobnym kanałem: w DAG-u ląduje sam skrót, a treść odbiorca
// dociąga sam i sprawdza. Awatar dodatkowo przechodzi przez przekodowanie.
func TestAttachmentAndAvatarTransfer(t *testing.T) {
	srv := httptest.NewServer(connector.NewServer())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	a := newCore(t, "ala", url)
	b := newCore(t, "bartek", url)
	if _, err := a.AddFriend(b.FriendCode()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddFriend(a.FriendCode()); err != nil {
		t.Fatal(err)
	}
	fs, _ := a.St.Friends()
	chat, err := a.CreateDirect(fs[0])
	if err != nil {
		t.Fatal(err)
	}

	pngData := testImagePNG(t)
	if err := a.SendFile(chat, "../../ważne/kot.png", pngData, "kot"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "b pobiera załącznik", 25*time.Second, func() bool {
		for _, m := range b.Messages(chat) {
			if m.Kind == "file" && m.Have {
				if m.File != "kot.png" || m.Mime != "image/png" || m.Text != "kot" {
					t.Fatalf("zła metryczka: %+v", m)
				}
				got, ok := b.BlobBytes(m.Blob)
				if !ok || !bytes.Equal(got, pngData) {
					t.Fatal("treść załącznika nie zgadza się z oryginałem")
				}
				return true
			}
		}
		return false
	})

	// zwykły plik wolno wysłać, ale nie może udawać obrazu
	if err := a.SendFile(chat, "wirus.png", []byte("<html><script>x()</script>"), ""); err != nil {
		t.Fatalf("zwykły plik odrzucony: %v", err)
	}
	for _, m := range a.Messages(chat) {
		if m.Kind == "file" && m.Size < 100 {
			if attach.IsMedia(m.Mime) || strings.HasSuffix(m.File, ".png") {
				t.Fatalf("HTML przeszedł jako obraz: %q %q", m.Mime, m.File)
			}
		}
	}

	if err := a.SetAvatar(testImageGIF(t)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "b widzi awatar ali", 25*time.Second, func() bool {
		data, mime, hash := b.AvatarOf(a.ID.Pub)
		if len(data) == 0 {
			return false
		}
		if len(hash) != 32 {
			t.Fatalf("awatar bez skrótu: %x", hash)
		}
		if mime != "image/gif" {
			t.Fatalf("awatar przyszedł jako %q", mime)
		}
		if _, err := gif.DecodeAll(bytes.NewReader(data)); err != nil {
			t.Fatalf("awatar nie jest poprawnym GIF-em: %v", err)
		}
		st, err := b.ChatState(chat)
		return err == nil && len(st.Members[string(a.ID.Pub)].Avatar) == 32
	})
}

// Duży plik (> PreviewMax) rozchodzi się jako sam podgląd; pełne bajty jadą
// dopiero po FetchBlob. Zarazem sprawdzamy „ukryj u mnie” i zmianę nazwy.
func TestBigFilePreviewAndHide(t *testing.T) {
	srv := httptest.NewServer(connector.NewServer())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	a := newCore(t, "ala", url)
	b := newCore(t, "bartek", url)
	if _, err := a.AddFriend(b.FriendCode()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddFriend(a.FriendCode()); err != nil {
		t.Fatal(err)
	}
	fs, _ := a.St.Friends()
	chat, err := a.CreateDirect(fs[0])
	if err != nil {
		t.Fatal(err)
	}

	big := testBigPNG(t)
	if int64(len(big)) <= attach.PreviewMax {
		t.Fatalf("plik testowy ma %d B — musi przekroczyć %d", len(big), attach.PreviewMax)
	}
	if err := a.SendFile(chat, "wielkie.png", big, "podgląd"); err != nil {
		t.Fatal(err)
	}

	find := func(c *Core) *Msg {
		for _, m := range c.Messages(chat) {
			if m.Kind == "file" {
				return &m
			}
		}
		return nil
	}
	// nadawca ma wszystko od razu — nie pobiera niczego od siebie
	if m := find(a); m == nil || !m.Have || len(m.Prev) == 0 || !m.HavePrev {
		t.Fatalf("nadawca: %+v", m)
	}
	waitFor(t, "b ma podgląd, ale nie pełny plik", 30*time.Second, func() bool {
		m := find(b)
		return m != nil && m.HavePrev && !m.Have
	})
	m := find(b)
	if _, ok := b.BlobBytes(m.Blob); ok {
		t.Fatal("odbiorca ściągnął cały plik bez pytania")
	}
	b.FetchBlob(chat, m.Blob, m.Mime, m.Size, m.Author)
	waitFor(t, "b pobiera pełny plik na żądanie", 60*time.Second, func() bool {
		got, ok := b.BlobBytes(m.Blob)
		return ok && bytes.Equal(got, big)
	})

	// soft delete: bajty zostają w DAG-u, ale u nas przestają być widoczne
	before := len(b.Messages(chat))
	if err := b.HideMsg(chat, m.ID); err != nil {
		t.Fatal(err)
	}
	if after := len(b.Messages(chat)); after != before-1 {
		t.Fatalf("po ukryciu %d wiadomości, przed %d", after, before)
	}
	if !b.St.HasEvent(chat, m.ID) {
		t.Fatal("ukrycie skasowało zdarzenie z bazy")
	}
	if b.HiddenCount(chat) != 1 {
		t.Fatalf("licznik ukrytych = %d", b.HiddenCount(chat))
	}
	if err := b.UnhideAll(chat); err != nil {
		t.Fatal(err)
	}
	if after := len(b.Messages(chat)); after != before {
		t.Fatalf("po przywróceniu %d wiadomości, chciałem %d", after, before)
	}

	// nowa nazwa dojeżdża do drugiej strony
	if err := b.SetNick("Bartek Nowy"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a widzi nową nazwę b", 20*time.Second, func() bool {
		st, err := a.ChatState(chat)
		return err == nil && st.Members[string(b.ID.Pub)].Name == "Bartek Nowy"
	})

	// zaproszenia wystawia tylko założyciel
	if _, _, err := b.NewInvite(chat); err == nil {
		t.Fatal("nie-założyciel wystawił zaproszenie")
	}
	if _, _, err := a.NewInvite(chat); err != nil {
		t.Fatalf("założyciel nie może zaprosić: %v", err)
	}
}

// testBigPNG: szum ponad attach.PreviewMax — PNG szumu się nie kompresuje.
func testBigPNG(t *testing.T) []byte {
	t.Helper()
	const side = 2100 // 2100² * 3 B ≈ 13 MB po kompresji szumu
	rnd := rand.New(rand.NewSource(11))
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	buf := make([]byte, side*side*4)
	rnd.Read(buf)
	for i := 3; i < len(buf); i += 4 {
		buf[i] = 0xff
	}
	img.Pix = buf
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// testImagePNG: szum, żeby PNG się nie skompresował — plik musi być większy
// od jednego kawałka transportu (blobChunk), inaczej test omija składanie.
func testImagePNG(t *testing.T) []byte {
	t.Helper()
	rnd := rand.New(rand.NewSource(7))
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			img.Set(x, y, color.RGBA{uint8(rnd.Intn(256)), uint8(rnd.Intn(256)), uint8(rnd.Intn(256)), 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testImageGIF(t *testing.T) []byte {
	t.Helper()
	g := &gif.GIF{}
	for i := 0; i < 4; i++ {
		p := image.NewPaletted(image.Rect(0, 0, 24, 24), color.Palette{color.Black, color.White})
		p.SetColorIndex(i, i, 1)
		g.Image = append(g.Image, p)
		g.Delay = append(g.Delay, 8)
		g.Disposal = append(g.Disposal, gif.DisposalNone)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDirectChatViaFriendCode(t *testing.T) {
	srv := httptest.NewServer(connector.NewServer())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	a := newCore(t, "ala", url)
	b := newCore(t, "bartek", url)

	// mutual friend add (codes carry pubkeys -> no TOFU)
	if _, err := a.AddFriend(b.FriendCode()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddFriend(a.FriendCode()); err != nil {
		t.Fatal(err)
	}
	fs, _ := a.St.Friends()
	chat, err := a.CreateDirect(fs[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Send(chat, "dm test"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "b dostaje DM", 20*time.Second, func() bool {
		for _, m := range b.Messages(chat) {
			if m.Text == "dm test" {
				return true
			}
		}
		return false
	})
}
