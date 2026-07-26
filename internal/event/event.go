// Package event: the chat DAG. Every chat is a hash-linked, signed,
// encrypted event log replicated on every member's machine ("blockchain-like").
// Chat ID = ID of its genesis event. Bodies are AEAD-encrypted with the
// chat key of the event's epoch; signatures cover the ciphertext, so events
// verify without keys and decrypt only for members.
package event

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"szept/internal/crypt"
)

// Body kinds.
const (
	KindCreate = "create" // genesis: {Name, AuthorName}
	KindMsg    = "msg"    // {Text}
	KindAdd    = "add"    // {Pub, Name} new member added by existing member
	KindRemove = "remove" // {Pub} removed by owner
	KindRekey  = "rekey"  // {} bumps epoch; new key travels out-of-band (E2E ctrl)
	KindRename = "rename" // {Name}
	KindFile   = "file"   // {Blob, Mime, Name, Size, W, H, Text=podpis} attachment
	KindAvatar = "avatar" // {Blob, Mime} author's avatar; empty Blob clears it
	KindNick   = "nick"   // {Name} author renames themselves in this chat
)

// QuietKinds: zdarzenia profilowe. Nie są treścią rozmowy — nie rysujemy ich
// i nie nabijają odznaki nieprzeczytanych.
func QuietKind(kind string) bool { return kind == KindAvatar || kind == KindNick }

type Event struct {
	Chat    []byte   `json:"chat"` // genesis ID; zeros for the genesis event itself
	Kind    string   `json:"kind"`
	Author  []byte   `json:"author"` // ed25519 pub
	Lamport uint64   `json:"lamport"`
	Parents [][]byte `json:"parents"` // IDs of DAG heads at creation time
	Epoch   uint32   `json:"epoch"`   // key epoch used for Box
	Box     []byte   `json:"box"`     // crypt.Seal(chatKey[Epoch], bodyJSON, AD=signable)
	Sig     []byte   `json:"sig"`     // ed25519 over signable bytes + Box
}

type Body struct {
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"`
	Pub  []byte `json:"pub,omitempty"`
	TS   int64  `json:"ts,omitempty"` // author's clock, display only (inside AEAD)

	// attachments (KindFile / KindAvatar): only the sha256 of the content
	// travels in the DAG; the bytes are fetched on demand from peers.
	Blob []byte `json:"blob,omitempty"`
	Mime string `json:"mime,omitempty"`
	Size int64  `json:"size,omitempty"`
	W    int    `json:"w,omitempty"`
	H    int    `json:"h,omitempty"`

	// Duże pliki (> attach.PreviewMax) replikują się tylko jako podgląd: Prev to
	// sha256 miniatury, którą wszyscy ściągają od razu. Pełne bajty (Blob) leżą
	// u nadawcy i wędrują dopiero, gdy ktoś naciśnie „pobierz”.
	Prev     []byte `json:"prev,omitempty"`
	PrevMime string `json:"pmime,omitempty"`
}

// signable: deterministic length-prefixed concat of all fields except Sig.
func (e *Event) signable() []byte {
	var b bytes.Buffer
	put := func(p []byte) {
		var l [4]byte
		binary.BigEndian.PutUint32(l[:], uint32(len(p)))
		b.Write(l[:])
		b.Write(p)
	}
	put(e.Chat)
	put([]byte(e.Kind))
	put(e.Author)
	var lam [8]byte
	binary.BigEndian.PutUint64(lam[:], e.Lamport)
	b.Write(lam[:])
	var ep [4]byte
	binary.BigEndian.PutUint32(ep[:], e.Epoch)
	b.Write(ep[:])
	for _, p := range e.Parents {
		put(p)
	}
	put(e.Box)
	return b.Bytes()
}

func (e *Event) ID() []byte {
	h := sha256.Sum256(append(e.signable(), e.Sig...))
	return h[:]
}

// New builds, encrypts and signs an event.
func New(priv ed25519.PrivateKey, chat []byte, kind string, body Body, lamport uint64, parents [][]byte, epoch uint32, key []byte) (*Event, error) {
	bj, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	e := &Event{
		Chat: chat, Kind: kind,
		Author:  priv.Public().(ed25519.PublicKey),
		Lamport: lamport, Parents: parents, Epoch: epoch,
	}
	// AD binds context minus Box itself.
	e.Box, err = crypt.Seal(key, bj, e.adBytes())
	if err != nil {
		return nil, err
	}
	e.Sig = ed25519.Sign(priv, e.signable())
	return e, nil
}

