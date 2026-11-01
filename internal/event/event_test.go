package event

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"reflect"
	"strings"
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

// "add" adds. It must never rewrite an entry that already exists —
// that was a way to undo the owner's kick, rename the owner in signed state and
// lock the whole chat by making the owner's kick cascade back onto themselves.
func TestAddNeverOverwritesMember(t *testing.T) {
	ownerPub, owner, _ := ed25519.GenerateKey(nil)
	malPub, mallory, _ := ed25519.GenerateKey(nil)
	helperPub, helper, _ := ed25519.GenerateKey(nil)
	key := crypt.NewKey()
	keys := map[uint32][]byte{1: key}

	gen, _ := New(owner, make([]byte, 32), KindCreate, Body{Name: "grupa", Text: "szef"}, 1, nil, 1, key)
	chatID := gen.ID()
	addMal, _ := New(owner, chatID, KindAdd, Body{Pub: malPub, Name: "mallory"}, 2, [][]byte{chatID}, 1, key)
	addHelper, _ := New(owner, chatID, KindAdd, Body{Pub: helperPub, Name: "pomocnik"}, 3, [][]byte{addMal.ID()}, 1, key)
	kick, _ := New(owner, chatID, KindRemove, Body{Pub: malPub}, 4, [][]byte{addHelper.ID()}, 1, key)

	// 3a: an ordinary member must not be able to resurrect a kicked account
	resurrect, _ := New(helper, chatID, KindAdd, Body{Pub: malPub, Name: "mallory"}, 5, [][]byte{kick.ID()}, 1, key)
	st, err := Fold([]*Event{gen, addMal, addHelper, kick, resurrect}, chatID, keys)
	if err != nil {
		t.Fatal(err)
	}
	if st.IsMember(malPub) {
		t.Fatal("zwykły członek wskrzesił wyrzucone konto")
	}

	// the owner, and only the owner, may let them back in
	readmit, _ := New(owner, chatID, KindAdd, Body{Pub: malPub, Name: "mallory"}, 6, [][]byte{resurrect.ID()}, 1, key)
	st, err = Fold([]*Event{gen, addMal, addHelper, kick, resurrect, readmit}, chatID, keys)
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsMember(malPub) {
		t.Fatal("właściciel nie może ponownie wpuścić wyrzuconego")
	}

	// 3b: a member must not be able to rewrite the owner's entry
	rename, _ := New(mallory, chatID, KindAdd, Body{Pub: ownerPub, Name: "NIE-SZEF"}, 3, [][]byte{addMal.ID()}, 1, key)
	st, err = Fold([]*Event{gen, addMal, rename}, chatID, keys)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Members[string(ownerPub)]; got.Name != "szef" {
		t.Fatalf("nazwa właściciela nadpisana: %q", got.Name)
	}
	if len(st.Members[string(ownerPub)].AddedBy) != 0 {
		t.Fatal("AddedBy właściciela nadpisane — kick atakującego kaskadowałby na właściciela")
	}

	// 3c: after all that, kicking the attacker must not remove the owner too
	kick2, _ := New(owner, chatID, KindRemove, Body{Pub: malPub}, 4, [][]byte{rename.ID()}, 1, key)
	st, err = Fold([]*Event{gen, addMal, rename, kick2}, chatID, keys)
	if err != nil {
		t.Fatal(err)
	}
	if !st.IsMember(ownerPub) {
		t.Fatal("właściciel wyleciał razem z wyrzucanym — czat zablokowany na stałe")
	}
	if st.IsMember(malPub) {
		t.Fatal("atakujący został członkiem mimo wyrzucenia")
	}
}

