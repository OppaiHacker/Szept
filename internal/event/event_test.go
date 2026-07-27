package event

import (
	"bytes"
	"crypto/ed25519"
	"testing"

	"szept/internal/crypt"
)

func TestDAGFoldAndAuth(t *testing.T) {
	_, owner, _ := ed25519.GenerateKey(nil)
	alicePub, alice, _ := ed25519.GenerateKey(nil)
	_, mallory, _ := ed25519.GenerateKey(nil)
	key := crypt.NewKey()
	keys := map[uint32][]byte{1: key}

	gen, err := New(owner, make([]byte, 32), KindCreate, Body{Name: "grupa", Text: "szef"}, 1, nil, 1, key)
	if err != nil {
		t.Fatal(err)
	}
	chatID := gen.ID()

	add, _ := New(owner, chatID, KindAdd, Body{Pub: alicePub, Name: "alice"}, 2, [][]byte{chatID}, 1, key)
	msg, _ := New(alice, chatID, KindMsg, Body{Text: "hej"}, 3, [][]byte{add.ID()}, 1, key)

	st, err := Fold([]*Event{msg, gen, add}, chatID, keys) // shuffled order
	if err != nil {
		t.Fatal(err)
	}
	if st.Name != "grupa" || !st.IsMember(alicePub) || st.Epoch != 1 {
		t.Fatalf("bad state: %+v", st)
	}

	// non-member message must be rejected
	evil, _ := New(mallory, chatID, KindMsg, Body{Text: "spam"}, 4, [][]byte{msg.ID()}, 1, key)
	if _, err := Fold([]*Event{gen, add, msg, evil}, chatID, keys); err == nil {
		t.Fatal("accepted message from non-member")
	}

	// tampered ciphertext must fail signature or decrypt
	tampered := *msg
	tampered.Box = bytes.Clone(msg.Box)
	tampered.Box[len(tampered.Box)-1] ^= 1
	if tampered.VerifySig() {
		t.Fatal("signature survived tampering")
	}

	// decrypt roundtrip
	b, err := msg.Decrypt(key)
	if err != nil || b.Text != "hej" {
		t.Fatalf("decrypt: %v %+v", err, b)
	}
	// wrong key must fail
	if _, err := msg.Decrypt(crypt.NewKey()); err == nil {
		t.Fatal("decrypted with wrong key")
	}
}

// Porządek foldu musi wynikać z DAG-u, usunięcie musi kaskadować, a zły klucz
// epoki nie może wywracać całego czatu.
func TestFoldOrderingAndRemoval(t *testing.T) {
	_, owner, _ := ed25519.GenerateKey(nil)
	malPub, mallory, _ := ed25519.GenerateKey(nil)
	sockPub, _, _ := ed25519.GenerateKey(nil)
	key := crypt.NewKey()
	keys := map[uint32][]byte{1: key}

	gen, _ := New(owner, make([]byte, 32), KindCreate, Body{Name: "grupa", Text: "szef"}, 1, nil, 1, key)
	chatID := gen.ID()
	add, _ := New(owner, chatID, KindAdd, Body{Pub: malPub, Name: "mallory"}, 2, [][]byte{chatID}, 1, key)
	rm, _ := New(owner, chatID, KindRemove, Body{Pub: malPub}, 3, [][]byte{add.ID()}, 1, key)

	// antydatowanie: "add" z lamportem sprzed usunięcia nie może wejść w stan
	backdated, _ := New(mallory, chatID, KindAdd, Body{Pub: sockPub}, 3, [][]byte{add.ID()}, 1, key)
	if st, err := Fold([]*Event{gen, add, rm, backdated}, chatID, keys); err == nil && st.IsMember(sockPub) {
		t.Fatal("antydatowany add wpuścił usuniętego z powrotem")
	}
	// lamport musi być max(rodzice)+1 — inaczej 2^64-1 przewija licznik i blokuje dopisywanie
	if _, err := Fold([]*Event{gen, mustNew(t, owner, chatID, KindMsg, Body{Text: "x"}, ^uint64(0), [][]byte{chatID}, key)},
		chatID, keys); err == nil {
		t.Fatal("zdarzenie z dowolnym lamportem weszło do DAG-u")
	}
	// usunięcie kaskaduje na tych, których usuwany wpuścił
	sock, _ := New(mallory, chatID, KindAdd, Body{Pub: sockPub, Name: "sock"}, 3, [][]byte{add.ID()}, 1, key)
	rm2, _ := New(owner, chatID, KindRemove, Body{Pub: malPub}, 4, [][]byte{sock.ID()}, 1, key)
	st, err := Fold([]*Event{gen, add, sock, rm2}, chatID, keys)
	if err != nil {
		t.Fatal(err)
	}
	if st.IsMember(malPub) || st.IsMember(sockPub) {
		t.Fatalf("kick nie kaskaduje: mallory=%v sock=%v", st.IsMember(malPub), st.IsMember(sockPub))
	}
	// podrzucony klucz epoki: treść pomijamy, czat ma działać dalej
	if _, err := Fold([]*Event{gen, add}, chatID, map[uint32][]byte{1: key, 7: crypt.NewKey()}); err != nil {
		t.Fatalf("zły klucz epoki zabił fold: %v", err)
	}
	// drugi "create" nie przejmuje własności czatu
	takeover, _ := New(mallory, chatID, KindCreate, Body{Name: "moje", Text: "m"}, 3, [][]byte{add.ID()}, 1, key)
	if st, err := Fold([]*Event{gen, add, takeover}, chatID, keys); err == nil && !bytes.Equal(st.Owner, gen.Author) {
		t.Fatal("drugi genesis przejął własność")
	}
}

func mustNew(t *testing.T, priv ed25519.PrivateKey, chat []byte, kind string, b Body, lamport uint64, parents [][]byte, key []byte) *Event {
	t.Helper()
	e, err := New(priv, chat, kind, b, lamport, parents, 1, key)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestReactAndDeleteEvents(t *testing.T) {
	_, owner, _ := ed25519.GenerateKey(nil)
	alicePub, alice, _ := ed25519.GenerateKey(nil)
	key := crypt.NewKey()
	keys := map[uint32][]byte{1: key}

	gen, _ := New(owner, make([]byte, 32), KindCreate, Body{Name: "grupa", Text: "szef"}, 1, nil, 1, key)
	chatID := gen.ID()
	add, _ := New(owner, chatID, KindAdd, Body{Pub: alicePub, Name: "alice"}, 2, [][]byte{chatID}, 1, key)
	msg, _ := New(alice, chatID, KindMsg, Body{Text: "hejka"}, 3, [][]byte{add.ID()}, 1, key)

	msgID := msg.ID()
	react, _ := New(alice, chatID, KindReact, Body{Target: msgID, Text: "👍"}, 4, [][]byte{msgID}, 1, key)
	del, _ := New(alice, chatID, KindDelete, Body{Target: msgID}, 5, [][]byte{react.ID()}, 1, key)

	st, err := Fold([]*Event{gen, add, msg, react, del}, chatID, keys)
	if err != nil {
		t.Fatal(err)
	}
	if st.Reactions[string(msgID)][string(alicePub)] != "👍" {
		t.Fatalf("brak reakcji w stanie: %v", st.Reactions)
	}
	if !st.Deleted[string(msgID)] {
		t.Fatalf("brak oznaczenia usunięcia w stanie: %v", st.Deleted)
	}
}
