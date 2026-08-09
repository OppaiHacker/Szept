package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
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
	"szept/internal/crypt"
	"szept/internal/identity"
	"szept/internal/p2p"
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

	if err := a.SetAvatar(testImageGIF(t), image.Rectangle{}); err != nil {
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

// Name history comes from the signed DAG: genesis + every KindNick, collapsed
// so one rename announced in several chats is a single entry.
func TestNickHistory(t *testing.T) {
	srv := httptest.NewServer(connector.NewServer())
	defer srv.Close()
	a := newCore(t, "ala", "ws"+strings.TrimPrefix(srv.URL, "http"))

	if _, err := a.CreateChat("grupa"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateChat("druga-grupa"); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"Ala K.", "Alicja"} {
		if err := a.SetNick(n); err != nil {
			t.Fatal(err)
		}
	}
	got := a.NickHistory(a.ID.Pub)
	want := []string{"ala", "Ala K.", "Alicja"}
	if len(got) != len(want) {
		t.Fatalf("historia = %+v, chciano %v", got, want)
	}
	for i, w := range want {
		if got[i].Name != w {
			t.Fatalf("historia[%d] = %q, chciano %q (całość %+v)", i, got[i].Name, w, got)
		}
	}
	// friend code carries no name any more
	if strings.Contains(string(mustDecodeCode(t, a.FriendCode())), "Alicja") {
		t.Fatal("kod znajomego niesie nazwę")
	}
}

func mustDecodeCode(t *testing.T, code string) []byte {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, "SZF-"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
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

// An epoch key is only accepted when the signed DAG vouches for it.
// Pre-filling a future epoch used to lock the real key out forever, and the
// first membership event landing in that epoch killed the chat outright.
func TestEpochKeyBoundToRekey(t *testing.T) {
	srv := httptest.NewServer(connector.NewServer())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	c := newCore(t, "ala", url)
	chat, err := c.CreateChat("grupa")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Rekey(chat); err != nil {
		t.Fatal(err)
	}
	st, err := c.ChatState(chat)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := c.St.ChatKeys(chat)
	if err != nil {
		t.Fatal(err)
	}
	real2 := keys[2]
	if len(real2) != 32 || len(st.KeyHash[2]) == 0 {
		t.Fatalf("rekey nie ogłosił skrótu klucza epoki 2 (klucz %d B, skrót %d B)", len(real2), len(st.KeyHash[2]))
	}
	if !c.keyMatchesDAG(st, 2, real2) {
		t.Fatal("prawdziwy klucz epoki odrzucony")
	}
	if c.keyMatchesDAG(st, 2, crypt.NewKey()) {
		t.Fatal("podrobiony klucz epoki przyjęty mimo niezgodnego skrótu")
	}
	// an epoch nobody announced is refused outright — that is the pre-poisoning path
	if c.keyMatchesDAG(st, 9, crypt.NewKey()) {
		t.Fatal("klucz dla nieogłoszonej epoki przyjęty")
	}
	// legacy chats have no hash: the fallback still refuses future epochs
	st.KeyHash = nil
	if c.keyMatchesDAG(st, st.Epoch+1, crypt.NewKey()) {
		t.Fatal("stara ścieżka zgodności przyjęła klucz z przyszłości")
	}
	if !c.keyMatchesDAG(st, 1, keys[1]) {
		t.Fatal("stara ścieżka zgodności odrzuciła klucz ogłoszonej epoki")
	}
}

// bBlobs is global, so membership in one chat must not be a pass to
// attachments announced in another.
func TestServeBlobCrossChat(t *testing.T) {
	srv := httptest.NewServer(connector.NewServer())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	c := newCore(t, "ala", url)
	secret, err := c.CreateChat("tajny")
	if err != nil {
		t.Fatal(err)
	}
	shared, err := c.CreateChat("wspolny")
	if err != nil {
		t.Fatal(err)
	}
	img := testImagePNG(t)
	if err := c.SendFile(secret, "tajne.png", img, ""); err != nil {
		t.Fatal(err)
	}
	hash, err := c.St.PutBlob(img)
	if err != nil {
		t.Fatal(err)
	}
	if !c.St.BlobInChat(secret, hash) {
		t.Fatal("załącznik nie trafił do indeksu swojego czatu")
	}
	if c.St.BlobInChat(shared, hash) {
		t.Fatal("hash z tajnego czatu wygląda na ogłoszony we wspólnym — IDOR nadal otwarty")
	}
	// deleting the chat takes its blobs with it
	if err := c.St.DeleteChat(secret); err != nil {
		t.Fatal(err)
	}
	if c.St.HasBlob(hash) {
		t.Fatal("załącznik przeżył usunięcie czatu")
	}
}

// Chunks are only accepted from a member of the chat the download
// belongs to, and a packet with a contradictory Total is dropped instead of
// wiping the buffer that is already half full.
func TestBlobPartFromNonMember(t *testing.T) {
	srv := httptest.NewServer(connector.NewServer())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	c := newCore(t, "ala", url)
	chat, err := c.CreateChat("grupa")
	if err != nil {
		t.Fatal(err)
	}
	data := testImagePNG(t)
	hash := sha256.Sum256(data)
	total := (len(data) + blobChunk - 1) / blobChunk
	if total < 2 {
		t.Fatalf("obrazek testowy mieści się w jednej części (%d B)", len(data))
	}
	c.mu.Lock()
	c.wants[string(hash[:])] = &blobWant{chat: bytes.Clone(chat), mime: "image/png", size: int64(len(data))}
	c.mu.Unlock()

	part := func(idx, tot int) Ctrl {
		end := min((idx+1)*blobChunk, len(data))
		return Ctrl{T: "blobpart", Chat: chat, Blob: hash[:], Idx: idx, Total: tot, Part: data[idx*blobChunk : end]}
	}

	stranger := &p2p.Peer{Pub: identity.Create().Pub}
	c.handleBlobPart(stranger, part(0, total))
	c.mu.Lock()
	got := c.incoming[string(hash[:])]
	c.mu.Unlock()
	if got != nil {
		t.Fatal("część od nie-członka założyła bufor składania")
	}

	me := &p2p.Peer{Pub: c.ID.Pub}
	c.handleBlobPart(me, part(0, total))
	c.mu.Lock()
	a := c.incoming[string(hash[:])]
	c.mu.Unlock()
	if a == nil || a.got != 1 {
		t.Fatalf("część od członka nie została przyjęta: %+v", a)
	}

	// crafted packet claiming a different Total
	c.handleBlobPart(me, Ctrl{T: "blobpart", Chat: chat, Blob: hash[:], Idx: 0, Total: total + 1, Part: []byte("x")})
	c.mu.Lock()
	after := c.incoming[string(hash[:])]
	c.mu.Unlock()
	if after != a || after.got != 1 {
		t.Fatal("sprzeczny Total skasował trwające pobieranie")
	}
}

func TestNormalizeWS(t *testing.T) {
	for in, want := range map[string]string{
		"vps:7777":         "ws://vps:7777",
		"  vps:7777  ":     "ws://vps:7777",
		"ws://vps:7777":    "ws://vps:7777",
		"wss://szept.dev":  "wss://szept.dev",
		"wws://szept.dev":  "wws://szept.dev", // literówka zostaje literówką
		"http://szept.dev": "http://szept.dev",
		"":                 "",
	} {
		if got := NormalizeWS(in); got != want {
			t.Errorf("NormalizeWS(%q) = %q, chciałem %q", in, got, want)
		}
	}
}