// One unreadable epoch degrades the view, it does not
// destroy the chat. Before the fix a single poisoned key made ChatState fail
// hard, and the conversation disappeared for good.
func TestFoldSurvivesPoisonedEpoch(t *testing.T) {
	ownerPub, owner, _ := ed25519.GenerateKey(nil)
	alicePub, alice, _ := ed25519.GenerateKey(nil)
	bobPub, _, _ := ed25519.GenerateKey(nil)
	key1, key2 := crypt.NewKey(), crypt.NewKey()

	gen, _ := New(owner, make([]byte, 32), KindCreate, Body{Name: "grupa", Text: "szef"}, 1, nil, 1, key1)
	chatID := gen.ID()
	addAlice, _ := New(owner, chatID, KindAdd, Body{Pub: alicePub, Name: "alice"}, 2, [][]byte{chatID}, 1, key1)
	rekey, _ := New(owner, chatID, KindRekey, Body{}, 3, [][]byte{addAlice.ID()}, 1, key1)
	// epoch 2: a membership event plus traffic we cannot read
	addBob, _ := New(owner, chatID, KindAdd, Body{Pub: bobPub, Name: "bob"}, 4, [][]byte{rekey.ID()}, 2, key2)
	msg, _ := New(alice, chatID, KindMsg, Body{Text: "tajne"}, 5, [][]byte{addBob.ID()}, 2, key2)

	evs := []*Event{gen, addAlice, rekey, addBob, msg}
	// victim holds a *wrong* key for epoch 2 (poisoned) — the worst case
	st, err := Fold(evs, chatID, map[uint32][]byte{1: key1, 2: crypt.NewKey()})
	if err != nil {
		t.Fatalf("zatruta epoka zabiła czat: %v", err)
	}
	if !st.IsMember(ownerPub) || !st.IsMember(alicePub) {
		t.Fatal("stan sprzed zatrutej epoki przepadł")
	}
	if !st.Degraded() || !st.MissingKey[2] {
		t.Fatal("zatruta epoka nie została odnotowana")
	}
	if st.IsMember(bobPub) {
		t.Fatal("nieodszyfrowane zdarzenie członkowskie zostało zastosowane")
	}
	// with the real key everything comes back
	st, err = Fold(evs, chatID, map[uint32][]byte{1: key1, 2: key2})
	if err != nil || !st.IsMember(bobPub) || st.Degraded() {
		t.Fatalf("prawdziwy klucz nie odtworzył stanu: %v", err)
	}
}

// KindCreate / KindRekey carry sha256 of the epoch key, so a
// receiver can tell the announced key from one a peer invented.
func TestKeyHashAnnouncedInDAG(t *testing.T) {
	_, owner, _ := ed25519.GenerateKey(nil)
	key1, key2 := crypt.NewKey(), crypt.NewKey()
	sum1, sum2 := sha256.Sum256(key1), sha256.Sum256(key2)

	gen, _ := New(owner, make([]byte, 32), KindCreate,
		Body{Name: "grupa", Text: "szef", Blob: sum1[:]}, 1, nil, 1, key1)
	chatID := gen.ID()
	rekey, _ := New(owner, chatID, KindRekey, Body{Blob: sum2[:]}, 2, [][]byte{chatID}, 1, key1)

	st, err := Fold([]*Event{gen, rekey}, chatID, map[uint32][]byte{1: key1})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(st.KeyHash[1], sum1[:]) || !bytes.Equal(st.KeyHash[2], sum2[:]) {
		t.Fatalf("skróty kluczy nie trafiły do stanu: %x %x", st.KeyHash[1], st.KeyHash[2])
	}
}

