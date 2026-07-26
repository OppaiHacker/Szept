package store

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"szept/internal/crypt"
	"szept/internal/event"
)

func TestStoreRoundtrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"), crypt.NewKey())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	_, priv, _ := ed25519.GenerateKey(nil)
	key := crypt.NewKey()
	gen, _ := event.New(priv, make([]byte, 32), event.KindCreate, event.Body{Name: "c", Text: "a"}, 1, nil, 1, key)
	chat := gen.ID()
	if fresh, err := s.AddEvent(chat, gen); err != nil || !fresh {
		t.Fatalf("add: %v fresh=%v", err, fresh)
	}
	if fresh, _ := s.AddEvent(chat, gen); fresh {
		t.Fatal("dup not detected")
	}
	m, _ := event.New(priv, chat, event.KindMsg, event.Body{Text: "x"}, 2, [][]byte{chat}, 1, key)
	s.AddEvent(chat, m)

	heads := s.Heads(chat)
	if len(heads) != 1 || string(heads[0]) != string(m.ID()) {
		t.Fatalf("heads wrong: %d", len(heads))
	}
	if s.NextLamport(chat) != 3 {
		t.Fatalf("lamport: %d", s.NextLamport(chat))
	}

	s.PutChatKey(chat, 1, key)
	ks, _ := s.ChatKeys(chat)
	if string(ks[1]) != string(key) {
		t.Fatal("key roundtrip failed")
	}

	// bloby: adresowane treścią, ta sama zawartość nie dubluje się w bazie
	data := []byte("udawany obrazek")
	hash, err := s.PutBlob(data)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := s.GetBlob(hash); !ok || string(got) != string(data) {
		t.Fatal("blob roundtrip failed")
	}
	if h2, _ := s.PutBlob(data); string(h2) != string(hash) {
		t.Fatal("ten sam blob dostał inny skrót")
	}
	if s.HasBlob(crypt.RandBytes(32)) {
		t.Fatal("HasBlob na losowym skrócie")
	}
	if _, ok := s.Avatar(priv.Public().(ed25519.PublicKey)); ok {
		t.Fatal("awatar znaleziony bez zapisu")
	}
	pub := priv.Public().(ed25519.PublicKey)
	s.PutAvatar(pub, AvatarMeta{Blob: hash, Mime: "image/gif"})
	if a, ok := s.Avatar(pub); !ok || a.Mime != "image/gif" || string(a.Blob) != string(hash) {
		t.Fatalf("awatar roundtrip: %+v %v", a, ok)
	}
	s.PutAvatar(pub, AvatarMeta{}) // pusty = usunięcie
	if _, ok := s.Avatar(pub); ok {
		t.Fatal("awatar przeżył usunięcie")
	}

	// awatary nie liczą się do odznaki nieprzeczytanych
	before := s.EventCount(chat)
	av, _ := event.New(priv, chat, event.KindAvatar, event.Body{Blob: hash, Mime: "image/gif"}, 3, [][]byte{m.ID()}, 1, key)
	s.AddEvent(chat, av)
	if after := s.EventCount(chat); after != before {
		t.Fatalf("EventCount %d -> %d, awatar nabił odznakę", before, after)
	}

	pinHash := sha256.Sum256([]byte("123456"))
	inv := Invite{Chat: chat, PinHash: pinHash[:], Exp: time.Now().Add(time.Minute).Unix()}
	h := crypt.RandBytes(32)
	s.PutInvite(h, inv)
	if _, err := s.TakeInvite(h, []byte("999999")); err == nil {
		t.Fatal("wrong pin accepted")
	}
	if _, err := s.TakeInvite(h, []byte("123456")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TakeInvite(h, []byte("123456")); err == nil {
		t.Fatal("invite reused")
	}

	// zgadywanie PIN-u pali zaproszenie: "redeem" to zwykła wiadomość na
	// otwartym kanale, więc bez licznika 10^6 kombinacji idzie w sekundy
	h3 := crypt.RandBytes(32)
	s.PutInvite(h3, Invite{Chat: chat, PinHash: pinHash[:], Exp: time.Now().Add(time.Minute).Unix()})
	for i := 0; i < MaxPinTries; i++ {
		if _, err := s.TakeInvite(h3, []byte("000000")); err == nil {
			t.Fatal("wrong pin accepted")
		}
	}
	if _, err := s.TakeInvite(h3, []byte("123456")); err == nil {
		t.Fatal("zaproszenie przeżyło zgadywanie PIN-u")
	}

	// expired invite must be rejected and not counted as open
	h2 := crypt.RandBytes(32)
	s.PutInvite(h2, Invite{Chat: chat, Exp: time.Now().Add(-time.Second).Unix()})
	if _, err := s.TakeInvite(h2, nil); err == nil {
		t.Fatal("expired invite accepted")
	}
	if n := s.OpenInviteCount(); n != 0 {
		t.Fatalf("OpenInviteCount = %d, want 0", n)
	}
}

// Plik bazy nie ma zdradzać metadanych rozmowy (kto, jakiego rodzaju zdarzenia,
// nazwy czatów), a wartości mają być związane ze swoim miejscem — przestawienie
// dwóch rekordów musi się wywalić na AEAD.
func TestAtRestOpaque(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	sk := crypt.NewKey() // ten sam klucz magazynu przy ponownym otwarciu
	s, err := Open(path, sk)
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, _ := ed25519.GenerateKey(nil)
	key := crypt.NewKey()
	gen, _ := event.New(priv, make([]byte, 32), event.KindCreate,
		event.Body{Name: "tajna-nazwa", Text: "a"}, 1, nil, 1, key)
	chat := gen.ID()
	s.AddEvent(chat, gen)
	s.PutChat(chat, ChatMeta{Name: "tajna-nazwa"})
	s.PutChatKey(chat, 1, key)
	h1, _ := s.PutBlob([]byte("plik jeden"))
	h2, _ := s.PutBlob([]byte("plik dwa"))
	s.Close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range [][]byte{pub, key, []byte("tajna-nazwa"), []byte(`"kind":"create"`), []byte("plik jeden")} {
		if bytes.Contains(raw, leak) {
			t.Fatalf("plik bazy zawiera jawnie %q", leak)
		}
	}

	// przestawienie wartości dwóch blobów: klucz ten sam, miejsce inne
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bBlobs)
		v1, v2 := bytes.Clone(b.Get(h1)), bytes.Clone(b.Get(h2))
		b.Put(h1, v2)
		return b.Put(h2, v1)
	})
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path, sk)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if _, ok := s2.GetBlob(h1); ok {
		t.Fatal("przestawiony blob dał się odczytać mimo poprawnego klucza")
	}
	// kontrola dodatnia: nieruszony rekord z tym samym kluczem czyta się dalej
	h3, _ := s2.PutBlob([]byte("plik trzy"))
	if v, ok := s2.GetBlob(h3); !ok || string(v) != "plik trzy" {
		t.Fatal("nieruszony blob przestał się czytać")
	}
}
