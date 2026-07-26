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