// Profile text from the network reaches Member.Status / Member.Bio, so
// the fold has to cleanse and cap it exactly like the profile index does.
func TestFoldSanitizesProfileText(t *testing.T) {
	ownerPub, owner, _ := ed25519.GenerateKey(nil)
	key := crypt.NewKey()
	keys := map[uint32][]byte{1: key}

	gen, _ := New(owner, make([]byte, 32), KindCreate, Body{Name: "grupa", Text: "szef"}, 1, nil, 1, key)
	chatID := gen.ID()
	card, _ := New(owner, chatID, KindProfile, Body{
		Text: "na\x1b[2Jrowerze",
		Bio:  strings.Repeat("y", MaxBio+50),
	}, 2, [][]byte{chatID}, 1, key)

	st, err := Fold([]*Event{gen, card}, chatID, keys)
	if err != nil {
		t.Fatal(err)
	}
	m := st.Members[string(ownerPub)]
	if strings.ContainsRune(m.Status, 0x1b) {
		t.Fatalf("sekwencja ANSI przeżyła w statusie: %q", m.Status)
	}
	if len(m.Bio) > MaxBio {
		t.Fatalf("opis %d znaków, limit %d", len(m.Bio), MaxBio)
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

// A member deletes only their own events, renames nothing, and a reaction
// cannot smuggle terminal escapes or a novel into the UI.
func TestMemberPowersAreLimited(t *testing.T) {
	_, owner, _ := ed25519.GenerateKey(nil)
	alicePub, alice, _ := ed25519.GenerateKey(nil)
	bobPub, bob, _ := ed25519.GenerateKey(nil)
	key := crypt.NewKey()
	keys := map[uint32][]byte{1: key}

	gen := mustNew(t, owner, make([]byte, 32), KindCreate, Body{Name: "grupa", Text: "szef"}, 1, nil, key)
	chatID := gen.ID()
	addA := mustNew(t, owner, chatID, KindAdd, Body{Pub: alicePub, Name: "alice"}, 2, [][]byte{chatID}, key)
	addB := mustNew(t, owner, chatID, KindAdd, Body{Pub: bobPub, Name: "bob"}, 3, [][]byte{addA.ID()}, key)
	msgA := mustNew(t, alice, chatID, KindMsg, Body{Text: "a"}, 4, [][]byte{addB.ID()}, key)
	msgO := mustNew(t, owner, chatID, KindMsg, Body{Text: "o"}, 5, [][]byte{msgA.ID()}, key)
	bobDel := mustNew(t, bob, chatID, KindDelete, Body{Target: msgA.ID()}, 6, [][]byte{msgO.ID()}, key)
	bobRename := mustNew(t, bob, chatID, KindRename, Body{Name: "przejęte"}, 7, [][]byte{bobDel.ID()}, key)
	evil := "\x1b]52;c;ZXZpbA==\x07" + strings.Repeat("x", 500)
	bobReact := mustNew(t, bob, chatID, KindReact, Body{Target: msgO.ID(), Text: evil}, 8, [][]byte{bobRename.ID()}, key)
	ownerDel := mustNew(t, owner, chatID, KindDelete, Body{Target: msgA.ID()}, 9, [][]byte{bobReact.ID()}, key)

	evs := []*Event{gen, addA, addB, msgA, msgO, bobDel, bobRename, bobReact}
	st, err := Fold(evs, chatID, keys)
	if err != nil {
		t.Fatal(err)
	}
	if st.Deleted[string(msgA.ID())] {
		t.Fatal("bob usunął wiadomość alice")
	}
	if st.Name != "grupa" {
		t.Fatalf("członek zmienił nazwę: %q", st.Name)
	}
	r := st.Reactions[string(msgO.ID())][string(bobPub)]
	if strings.ContainsAny(r, "\x1b\x07") || len(r) > MaxReact {
		t.Fatalf("reakcja nie wyczyszczona: %q", r)
	}
	st, err = Fold(append(evs, ownerDel), chatID, keys)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Deleted[string(msgA.ID())] {
		t.Fatal("właściciel nie może usunąć wiadomości")
	}
}

// A Folder extended one event at a time must match a full fold, and an event
// it rejects must leave it exactly as it was.
func TestFolderIncremental(t *testing.T) {
	_, owner, _ := ed25519.GenerateKey(nil)
	alicePub, alice, _ := ed25519.GenerateKey(nil)
	_, mallory, _ := ed25519.GenerateKey(nil)
	key := crypt.NewKey()
	keys := map[uint32][]byte{1: key}
	gen, _ := New(owner, make([]byte, 32), KindCreate, Body{Name: "grupa", Text: "szef"}, 1, nil, 1, key)
	chatID := gen.ID()
	add, _ := New(owner, chatID, KindAdd, Body{Pub: alicePub, Name: "alice"}, 2, [][]byte{chatID}, 1, key)
	msg, _ := New(alice, chatID, KindMsg, Body{Text: "hej"}, 3, [][]byte{add.ID()}, 1, key)
	react, _ := New(alice, chatID, KindReact, Body{Target: msg.ID(), Text: "+1"}, 4, [][]byte{msg.ID()}, 1, key)
	evil, _ := New(mallory, chatID, KindMsg, Body{Text: "spam"}, 5, [][]byte{react.ID()}, 1, key)
	del, _ := New(alice, chatID, KindDelete, Body{Target: msg.ID()}, 5, [][]byte{react.ID()}, 1, key)

	f := NewFolder(chatID, keys, nil, true)
	for _, e := range []*Event{gen, add, msg, react} {
		if !f.After(e) {
			t.Fatal("canonical successor reported as out of order")
		}
		if err := f.Apply(e); err != nil {
			t.Fatal(err)
		}
	}
	if f.After(add) {
		t.Fatal("older event reported as a successor")
	}
	if err := f.Apply(evil); err == nil {
		t.Fatal("non-member message accepted")
	}
	if err := f.Apply(del); err != nil {
		t.Fatalf("valid event after a rejected one: %v", err)
	}
	full, err := Fold([]*Event{gen, add, msg, react, del}, chatID, keys)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(full, f.St) {
		t.Fatalf("incremental %+v != full %+v", f.St, full)
	}
	if c := f.St.Clone(); !reflect.DeepEqual(c, f.St) {
		t.Fatal("Clone differs")
	}
}

// A member's unreadable profile event must not degrade the epoch: that used to
// park every later owner kick in it, so a kicked member stayed in.
func TestUnreadableProfileDoesNotDegrade(t *testing.T) {
	ownerPub, owner, _ := ed25519.GenerateKey(nil)
	malPub, mal, _ := ed25519.GenerateKey(nil)
	key1 := crypt.NewKey()
	gen, _ := New(owner, make([]byte, 32), KindCreate, Body{Name: "grupa", Text: "szef"}, 1, nil, 1, key1)
	chatID := gen.ID()
	addMal, _ := New(owner, chatID, KindAdd, Body{Pub: malPub, Name: "mal"}, 2, [][]byte{chatID}, 1, key1)
	junk, _ := New(mal, chatID, KindProfile, Body{Text: "x"}, 3, [][]byte{addMal.ID()}, 1, crypt.NewKey())
	kick, _ := New(owner, chatID, KindRemove, Body{Pub: malPub}, 4, [][]byte{junk.ID()}, 1, key1)
	st, err := Fold([]*Event{gen, addMal, junk, kick}, chatID, map[uint32][]byte{1: key1})
	if err != nil {
		t.Fatal(err)
	}
	if st.Degraded() || st.IsMember(malPub) || !st.IsMember(ownerPub) {
		t.Fatal("nieczytelny profil zablokował wyrzucenie")
	}
}

// A NoHistory add racing an owner rekey must not hand the newcomer the epoch
// that rekey opened: the owner wrote in it without knowing about them.
func TestJoinEpochRacingRekey(t *testing.T) {
	for range 16 { // both canonical orders of the two concurrent events
		_, owner, _ := ed25519.GenerateKey(nil)
		alicePub, alice, _ := ed25519.GenerateKey(nil)
		xPub, _, _ := ed25519.GenerateKey(nil)
		k := map[uint32][]byte{1: crypt.NewKey(), 2: crypt.NewKey(), 3: crypt.NewKey(), 4: crypt.NewKey()}
		gen, _ := New(owner, make([]byte, 32), KindCreate, Body{Name: "g", Text: "szef", NoHist: true}, 1, nil, 1, k[1])
		chat := gen.ID()
		addA, _ := New(owner, chat, KindAdd, Body{Pub: alicePub, Name: "a"}, 2, [][]byte{chat}, 1, k[1])
		rk1, _ := New(owner, chat, KindRekey, Body{}, 3, [][]byte{addA.ID()}, 1, k[1])
		addX, _ := New(alice, chat, KindAdd, Body{Pub: xPub, Name: "x"}, 4, [][]byte{rk1.ID()}, 2, k[2])
		rk2, _ := New(owner, chat, KindRekey, Body{}, 4, [][]byte{rk1.ID()}, 2, k[2])
		st, err := Fold([]*Event{gen, addA, rk1, addX, rk2}, chat, k)
		if err != nil {
			t.Fatal(err)
		}
		if st.Members[string(alicePub)].JoinEpoch != 2 {
			t.Fatalf("zwykłe dołączenie: JoinEpoch %d", st.Members[string(alicePub)].JoinEpoch)
		}
		if j := st.Members[string(xPub)].JoinEpoch; j != 4 {
			t.Fatalf("nowy dostał epokę %d otwartą bez wiedzy o nim", j)
		}
		// the rotation made after the add opens it, and the join stays there
		rk3, _ := New(owner, chat, KindRekey, Body{}, 5, [][]byte{addX.ID(), rk2.ID()}, 3, k[3])
		st, err = Fold([]*Event{gen, addA, rk1, addX, rk2, rk3}, chat, k)
		if err != nil || st.Members[string(xPub)].JoinEpoch != 4 || st.Epoch != 4 {
			t.Fatalf("rotacja po dodaniu: %v %d", err, st.Members[string(xPub)].JoinEpoch)
		}
	}
}

// The group's look belongs to the owner: a member's server event is ignored.
func TestServerLookOwnerOnly(t *testing.T) {
	_, owner, _ := ed25519.GenerateKey(nil)
	bobPub, bob, _ := ed25519.GenerateKey(nil)
	key := crypt.NewKey()
	keys := map[uint32][]byte{1: key}
	icon, banner := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)

	gen := mustNew(t, owner, make([]byte, 32), KindCreate, Body{Name: "grupa", Text: "szef"}, 1, nil, key)
	chatID := gen.ID()
	add := mustNew(t, owner, chatID, KindAdd, Body{Pub: bobPub, Name: "bob"}, 2, [][]byte{chatID}, key)
	look := mustNew(t, owner, chatID, KindServer, Body{Blob: icon, Mime: "image/png", Prev: banner, PrevMime: "image/png", Style: "0.0.3.5.2.0.0<x>"}, 3, [][]byte{add.ID()}, key)
	hijack := mustNew(t, bob, chatID, KindServer, Body{Style: "0.0.0.0.9.0.0"}, 4, [][]byte{look.ID()}, key)

	st, err := Fold([]*Event{gen, add, look, hijack}, chatID, keys)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(st.Icon, icon) || !bytes.Equal(st.Banner, banner) || st.Style != "0.0.3.5.2.0.0x" {
		t.Fatalf("wygląd serwera: icon %x banner %x style %q", st.Icon, st.Banner, st.Style)
	}
}
