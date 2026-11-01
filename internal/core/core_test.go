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
	"szept/internal/event"
	"szept/internal/identity"
	"szept/internal/p2p"
	"szept/internal/store"
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

	chat, err := a.CreateChat("tajna-grupa", false)
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

	if _, err := a.CreateChat("grupa", false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateChat("druga-grupa", false); err != nil {
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
	// link up first: a chat created while already connected used to reach b
	// only after a reconnect (no sync for a chat b had never heard of)
	go a.ensurePeer(fs[0].Pub)
	waitFor(t, "link up", 20*time.Second, func() bool { return a.Online(fs[0].Pub) })
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

// An epoch key is only accepted when the signed DAG vouches for it,
// and a wrong one already stored is replaced once the right one is offered.
// Pre-filling a future epoch used to lock the real key out forever, and the
// first membership event landing in that epoch killed the chat outright.
func TestEpochKeyBoundToRekey(t *testing.T) {
	srv := httptest.NewServer(connector.NewServer())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	c := newCore(t, "ala", url)
	chat, err := c.CreateChat("grupa", false)
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
	me := &p2p.Peer{Pub: c.ID.Pub}
	stored := func(ep uint32) []byte {
		k, _ := c.St.ChatKeys(chat)
		return k[ep]
	}
	// a fake key never replaces the right one
	c.handleKeys(me, Ctrl{T: "keys", Chat: chat, Epochs: map[uint32][]byte{2: crypt.NewKey()}})
	if !bytes.Equal(stored(2), real2) {
		t.Fatal("podrobiony klucz epoki nadpisał prawdziwy")
	}
	// A fake key that got in anyway is repaired by the right one
	c.St.PutChatKey(chat, 2, crypt.NewKey())
	c.invalidate(chat)
	c.handleKeys(me, Ctrl{T: "keys", Chat: chat, Epochs: map[uint32][]byte{2: real2}})
	if !bytes.Equal(stored(2), real2) {
		t.Fatal("prawdziwy klucz nie zastąpił podrobionego")
	}
	// an epoch nobody announced is refused outright — that is the pre-poisoning path
	c.handleKeys(me, Ctrl{T: "keys", Chat: chat, Epochs: map[uint32][]byte{9: crypt.NewKey()}})
	if stored(9) != nil {
		t.Fatal("klucz dla nieogłoszonej epoki przyjęty")
	}
	// joining (no state yet): the genesis, authenticated by the chat ID, judges epoch 1
	gen, err := c.St.GetEvent(chat, chat)
	if err != nil {
		t.Fatal(err)
	}
	if known, ok := keyVerdict(nil, gen, 1, keys[1]); !known || !ok {
		t.Fatal("genesis odrzucił prawdziwy klucz epoki 1")
	}
	if known, ok := keyVerdict(nil, gen, 1, crypt.NewKey()); !known || ok {
		t.Fatal("genesis przyjął podrobiony klucz epoki 1")
	}
}

// bBlobs is global, so membership in one chat must not be a pass to
// attachments announced in another.
func TestServeBlobCrossChat(t *testing.T) {
	srv := httptest.NewServer(connector.NewServer())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	c := newCore(t, "ala", url)
	secret, err := c.CreateChat("tajny", false)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := c.CreateChat("wspolny", false)
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
	// a member of the shared chat re-announces the hash: still not servable there
	keys, _ := c.St.ChatKeys(shared)
	mal := identity.Create()
	fake, _ := event.New(mal.Priv, shared, event.KindFile, event.Body{Blob: hash, Mime: "image/png"}, 9, [][]byte{shared}, 1, keys[1])
	c.noteAttachment(shared, fake)
	if c.St.BlobInChat(shared, hash) {
		t.Fatal("cudze ogłoszenie hasha wpisało go do indeksu wspólnego czatu")
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
	chat, err := c.CreateChat("grupa", false)
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
	c.wants[string(hash[:])] = &blobWant{chat: bytes.Clone(chat), mime: "image/png", size: int64(len(data)), peer: c.ID.Pub}
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

// One sender holding a full share cannot start more buffers.
func TestBlobSenderBudget(t *testing.T) {
	c := newCore(t, "ala", "ws://127.0.0.1:1")
	chat, _ := c.CreateChat("grupa", false)
	data := testImagePNG(t)
	hash := sha256.Sum256(data)
	me := &p2p.Peer{Pub: c.ID.Pub}
	c.mu.Lock()
	c.wants[string(hash[:])] = &blobWant{chat: bytes.Clone(chat), mime: "image/png", peer: c.ID.Pub}
	c.incoming["stalled"] = &blobAsm{from: string(me.Pub), bytes: senderBudget, last: time.Now()}
	c.mu.Unlock()
	c.handleBlobPart(me, Ctrl{T: "blobpart", Chat: chat, Blob: hash[:], Idx: 0, Total: 2, Part: data[:blobChunk]})
	c.mu.Lock()
	got := c.incoming[string(hash[:])]
	c.mu.Unlock()
	if got != nil {
		t.Fatal("nadawca przekroczył swój limit buforów")
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

// An open invite must not let arbitrary strangers connect (and learn our IP):
// only an offer carrying the hash of that invite's secret gets through.
func TestStrangerNeedsInviteTicket(t *testing.T) {
	a := newCore(t, "ala", "ws://127.0.0.1:1")
	chat, _ := a.CreateChat("g", false)
	code, _, err := a.NewInvite(chat)
	if err != nil {
		t.Fatal(err)
	}
	inv, _ := ParseInvite(code)
	stranger := identity.Create().Pub
	if a.acceptPeer(stranger, nil) || a.acceptPeer(stranger, crypt.RandBytes(32)) {
		t.Fatal("stranger accepted without the invite ticket")
	}
	h := sha256.Sum256(inv.Secret)
	if !a.acceptPeer(stranger, h[:]) {
		t.Fatal("invite holder rejected")
	}
}

func TestBlobFrame(t *testing.T) {
	chat, hash := bytes.Repeat([]byte{7}, 32), bytes.Repeat([]byte{9}, 32)
	m, ok := parseBlobFrame(blobFrame(chat, hash, 3, 10, []byte("part")))
	if !ok || !bytes.Equal(m.Chat, chat) || !bytes.Equal(m.Blob, hash) || m.Idx != 3 || m.Total != 10 || string(m.Part) != "part" {
		t.Fatalf("round trip: %+v %v", m, ok)
	}
	for _, junk := range [][]byte{nil, {blobTag}, []byte(`{"t":"sync"}`), blobFrame(chat, hash, 0, 1, nil)[:40]} {
		if _, ok := parseBlobFrame(junk); ok {
			t.Fatalf("accepted %q", junk)
		}
	}
}

// With QuietLookup only members of a chat the user is in get looked
// up, so the connector does not learn the whole contact list.
func TestQuietLookup(t *testing.T) {
	a := newCore(t, "ala", "ws://127.0.0.1:1")
	b, d := identity.Create(), identity.Create()
	a.St.PutFriend(store.Friend{Pub: b.Pub, Name: "bartek"})
	chat, err := a.CreateDirect(store.Friend{Pub: d.Pub, Name: "dorota"})
	if err != nil {
		t.Fatal(err)
	}
	has := func(pub []byte) bool {
		for _, p := range a.lookupTargets() {
			if bytes.Equal(p, pub) {
				return true
			}
		}
		return false
	}
	if !has(b.Pub) || !has(d.Pub) {
		t.Fatal("normal mode should look up everyone")
	}
	a.SetQuietLookup(true)
	a.mu.Lock()
	clear(a.active)
	a.mu.Unlock()
	if has(b.Pub) || has(d.Pub) {
		t.Fatal("quiet mode looked up an idle contact")
	}
	a.MarkChatRead(chat)
	if has(b.Pub) || !has(d.Pub) {
		t.Fatal("quiet mode should look up only the open chat")
	}
}

// In a chat without history a newcomer folds membership from
// genesis with member keys only, never gets the content keys from before the
// join, and the owner rotates the key so they can write.
func TestNoHistoryForNewMembers(t *testing.T) {
	srv := httptest.NewServer(connector.NewServer())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	a := newCore(t, "ala", url)
	b := newCore(t, "bartek", url)

	chat, err := a.CreateChat("bez-historii", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Send(chat, "stare"); err != nil {
		t.Fatal(err)
	}
	code, pin, err := a.NewInvite(chat)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Redeem(code, pin); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "b folds the chat", 20*time.Second, func() bool {
		st, err := b.ChatState(chat)
		return err == nil && st.IsMember(b.ID.Pub) && st.Name == "bez-historii" && st.Epoch == 2 && !st.Degraded()
	})
	waitFor(t, "b can write", 10*time.Second, func() bool { return b.Send(chat, "hej") == nil })
	if err := a.Send(chat, "nowe"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "b reads after joining", 10*time.Second, func() bool {
		for _, m := range b.Messages(chat) {
			if m.Text == "nowe" {
				return true
			}
		}
		return false
	})
	waitFor(t, "a reads b", 10*time.Second, func() bool {
		for _, m := range a.Messages(chat) {
			if m.Text == "hej" {
				return true
			}
		}
		return false
	})
	for _, m := range b.Messages(chat) {
		if m.Text == "stare" {
			t.Fatal("newcomer read history from before joining")
		}
	}
	if keys, _ := b.St.ChatKeys(chat); keys[1] != nil {
		t.Fatal("newcomer holds the epoch-1 content key")
	}
}

func TestChannels(t *testing.T) {
	srv := httptest.NewServer(connector.NewServer())
	defer srv.Close()
	a := newCore(t, "ala", "ws"+strings.TrimPrefix(srv.URL, "http"))
	chat, err := a.CreateChat("grupa", false)
	if err != nil {
		t.Fatal(err)
	}
	cat, _ := a.AddChannel(chat, "cat", "Gry", nil)
	ch, err := a.AddChannel(chat, "text", "memy", cat)
	if err != nil {
		t.Fatal(err)
	}
	a.SetChannel(chat, ch)
	a.Send(chat, "w kanale")
	a.SetChannel(chat, nil)
	a.Send(chat, "w czacie")
	a.PutChannel(chat, ch, "text", "memy2", cat, false)
	a.PutChannel(chat, cat, "cat", "", nil, false)
	// the defaults rename, move and archive like any other channel
	a.PutChannel(chat, nil, "voice", "Głośno", nil, true)
	st, _ := a.ChatState(chat)
	if len(st.Channels) != 3 || st.Channels[0].Name != "czat" || st.Channels[1].Name != "Głośno" ||
		!st.Channels[1].Arch || st.Channels[2].Name != "memy2" {
		t.Fatalf("channels: %+v", st.Channels)
	}
	a.PutChannel(chat, nil, "voice", "", nil, false)
	if st, _ = a.ChatState(chat); len(st.Channels) != 2 || st.Channels[1].Name != "memy2" {
		t.Fatalf("after purge: %+v", st.Channels)
	}
	var in, def int
	for _, m := range a.Messages(chat) {
		if m.Kind == "msg" && bytes.Equal(m.Ch, ch) {
			in++
		} else if m.Kind == "msg" && m.Ch == nil {
			def++
		}
	}
	if in != 1 || def != 1 {
		t.Fatalf("in=%d def=%d", in, def)
	}
}

// Two people opening a DM at the same moment create two chats. Both sides must
// settle on the same one and show every message in it.
func TestDirectTwinsMerge(t *testing.T) {
	srv := httptest.NewServer(connector.NewServer())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	a := newCore(t, "ala", url)
	b := newCore(t, "bartek", url)
	a.AddFriend(b.FriendCode())
	b.AddFriend(a.FriendCode())
	fa, _ := a.St.Friends()
	fb, _ := b.St.Friends()
	ca, _ := a.CreateDirect(fa[0])
	cb, _ := b.CreateDirect(fb[0])
	if bytes.Equal(ca, cb) {
		t.Fatal("test needs two separate DMs")
	}
	a.Send(ca, "od ali")
	b.Send(cb, "od bartka")
	for _, c := range []*Core{a, b} {
		waitFor(t, "one merged DM", 20*time.Second, func() bool {
			var direct []ChatInfo
			for _, x := range c.Chats() {
				if x.Direct {
					direct = append(direct, x)
				}
			}
			if len(direct) != 1 {
				return false
			}
			n := 0
			for _, m := range c.Messages(direct[0].ID) {
				if m.Text == "od ali" || m.Text == "od bartka" {
					n++
				}
			}
			return n == 2
		})
	}
	da, _ := a.CreateDirect(fa[0])
	db, _ := b.CreateDirect(fb[0])
	if !bytes.Equal(da, db) {
		t.Fatalf("sides picked different DMs: %x vs %x", da[:4], db[:4])
	}
}

func TestReplyAndPin(t *testing.T) {
	srv := httptest.NewServer(connector.NewServer())
	defer srv.Close()
	a := newCore(t, "ala", "ws"+strings.TrimPrefix(srv.URL, "http"))
	chat, err := a.CreateChat("grupa", false)
	if err != nil {
		t.Fatal(err)
	}
	a.Send(chat, "pytanie")
	q := a.Messages(chat)
	first := q[len(q)-1].ID
	if err := a.SendReply(chat, "odpowiedź", first); err != nil {
		t.Fatal(err)
	}
	a.PinMsg(chat, first, false)
	ms := a.Messages(chat)
	if !bytes.Equal(ms[len(ms)-1].Reply, first) || !ms[len(ms)-2].Pinned {
		t.Fatalf("reply/pin: %+v", ms)
	}
	a.PinMsg(chat, first, true)
	ms = a.Messages(chat)
	if ms[len(ms)-2].Pinned {
		t.Fatal("unpin ignored")
	}
}

// A stale avatar event (late sync, or another chat scanned last on startup)
// must not replace a newer avatar in the profile index.
func TestStaleAvatarDoesNotWin(t *testing.T) {
	a := newCore(t, "ala", "ws://127.0.0.1:1")
	c1, err := a.CreateChat("one", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateChat("two", false); err != nil {
		t.Fatal(err)
	}
	if err := a.SetAvatar(testImageGIF(t), image.Rectangle{}); err != nil {
		t.Fatal(err)
	}
	want, _ := a.St.Profile(a.ID.Pub)
	stale := bytes.Repeat([]byte{7}, 32)
	if _, err := a.appendEvent(c1, event.KindAvatar,
		event.Body{Blob: stale, Mime: "image/png", TS: time.Now().Unix() - 100}); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		a.rescanAttachments()
		if got, _ := a.St.Profile(a.ID.Pub); !bytes.Equal(got.Blob, want.Blob) {
			t.Fatalf("stale avatar won: %x", got.Blob)
		}
	}
	// two changes within one second: the later one wins (ns, not seconds)
	if err := a.SetAvatar(testImagePNG(t), image.Rectangle{}); err != nil {
		t.Fatal(err)
	}
	png, _ := a.St.Profile(a.ID.Pub)
	if err := a.SetAvatar(testImageGIF(t), image.Rectangle{}); err != nil {
		t.Fatal(err)
	}
	a.rescanAttachments()
	if got, _ := a.St.Profile(a.ID.Pub); bytes.Equal(got.Blob, png.Blob) || !bytes.Equal(got.Blob, want.Blob) {
		t.Fatalf("same-second change lost: %x", got.Blob)
	}
	// a cleared avatar stays cleared
	if err := a.ClearAvatar(); err != nil {
		t.Fatal(err)
	}
	a.rescanAttachments()
	if got, _ := a.St.Profile(a.ID.Pub); len(got.Blob) != 0 {
		t.Fatalf("cleared avatar came back: %x", got.Blob)
	}
}

// A peer's profile stamp far in the future is ignored (it would freeze that
// profile for good), and our own change after a clock rollback still wins.
func TestProfileClockSkew(t *testing.T) {
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
	waitFor(t, "b joins the chat", 25*time.Second, func() bool {
		st, err := b.ChatState(chat)
		return err == nil && st.IsMember(b.ID.Pub)
	})
	future := time.Now().Add(24 * time.Hour)
	ev, err := b.appendEvent(chat, event.KindAvatar, event.Body{Blob: bytes.Repeat([]byte{9}, 32),
		Mime: "image/png", TS: future.Unix(), NS: future.UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a receives the event", 25*time.Second, func() bool { return a.St.HasEvent(chat, ev.ID()) })
	if got, _ := a.St.Profile(b.ID.Pub); bytes.Equal(got.Blob, bytes.Repeat([]byte{9}, 32)) {
		t.Fatal("future-dated avatar accepted")
	}

	// our clock jumped ahead once, then came back: the next change must still win
	card, _ := a.St.Profile(a.ID.Pub)
	card.AvatarTS = future.UnixNano()
	a.St.PutProfile(a.ID.Pub, card)
	if err := a.SetAvatar(testImageGIF(t), image.Rectangle{}); err != nil {
		t.Fatal(err)
	}
	want, _ := a.St.Profile(a.ID.Pub)
	if want.AvatarTS <= future.UnixNano() {
		t.Fatalf("stamp went backwards: %d", want.AvatarTS)
	}
	a.rescanAttachments()
	if got, _ := a.St.Profile(a.ID.Pub); !bytes.Equal(got.Blob, want.Blob) {
		t.Fatalf("new avatar lost after rollback: %x", got.Blob)
	}
}

// One peer cannot take the whole orphan buffer: past its share, its events are
// dropped and another peer's still park.
func TestOrphanSenderCap(t *testing.T) {
	c := newCore(t, "ala", "ws://127.0.0.1:1")
	chat, _ := c.CreateChat("grupa", false)
	mal := identity.Create()
	a, b := &p2p.Peer{Pub: mal.Pub}, &p2p.Peer{Pub: identity.Create().Pub}
	ev := func(i int) *event.Event {
		e, _ := event.New(mal.Priv, chat, event.KindMsg, event.Body{}, uint64(i+2), [][]byte{{byte(i), byte(i >> 8)}}, 1, crypt.NewKey())
		return e
	}
	for i := range maxSenderOrphans + 10 {
		c.park(chat, ev(i), a)
	}
	c.park(chat, ev(maxSenderOrphans+20), b)
	c.mu.Lock()
	n := len(c.orphans[string(chat)])
	c.mu.Unlock()
	if n != maxSenderOrphans+1 {
		t.Fatalf("w buforze %d sierot, chciałem %d", n, maxSenderOrphans+1)
	}
}

// A member leaves on their own: the owner sees them go and rotates the key, the
// chat is gone on the leaver's side, and the owner may leave only when alone.
func TestLeave(t *testing.T) {
	srv := httptest.NewServer(connector.NewServer())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	a := newCore(t, "ala", url)
	b := newCore(t, "bartek", url)

	chat, err := a.CreateChat("grupa", false)
	if err != nil {
		t.Fatal(err)
	}
	code, pin, err := a.NewInvite(chat)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Redeem(code, pin); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "b joins", 20*time.Second, func() bool {
		st, err := b.ChatState(chat)
		return err == nil && st.IsMember(b.ID.Pub)
	})
	if err := a.Leave(chat); err == nil {
		t.Fatal("owner left a chat that still has members")
	}
	before, _ := a.ChatState(chat)
	a.P2P.Peer(b.ID.Pub).Close() // nobody to carry the leave: refused
	waitFor(t, "link down", 10*time.Second, func() bool { return !b.Online(a.ID.Pub) })
	if err := b.Leave(chat); err == nil {
		t.Fatal("left with no member online")
	}
	go b.ensurePeer(a.ID.Pub)
	waitFor(t, "link back", 20*time.Second, func() bool { return b.Online(a.ID.Pub) })
	if err := b.Leave(chat); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ChatState(chat); err == nil || !b.left(chat) {
		t.Fatal("chat still on the leaver's side")
	}
	waitFor(t, "a sees b leave and rekeys", 10*time.Second, func() bool {
		st, err := a.ChatState(chat)
		return err == nil && !st.IsMember(b.ID.Pub) && st.Epoch == before.Epoch+1
	})
	if err := a.Leave(chat); err != nil {
		t.Fatalf("owner alone cannot delete: %v", err)
	}
}

func TestCallState(t *testing.T) {
	for want, call := range map[string]Call{
		"":    {Mic: true},
		"m":   {},
		"md":  {Deaf: true},
		"cs":  {Mic: true, Cam: true, Screen: true},
		"mdc": {Deaf: true, Cam: true},
	} {
		if got := call.State(); got != want {
			t.Errorf("%+v: %q, want %q", call, got, want)
		}
	}
}

func TestDeafenMutesMic(t *testing.T) {
	srv := httptest.NewServer(connector.NewServer())
	defer srv.Close()
	c := newCore(t, "ala", "ws"+strings.TrimPrefix(srv.URL, "http"))
	chat, err := c.CreateChat("g", false)
	if err != nil {
		t.Fatal(err)
	}
	call := &Call{Chat: chat, Joined: true, Mic: true, Members: map[string]bool{}}
	c.calls[string(chat)] = call
	check := func(step, want string) {
		t.Helper()
		if got := call.State(); got != want {
			t.Fatalf("%s: %q, want %q", step, got, want)
		}
	}
	c.ToggleDeafen(chat)
	check("deafen", "md")
	c.ToggleDeafen(chat)
	check("undeafen", "")
	c.ToggleMic(chat)
	c.ToggleDeafen(chat)
	c.ToggleDeafen(chat)
	check("muted by hand stays muted", "m")
	c.ToggleMic(chat)
	c.ToggleDeafen(chat)
	c.ToggleMic(chat)
	check("mic on undeafens", "")
}
