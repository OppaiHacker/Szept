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
		t.Fatal("wrong PIN gave membership")
	}
	if err := b.Redeem(code, pin); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "b sees chat after invite", 20*time.Second, func() bool {
		st, err := b.ChatState(chat)
		return err == nil && st.IsMember(b.ID.Pub) && st.Name == "tajna-grupa"
	})

	if err := a.Send(chat, "czesc bartek"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "message a->b", 10*time.Second, func() bool {
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
	waitFor(t, "message b->a", 10*time.Second, func() bool {
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
		t.Fatal("used code worked second time")
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
			t.Fatal("kicked member received new message")
		}
	}
}

// Attachment travels over separate channel: DAG gets only hash, content recipient
// fetches on demand and verifies. Avatar additionally undergoes re-encoding.
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
	waitFor(t, "b fetches attachment", 25*time.Second, func() bool {
		for _, m := range b.Messages(chat) {
			if m.Kind == "file" && m.Have {
				if m.File != "kot.png" || m.Mime != "image/png" || m.Text != "kot" {
					t.Fatalf("bad metadata: %+v", m)
				}
				got, ok := b.BlobBytes(m.Blob)
				if !ok || !bytes.Equal(got, pngData) {
					t.Fatal("attachment content does not match original")
				}
				return true
			}
		}
		return false
	})

	// normal file is allowed to be sent, but cannot pretend to be an image
	if err := a.SendFile(chat, "wirus.png", []byte("<html><script>x()</script>"), ""); err != nil {
		t.Fatalf("normal file rejected: %v", err)
	}
	for _, m := range a.Messages(chat) {
		if m.Kind == "file" && m.Size < 100 {
			if attach.IsMedia(m.Mime) || strings.HasSuffix(m.File, ".png") {
				t.Fatalf("HTML passed as image: %q %q", m.Mime, m.File)
			}
		}
	}

	if err := a.SetAvatar(testImageGIF(t)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "b sees ala's avatar", 25*time.Second, func() bool {
		data, mime, hash := b.AvatarOf(a.ID.Pub)
		if len(data) == 0 {
			return false
		}
		if len(hash) != 32 {
			t.Fatalf("avatar without hash: %x", hash)
		}
		if mime != "image/gif" {
			t.Fatalf("avatar arrived as %q", mime)
		}
		if _, err := gif.DecodeAll(bytes.NewReader(data)); err != nil {
			t.Fatalf("avatar is not a valid GIF: %v", err)
		}
		st, err := b.ChatState(chat)
		return err == nil && len(st.Members[string(a.ID.Pub)].Avatar) == 32
	})
}

// Large file (> PreviewMax) auto-replicates thumbnail only; full bytes travel
// only after FetchBlob. Simultaneously tests "hide locally" and name changes.
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
		t.Fatalf("test file has %d B — must exceed %d", len(big), attach.PreviewMax)
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
	// sender has everything immediately — doesn't fetch from self
	if m := find(a); m == nil || !m.Have || len(m.Prev) == 0 || !m.HavePrev {
		t.Fatalf("sender: %+v", m)
	}
	waitFor(t, "b has preview, but not full file", 30*time.Second, func() bool {
		m := find(b)
		return m != nil && m.HavePrev && !m.Have
	})
	m := find(b)
	if _, ok := b.BlobBytes(m.Blob); ok {
		t.Fatal("recipient fetched whole file without asking")
	}
	b.FetchBlob(chat, m.Blob, m.Mime, m.Size, m.Author)
	waitFor(t, "b fetches full file on demand", 60*time.Second, func() bool {
		got, ok := b.BlobBytes(m.Blob)
		return ok && bytes.Equal(got, big)
	})

	// soft delete: bytes remain in DAG, but become hidden locally
	before := len(b.Messages(chat))
	if err := b.HideMsg(chat, m.ID); err != nil {
		t.Fatal(err)
	}
	if after := len(b.Messages(chat)); after != before-1 {
		t.Fatalf("after hiding %d messages, before %d", after, before)
	}
	if !b.St.HasEvent(chat, m.ID) {
		t.Fatal("hiding deleted event from database")
	}
	if b.HiddenCount(chat) != 1 {
		t.Fatalf("hidden count = %d", b.HiddenCount(chat))
	}
	if err := b.UnhideAll(chat); err != nil {
		t.Fatal(err)
	}
	if after := len(b.Messages(chat)); after != before {
		t.Fatalf("after unhiding %d messages, wanted %d", after, before)
	}

	// new name arrives at the other peer
	if err := b.SetNick("Bartek Nowy"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a sees b's new name", 20*time.Second, func() bool {
		st, err := a.ChatState(chat)
		return err == nil && st.Members[string(b.ID.Pub)].Name == "Bartek Nowy"
	})

	// invites issued only by creator
	if _, _, err := b.NewInvite(chat); err == nil {
		t.Fatal("non-creator issued invite")
	}
	if _, _, err := a.NewInvite(chat); err != nil {
		t.Fatalf("creator cannot invite: %v", err)
	}
}

// testBigPNG: noise above attach.PreviewMax — PNG noise does not compress.
func testBigPNG(t *testing.T) []byte {
	t.Helper()
	const side = 2100 // 2100² * 3 B ≈ 13 MB after noise compression
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

// testImagePNG: noise so PNG does not compress — file must be larger
// than single transport chunk (blobChunk), otherwise test bypasses assembly.
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
	waitFor(t, "b receives DM", 20*time.Second, func() bool {
		for _, m := range b.Messages(chat) {
			if m.Text == "dm test" {
				return true
			}
		}
		return false
	})
}
