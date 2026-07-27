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

// Fold order must derive from DAG, removal must cascade, and bad epoch key
// must not break entire chat.
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

	// backdating: "add" with lamport prior to removal cannot enter state
	backdated, _ := New(mallory, chatID, KindAdd, Body{Pub: sockPub}, 3, [][]byte{add.ID()}, 1, key)
	if st, err := Fold([]*Event{gen, add, rm, backdated}, chatID, keys); err == nil && st.IsMember(sockPub) {
		t.Fatal("backdated add admitted removed member back in")
	}
	// lamport must be max(parents)+1 — otherwise 2^64-1 rolls counter and blocks appending
	if _, err := Fold([]*Event{gen, mustNew(t, owner, chatID, KindMsg, Body{Text: "x"}, ^uint64(0), [][]byte{chatID}, key)},
		chatID, keys); err == nil {
		t.Fatal("event with arbitrary lamport entered DAG")
	}
	// removal cascades to those introduced by removed member
	sock, _ := New(mallory, chatID, KindAdd, Body{Pub: sockPub, Name: "sock"}, 3, [][]byte{add.ID()}, 1, key)
	rm2, _ := New(owner, chatID, KindRemove, Body{Pub: malPub}, 4, [][]byte{sock.ID()}, 1, key)
	st, err := Fold([]*Event{gen, add, sock, rm2}, chatID, keys)
	if err != nil {
		t.Fatal(err)
	}
	if st.IsMember(malPub) || st.IsMember(sockPub) {
		t.Fatalf("kick does not cascade: mallory=%v sock=%v", st.IsMember(malPub), st.IsMember(sockPub))
	}
	// bad epoch key: skip content, chat remains operational
	if _, err := Fold([]*Event{gen, add}, chatID, map[uint32][]byte{1: key, 7: crypt.NewKey()}); err != nil {
		t.Fatalf("bad epoch key killed fold: %v", err)
	}
	// second "create" does not take ownership of chat
	takeover, _ := New(mallory, chatID, KindCreate, Body{Name: "moje", Text: "m"}, 3, [][]byte{add.ID()}, 1, key)
	if st, err := Fold([]*Event{gen, add, takeover}, chatID, keys); err == nil && !bytes.Equal(st.Owner, gen.Author) {
		t.Fatal("second genesis took ownership")
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
		t.Fatalf("missing reaction in state: %v", st.Reactions)
	}
	if !st.Deleted[string(msgID)] {
		t.Fatalf("missing deletion flag in state: %v", st.Deleted)
	}
}