// adBytes: signable() with empty Box — used as AEAD associated data.
func (e *Event) adBytes() []byte {
	saved := e.Box
	e.Box = nil
	ad := e.signable()
	e.Box = saved
	return ad
}

func (e *Event) VerifySig() bool {
	if len(e.Author) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(e.Author), e.signable(), e.Sig)
}

func (e *Event) Decrypt(key []byte) (Body, error) {
	var b Body
	pt, err := crypt.Open(key, e.Box, e.adBytes())
	if err != nil {
		return b, err
	}
	err = json.Unmarshal(pt, &b)
	return b, err
}

func Marshal(e *Event) []byte { b, _ := json.Marshal(e); return b }
func Unmarshal(b []byte) (*Event, error) {
	var e Event
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// ---- membership fold ----

type Member struct {
	Pub        []byte
	Name       string
	Removed    bool
	AddedBy    []byte // kto go wpuścił (nil dla założyciela) — usunięcie kaskaduje
	Avatar     []byte // sha256 of the avatar blob ("" = default initials)
	AvatarMime string
}

type State struct {
	Name    string
	Owner   []byte
	Epoch   uint32
	Members map[string]*Member // key: hex-free string(pub)
}

func (s *State) IsMember(pub []byte) bool {
	m, ok := s.Members[string(pub)]
	return ok && !m.Removed
}

// Sort events into canonical apply order: (Lamport, ID).
func SortCanonical(evs []*Event) {
	sort.Slice(evs, func(i, j int) bool {
		if evs[i].Lamport != evs[j].Lamport {
			return evs[i].Lamport < evs[j].Lamport
		}
		return bytes.Compare(evs[i].ID(), evs[j].ID()) < 0
	})
}

// Fold replays events (any order) into chat state, enforcing authorization:
// genesis by owner, adds by members, removes/rekeys by owner, msgs by members.
// keys maps epoch -> chat key; events whose epoch key is missing still count
// for membership only if decryptable — so Fold needs at least the epochs
// covering membership events. Returns error on an unauthorized or bad event.
func Fold(evs []*Event, chatID []byte, keys map[uint32][]byte) (*State, error) {
	return fold(evs, chatID, keys, true)
}

// FoldTrusted pomija weryfikację podpisów. Wolno jej użyć wyłącznie na
// zdarzeniach, które przeszły już przez ingest (czyli na tym, co leży w naszej
// bazie) — sprawdzanie Ed25519 to ~90% kosztu foldu, a robimy go często.
func FoldTrusted(evs []*Event, chatID []byte, keys map[uint32][]byte) (*State, error) {
	return fold(evs, chatID, keys, false)
}

func fold(evs []*Event, chatID []byte, keys map[uint32][]byte, verify bool) (*State, error) {
	SortCanonical(evs)
	st := &State{Members: map[string]*Member{}}
	// lamport rodziców: porządek stosowania ma wynikać z DAG-u, nie z licznika
	seen := make(map[string]uint64, len(evs))
	for _, e := range evs {
		if verify && !e.VerifySig() {
			return nil, fmt.Errorf("event %x: bad signature", e.ID()[:6])
		}
		isGenesis := len(st.Owner) == 0
		if isGenesis {
			if e.Kind != KindCreate || !bytes.Equal(e.ID(), chatID) {
				return nil, errors.New("first event is not the expected genesis")
			}
		} else if !bytes.Equal(e.Chat, chatID) {
			return nil, errors.New("event from another chat")
		}
		// Lamport = max(rodzice)+1, dokładnie. Bez tego autor sam wybiera, w
		// którym miejscu historii wyląduje jego zdarzenie: antydatowane "add"
		// wchodzi w stan sprzed czyjegoś usunięcia, a Lamport = 2^64-1 przewija
		// licznik i blokuje wszystkim dopisywanie.
		if err := checkLamport(e, seen, isGenesis); err != nil {
			return nil, err
		}
		seen[string(e.ID())] = e.Lamport
		key, ok := keys[e.Epoch]
		var body Body
		if ok {
			var err error
			if body, err = e.Decrypt(key); err != nil {
				// zły klucz epoki (np. podrzucony przez peera) nie może zabić
				// całego czatu — traktujemy go jak brak klucza
				ok = false
			}
		}
		if !ok {
			// Can't decrypt: tolerate only content kinds; membership must be readable.
			switch e.Kind {
			case KindMsg, KindRename, KindFile, KindAvatar, KindNick:
				continue
			}
			return nil, fmt.Errorf("missing key for epoch %d (kind %s)", e.Epoch, e.Kind)
		}
		switch e.Kind {
		case KindCreate:
			// genesis jest dokładnie jeden: drugi "create" nadpisywałby
			// właściciela, czyli każdy członek przejmowałby czat
			if !isGenesis {
				return nil, errors.New("second genesis event")
			}
			// body.Name = chat name, body.Text = creator's display name
			st.Owner = e.Author
			st.Name = body.Name
			st.Epoch = 1
			st.Members[string(e.Author)] = &Member{Pub: e.Author, Name: body.Text}
		case KindAdd:
			if !st.IsMember(e.Author) {
				return nil, errors.New("add by non-member")
			}
			if len(body.Pub) == ed25519.PublicKeySize {
				st.Members[string(body.Pub)] = &Member{Pub: body.Pub, Name: body.Name, AddedBy: e.Author}
			}
		case KindRemove:
			if !bytes.Equal(e.Author, st.Owner) {
				return nil, errors.New("remove by non-owner")
			}
			removeTree(st, body.Pub)
		case KindRekey:
			if !bytes.Equal(e.Author, st.Owner) {
				return nil, errors.New("rekey by non-owner")
			}
			if e.Epoch+1 > st.Epoch {
				st.Epoch = e.Epoch + 1
			}
		case KindRename:
			if st.IsMember(e.Author) {
				st.Name = body.Name
			}
		case KindMsg, KindFile:
			if !st.IsMember(e.Author) {
				return nil, errors.New("msg by non-member")
			}
		case KindAvatar:
			// only your own avatar, and only while you are a member
			if m, ok := st.Members[string(e.Author)]; ok && !m.Removed {
				m.Avatar, m.AvatarMime = body.Blob, body.Mime
			}
		case KindNick:
			// your own display name, nobody else's
			if m, ok := st.Members[string(e.Author)]; ok && !m.Removed && body.Name != "" {
				m.Name = body.Name
			}
		}
	}
	if len(st.Owner) == 0 {
		return nil, errors.New("no genesis event")
	}
	return st, nil
}

// checkLamport wymusza Lamport = max(rodzice)+1 i obecność rodziców przed
// dzieckiem. Uczciwy klient zawsze tak liczy (heads + 1), więc to nie zmienia
// niczego dla poprawnej historii — odcina za to wybieranie sobie miejsca
// w porządku foldu.
func checkLamport(e *Event, seen map[string]uint64, isGenesis bool) error {
	if isGenesis {
		return nil
	}
	if len(e.Parents) == 0 {
		return fmt.Errorf("event %x: no parents", e.ID()[:6])
	}
	var max uint64
	for _, p := range e.Parents {
		lam, ok := seen[string(p)]
		if !ok {
			return fmt.Errorf("event %x: parent %x not applied before child", e.ID()[:6], p[:4])
		}
		if lam > max {
			max = lam
		}
	}
	if e.Lamport != max+1 {
		return fmt.Errorf("event %x: lamport %d, expected %d", e.ID()[:6], e.Lamport, max+1)
	}
	return nil
}

// removeTree: usunięcie kaskaduje na tych, których usuwany wpuścił. Bez tego
// wystarczy dodać do czatu drugą swoją tożsamość, żeby przeżyć wyrzucenie —
// zostaje członkiem, więc dostaje wszystkie klucze epok, także te po rotacji.
func removeTree(st *State, pub []byte) {
	m, ok := st.Members[string(pub)]
	if !ok || m.Removed {
		return
	}
	m.Removed = true
	for _, other := range st.Members {
		if bytes.Equal(other.AddedBy, pub) {
			removeTree(st, other.Pub)
		}
	}
}
