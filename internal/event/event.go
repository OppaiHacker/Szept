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
	"maps"
	"slices"
	"sort"
	"strings"

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
	KindFile   = "file"   // {Blob, Mime, Name, Size, W, H, Text=caption} attachment
	KindAvatar = "avatar" // {Blob, Mime} author's avatar; empty Blob clears it
	KindNick   = "nick"   // {Name} author renames themselves in this chat
	KindReact  = "react"  // {Target, Text=emoji} reaction to an event
	KindDelete = "delete" // {Target} deletes/revokes an event
	KindPin    = "pin"    // {Target, Arch=unpin} pins a message for everyone
	// KindProfile: {Text=status, Bio, Blob/Mime=banner} author's own profile card.
	// Empty field clears it; all three travel together, so one event carries
	// the whole card.
	KindProfile = "profile"
	// KindChannel: {Target=channel ID, Name, Text=text|voice|cat, Ch=parent
	// category, Arch=archived}. Owner only; an empty Name deletes the channel
	// for good. An empty Target names the default channel of that kind.
	KindChannel = "channel"
	// KindServer: {Blob/Mime=icon, Prev/PrevMime=banner, Style=decoration}
	// the group's look. Owner only; the whole look travels in one event, an
	// empty field clears that part.
	KindServer = "server"
	// KindHistory: {NoHist} whether members added from now on get the content
	// keys from before they joined. Owner only; overrides KindCreate's NoHist.
	KindHistory = "history"
	// KindLeave: {} the author leaves the chat. Anyone but the owner; unlike a
	// remove it takes no one else along (the people they added stay).
	KindLeave = "leave"
)

// MembershipKind: events that decide who belongs to the chat. The fold can only
// judge authorization once these are readable, so a caller holding no key for
// one of them should wait for the key rather than store it.
func MembershipKind(kind string) bool {
	return kind == KindCreate || kind == KindAdd || kind == KindRemove || kind == KindRekey || kind == KindLeave
}

// MetaKind: events sealed with the epoch's member key (MemberKey) instead of
// the content key: who is in the chat and what they call themselves. A member
// who joined a chat without history gets member keys for every epoch, so they
// can fold membership from genesis, and content keys only from their join on.
func MetaKind(kind string) bool {
	switch kind {
	case KindCreate, KindAdd, KindRemove, KindRekey, KindRename, KindNick, KindAvatar, KindProfile, KindChannel, KindServer, KindHistory, KindLeave:
		return true
	}
	return false
}

// MemberKey derives the member key from an epoch's content key. One-way: the
// member key does not reveal the content key.
func MemberKey(contentKey []byte) []byte { return crypt.Derive(contentKey, "szept-member-v1") }

// QuietKinds: profile and modifying events. Not separate conversation items —
// we don't render them as separate lines and they don't increment unread badges.
func QuietKind(kind string) bool {
	return kind == KindAvatar || kind == KindNick || kind == KindReact ||
		kind == KindDelete || kind == KindProfile || kind == KindChannel || kind == KindPin || kind == KindServer
}

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
	Text   string `json:"text,omitempty"`
	Name   string `json:"name,omitempty"`
	Pub    []byte `json:"pub,omitempty"`
	TS     int64  `json:"ts,omitempty"`     // author's clock, display only (inside AEAD)
	NS     int64  `json:"ns,omitempty"`     // KindAvatar / KindProfile: author's clock in ns, newest card wins
	Target []byte `json:"target,omitempty"` // ID of target event (KindReact / KindDelete / KindPin; KindMsg: the message replied to)
	Bio    string `json:"bio,omitempty"`    // KindProfile: description under the name
	Style  string `json:"style,omitempty"`  // KindProfile: name, avatar and card effects (see SafeStyle)

	// attachments (KindFile / KindAvatar): only the sha256 of the content
	// travels in the DAG; the bytes are fetched on demand from peers.
	Blob []byte `json:"blob,omitempty"`
	Mime string `json:"mime,omitempty"`
	Size int64  `json:"size,omitempty"`
	W    int    `json:"w,omitempty"`
	H    int    `json:"h,omitempty"`

	// Large files (> attach.PreviewMax) replicate as thumbnail only: Prev is
	// sha256 of thumbnail, fetched by everyone immediately. Full bytes (Blob) reside
	// at sender and travel only when recipient taps "download".
	Prev     []byte `json:"prev,omitempty"`
	PrevMime string `json:"pmime,omitempty"`

	// KindCreate / KindRekey: sha256 of the next epoch's member key (Blob holds
	// the content key's), so a member key alone can be checked too.
	MHash []byte `json:"mhash,omitempty"`
	// NoHist (KindCreate): members added later get no content keys from
	// before they joined.
	NoHist bool `json:"nohist,omitempty"`
	// Ch: the channel a KindMsg / KindFile belongs to (empty = the default
	// one), or the parent category of a KindChannel.
	Ch []byte `json:"ch,omitempty"`
	// Arch (KindChannel): the channel sits in the owner's archive.
	Arch bool `json:"arch,omitempty"`
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
	if MetaKind(kind) {
		key = MemberKey(key)
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

// Decrypt opens the body with the epoch's content key (the member key is
// derived from it for MetaKind events).
func (e *Event) Decrypt(key []byte) (Body, error) {
	if MetaKind(e.Kind) {
		key = MemberKey(key)
	}
	return e.open(key)
}

// DecryptMeta opens a MetaKind event with the epoch's member key alone.
func (e *Event) DecryptMeta(mkey []byte) (Body, error) {
	if !MetaKind(e.Kind) {
		return Body{}, errors.New("event: not a member-key event")
	}
	return e.open(mkey)
}

func (e *Event) open(key []byte) (Body, error) {
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

// ---- text arriving from the network ----

const (
	// MaxName: ceiling on a network-provided name (a peer sends arbitrary bytes).
	MaxName = 64
	// MaxBio: ceiling on the longer profile description.
	MaxBio = 400
	// MaxReact: a reaction is one emoji (ZWJ sequences run ~25 bytes).
	MaxReact = 32
)

// SafeText cleanses incoming network text before it is rendered: control
// characters are stripped because the TUI writes text straight to the terminal,
// where ANSI/OSC sequences could redraw the screen, spoof messages or touch the
// clipboard. Newlines and tabs remain.
func SafeText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
}

// SafeName: single-line name (author, chat, friend) with capped length.
func SafeName(s string) string {
	s = strings.ReplaceAll(SafeText(s), "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	if len(s) > MaxName {
		s = strings.ToValidUTF8(s[:MaxName], "")
	}
	return s
}

// SafeBio: multi-line profile description — newlines survive, length capped.
func SafeBio(s string) string {
	s = SafeText(s)
	if len(s) > MaxBio {
		s = strings.ToValidUTF8(s[:MaxBio], "")
	}
	return s
}

// SafeStyle: the profile-effects string is a short list of preset names and
// palette indices ("font.effect.hue.hue.deco.frame.fx"). Anything outside
// [a-z0-9.] is dropped; clients render only names they know.
func SafeStyle(s string) string {
	s = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' {
			return r
		}
		return -1
	}, s)
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// ---- membership fold ----

type Member struct {
	Pub        []byte
	Name       string
	Removed    bool
	AddedBy    []byte // who added this member (nil for genesis owner) — removal cascades
	Avatar     []byte // sha256 of the avatar blob ("" = default initials)
	AvatarMime string
	Status     string // short "what am I up to" line
	Bio        string // longer description shown on the profile
	Style      string // profile effects, already passed through SafeStyle
	Banner     []byte // sha256 of the profile banner blob
	BannerMime string
	// JoinEpoch: first epoch whose content key this member gets. 1 unless the
	// chat has NoHistory, where it is the epoch after the one they were added in.
	JoinEpoch uint32
	addID     []byte // NoHistory: the add that set JoinEpoch, until a rekey that saw it opens that epoch
}

type State struct {
	Name      string
	Owner     []byte
	Epoch     uint32
	Members   map[string]*Member           // key: hex-free string(pub)
	Reactions map[string]map[string]string // key: string(targetID) -> pub -> emoji
	Deleted   map[string]bool              // key: string(targetID) -> true
	Pinned    map[string]bool              // key: string(targetID) -> true

	// KeyHash: epoch -> sha256 of the key that epoch is *supposed* to use, as
	// announced by the signed KindCreate / KindRekey event. Lets the key handler
	// reject a key that does not match what the DAG promised. Absent for chats
	// created before this field existed — see handleKeys for the legacy path.
	KeyHash  map[uint32][]byte
	MKeyHash map[uint32][]byte // the same for member keys

	// NoHistory: set at creation, changed by the owner with KindHistory;
	// members added while it holds do not get older content keys, and the
	// owner rotates the key after every add.
	NoHistory bool

	// MissingKey: epochs we hold no usable member key for. Membership events in
	// those epochs were skipped rather than aborting the fold — one bad epoch
	// key must degrade the view of the chat, never destroy the chat. A missing
	// content key alone does not count: membership is still complete.
	MissingKey map[uint32]bool

	// Channels: text/voice channels and categories, in order of creation.
	// Genesis seeds the defaults "czat" and "Rozmowa" with an empty ID: their
	// messages carry no Ch and their call key is the bare chat ID.
	Channels []Channel

	// the group's look (KindServer): sha256 of icon and banner blobs, and
	// the decoration string, already passed through SafeStyle
	Icon, Banner         []byte
	IconMime, BannerMime string
	Style                string
}

type Channel struct {
	ID   []byte
	Name string
	Kind string // text | voice | cat
	Cat  []byte // parent category, nil = default
	Arch bool   // archived: hidden from members, the owner can restore it
}

// Clone copies the maps a later Apply could change. Byte slices are never
// mutated in place, so they are shared.
func (s *State) Clone() *State {
	c := *s
	c.Members = make(map[string]*Member, len(s.Members))
	for k, m := range s.Members {
		mm := *m
		c.Members[k] = &mm
	}
	c.Reactions = make(map[string]map[string]string, len(s.Reactions))
	for k, r := range s.Reactions {
		c.Reactions[k] = maps.Clone(r)
	}
	c.Deleted = maps.Clone(s.Deleted)
	c.Pinned = maps.Clone(s.Pinned)
	c.KeyHash = maps.Clone(s.KeyHash)
	c.MKeyHash = maps.Clone(s.MKeyHash)
	c.MissingKey = maps.Clone(s.MissingKey)
	c.Channels = slices.Clone(s.Channels)
	return &c
}

func (s *State) IsMember(pub []byte) bool {
	m, ok := s.Members[string(pub)]
	return ok && !m.Removed
}

// Degraded reports whether part of the history was unreadable, so membership
// here is incomplete and must not be used to reject events.
func (s *State) Degraded() bool { return len(s.MissingKey) > 0 }

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
	return fold(evs, chatID, keys, nil, true)
}

// FoldTrusted skips signature verification. May only be used on
// events already validated via ingest (i.e. stored in database) —
// Ed25519 checks account for ~90% of fold cost and fold runs frequently.
func FoldTrusted(evs []*Event, chatID []byte, keys map[uint32][]byte) (*State, error) {
	return fold(evs, chatID, keys, nil, false)
}

func fold(evs []*Event, chatID []byte, keys, mkeys map[uint32][]byte, verify bool) (*State, error) {
	SortCanonical(evs)
	f := NewFolder(chatID, keys, mkeys, verify)
	for _, e := range evs {
		if err := f.Apply(e); err != nil {
			return nil, err
		}
	}
	if len(f.St.Owner) == 0 {
		return nil, errors.New("no genesis event")
	}
	return f.St, nil
}

// Folder is a fold in progress. Apply takes events in canonical order, so a
// caller holding a Folder can extend it with an event that sorts after the
// last one instead of replaying the whole history. After an error the Folder
// is unchanged, so a rejected event does not cost a full replay.
type Folder struct {
	St     *State
	chatID []byte
	keys   map[uint32][]byte // epoch -> content key
	mkeys  map[uint32][]byte // epoch -> member key, for epochs without a content key
	verify bool
	// parent Lamport: apply order derives from DAG, not internal counter
	seen    map[string]uint64
	authors map[string][]byte   // event id -> author, for KindDelete
	parents map[string][][]byte // NoHistory only: event id -> parents, for descends
	last    *Event
}

func NewFolder(chatID []byte, keys, mkeys map[uint32][]byte, verify bool) *Folder {
	return &Folder{
		St: &State{
			Members:    map[string]*Member{},
			Reactions:  map[string]map[string]string{},
			Deleted:    map[string]bool{},
			Pinned:     map[string]bool{},
			KeyHash:    map[uint32][]byte{},
			MKeyHash:   map[uint32][]byte{},
			MissingKey: map[uint32]bool{},
		},
		chatID: chatID, keys: keys, mkeys: mkeys, verify: verify,
		seen: map[string]uint64{}, authors: map[string][]byte{}, parents: map[string][][]byte{},
	}
}

// After reports whether e sorts after every event applied so far, i.e.
// whether Apply(e) gives the same state as a full fold with e included.
func (f *Folder) After(e *Event) bool {
	if f.last == nil {
		return true
	}
	if e.Lamport != f.last.Lamport {
		return e.Lamport > f.last.Lamport
	}
	return bytes.Compare(e.ID(), f.last.ID()) > 0
}

// SameKeys: the Folder decrypted with exactly these epoch keys.
func (f *Folder) SameKeys(keys, mkeys map[uint32][]byte) bool {
	return sameKeys(keys, f.keys) && sameKeys(mkeys, f.mkeys)
}

func sameKeys(a, b map[uint32][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for ep, k := range a {
		if !bytes.Equal(k, b[ep]) {
			return false
		}
	}
	return true
}

// Open decrypts e with the content key of its epoch, or for MetaKind events
// with a member key held on its own.
func (e *Event) Open(keys, mkeys map[uint32][]byte) (Body, bool) {
	if k, ok := keys[e.Epoch]; ok {
		if b, err := e.Decrypt(k); err == nil {
			return b, true
		}
	}
	if mk, ok := mkeys[e.Epoch]; ok && MetaKind(e.Kind) {
		if b, err := e.DecryptMeta(mk); err == nil {
			return b, true
		}
	}
	return Body{}, false
}

// Apply folds one more event into St. Every rejection happens before St is
// touched, so undoing the bookkeeping below restores the Folder exactly.
func (f *Folder) Apply(e *Event) error {
	prev := f.last
	if err := f.apply(e); err != nil {
		f.last = prev
		delete(f.seen, string(e.ID()))
		delete(f.authors, string(e.ID()))
		delete(f.parents, string(e.ID()))
		return err
	}
	return nil
}

func (f *Folder) apply(e *Event) error {
	st, chatID, verify, seen, authors := f.St, f.chatID, f.verify, f.seen, f.authors
	f.last = e
	if verify && !e.VerifySig() {
		return fmt.Errorf("event %x: bad signature", e.ID()[:6])
	}
	isGenesis := len(st.Owner) == 0
	if isGenesis {
		if e.Kind != KindCreate || !bytes.Equal(e.ID(), chatID) {
			return errors.New("first event is not the expected genesis")
		}
	} else if !bytes.Equal(e.Chat, chatID) {
		return errors.New("event from another chat")
	}
	// Lamport = max(parents)+1, strictly. Without this an author selects
	// where in history their event lands: backdated "add"
	// enters state prior to someone's kick, or Lamport = 2^64-1 rolls counter
	// and blocks everyone from appending.
	if err := checkLamport(e, seen, isGenesis); err != nil {
		return err
	}
	seen[string(e.ID())] = e.Lamport
	authors[string(e.ID())] = e.Author
	if st.NoHistory {
		f.parents[string(e.ID())] = e.Parents
	}
	// a bad epoch key (e.g. spoofed by a peer) counts as a missing one
	body, ok := e.Open(f.keys, f.mkeys)
	if !ok {
		// Can't decrypt. Genesis is fatal — without it there is no chat at all.
		// A membership event is skipped and its epoch flagged: a single bad or
		// not-yet-delivered epoch key used to abort the whole fold, which made
		// key poisoning a permanent chat lockout instead of a gap in the view.
		// Anything else only needs its author checked, which works without the
		// body. Only membership kinds flag the epoch: a member's unreadable
		// profile or nick used to mark it degraded and so park every later
		// owner kick in that epoch.
		if isGenesis {
			return fmt.Errorf("missing key for epoch %d (kind %s)", e.Epoch, e.Kind)
		}
		if MembershipKind(e.Kind) {
			st.MissingKey[e.Epoch] = true
		} else if !st.IsMember(e.Author) && !st.Degraded() {
			return fmt.Errorf("%s by non-member", e.Kind)
		}
		return nil
	}
	switch e.Kind {
	case KindCreate:
		// genesis is strictly single: second "create" would overwrite
		// owner, meaning any member could hijack chat
		if !isGenesis {
			return errors.New("second genesis event")
		}
		// body.Name = chat name, body.Text = creator's display name
		st.Owner = e.Author
		st.Name = SafeName(body.Name)
		st.Epoch = 1
		st.NoHistory = body.NoHist
		st.Members[string(e.Author)] = &Member{Pub: e.Author, Name: SafeName(body.Text), JoinEpoch: 1}
		st.Channels = []Channel{{Name: "czat", Kind: "text"}, {Name: "Rozmowa", Kind: "voice"}}
		if len(body.Blob) == sha256.Size {
			st.KeyHash[1] = body.Blob
		}
		if len(body.MHash) == sha256.Size {
			st.MKeyHash[1] = body.MHash
		}
	case KindAdd:
		if !st.IsMember(e.Author) {
			if st.Degraded() {
				return nil // membership view has holes; cannot judge the author
			}
			return errors.New("add by non-member")
		}
		if len(body.Pub) != ed25519.PublicKeySize {
			break
		}
		// "add" adds; it never rewrites an existing entry. Overwriting let any
		// member resurrect someone the owner kicked, rename the owner in signed
		// state, and — via AddedBy — make the owner's own kick cascade onto
		// themselves, locking the chat for everyone. Re-admitting a removed
		// member is the owner's call alone.
		// without history a newcomer starts at the next epoch, which the owner
		// opens right after the add (see core.autoRekey)
		join := uint32(1)
		if st.NoHistory {
			join = st.Epoch + 1
		}
		if prev, exists := st.Members[string(body.Pub)]; exists {
			if prev.Removed && bytes.Equal(e.Author, st.Owner) {
				prev.Removed, prev.AddedBy = false, nil
				prev.JoinEpoch = max(prev.JoinEpoch, join)
				if st.NoHistory {
					prev.addID = e.ID()
				}
			}
			break
		}
		m := &Member{Pub: body.Pub, Name: SafeName(body.Name), AddedBy: e.Author, JoinEpoch: join}
		if st.NoHistory {
			m.addID = e.ID()
		}
		st.Members[string(body.Pub)] = m
	case KindLeave:
		if bytes.Equal(e.Author, st.Owner) {
			return errors.New("owner cannot leave")
		}
		if m := st.Members[string(e.Author)]; m != nil {
			m.Removed = true
		}
	case KindRemove:
		if !bytes.Equal(e.Author, st.Owner) {
			return errors.New("remove by non-owner")
		}
		removeTree(st, body.Pub)
	case KindRekey:
		if !bytes.Equal(e.Author, st.Owner) {
			return errors.New("rekey by non-owner")
		}
		if e.Epoch+1 > st.Epoch {
			st.Epoch = e.Epoch + 1
		}
		// A rekey that opens a newcomer's JoinEpoch without having seen their
		// add raced it: the owner may already have written in that epoch for
		// people who did not know about them. Push the join one epoch on, so
		// they start at the rotation made after the add (core.autoRekey).
		for _, m := range st.Members {
			if m.addID == nil || m.Removed || m.JoinEpoch != e.Epoch+1 {
				continue
			}
			if f.descends(e, m.addID) {
				m.addID = nil
			} else {
				m.JoinEpoch++
			}
		}
		// binds the next epoch's key to signed history (see State.KeyHash)
		if len(body.Blob) == sha256.Size {
			st.KeyHash[e.Epoch+1] = body.Blob
		}
		if len(body.MHash) == sha256.Size {
			st.MKeyHash[e.Epoch+1] = body.MHash
		}
	case KindRename:
		// owner only; a member's rename is ignored, not fatal — old
		// histories may already carry one
		if bytes.Equal(e.Author, st.Owner) {
			st.Name = SafeName(body.Name)
		}
	case KindMsg, KindFile:
		if !st.IsMember(e.Author) {
			if st.Degraded() {
				return nil
			}
			return errors.New("msg by non-member")
		}
	case KindChannel:
		// owner only, ignored otherwise like a member's rename
		k := body.Text
		if !bytes.Equal(e.Author, st.Owner) || len(body.Target) > 16 ||
			(k != "text" && k != "voice" && k != "cat") || (len(body.Target) == 0 && k == "cat") {
			break
		}
		// both defaults have an empty ID: the kind tells them apart
		i := slices.IndexFunc(st.Channels, func(c Channel) bool {
			return bytes.Equal(c.ID, body.Target) && (len(body.Target) > 0 || c.Kind == k)
		})
		switch {
		case body.Name == "" && i >= 0:
			st.Channels = slices.Delete(st.Channels, i, i+1)
		case body.Name == "":
		case i >= 0:
			st.Channels[i].Name, st.Channels[i].Cat, st.Channels[i].Arch = SafeName(body.Name), body.Ch, body.Arch
		default:
			st.Channels = append(st.Channels, Channel{ID: body.Target, Name: SafeName(body.Name), Kind: k, Cat: body.Ch, Arch: body.Arch})
		}
	case KindHistory:
		// members already in keep the keys they hold; this decides what the
		// next ones get (core.keysFor) and whether adds rotate the key
		if bytes.Equal(e.Author, st.Owner) {
			st.NoHistory = body.NoHist
		}
	case KindServer:
		// owner only, ignored otherwise like a member's rename
		if bytes.Equal(e.Author, st.Owner) {
			st.Icon, st.IconMime = body.Blob, body.Mime
			st.Banner, st.BannerMime = body.Prev, body.PrevMime
			st.Style = SafeStyle(body.Style)
		}
	case KindAvatar:
		// only your own avatar, and only while you are a member
		if m, ok := st.Members[string(e.Author)]; ok && !m.Removed {
			m.Avatar, m.AvatarMime = body.Blob, body.Mime
		}
	case KindProfile:
		// status, description and banner — your own card, nobody else's.
		// Text arrives from the network: cleanse and cap it here too, so the
		// folded state is as safe to render as the profile index.
		if m, ok := st.Members[string(e.Author)]; ok && !m.Removed {
			m.Status, m.Bio, m.Style = SafeName(body.Text), SafeBio(body.Bio), SafeStyle(body.Style)
			m.Banner, m.BannerMime = body.Blob, body.Mime
		}
	case KindNick:
		// your own display name, nobody else's
		if m, ok := st.Members[string(e.Author)]; ok && !m.Removed && body.Name != "" {
			m.Name = SafeName(body.Name)
		}
	case KindReact:
		if !st.IsMember(e.Author) {
			if st.Degraded() {
				return nil
			}
			return errors.New("react by non-member")
		}
		if len(body.Target) > 0 {
			tID := string(body.Target)
			if st.Reactions[tID] == nil {
				st.Reactions[tID] = make(map[string]string)
			}
			// network text straight into the UI: cleanse (ANSI/OSC 52 in the
			// TUI) and cap — a reaction is an emoji, not a message
			if t := SafeName(body.Text); t == "" {
				delete(st.Reactions[tID], string(e.Author))
			} else {
				if len(t) > MaxReact {
					t = strings.ToValidUTF8(t[:MaxReact], "")
				}
				st.Reactions[tID][string(e.Author)] = t
			}
		}
	case KindDelete:
		if !st.IsMember(e.Author) {
			if st.Degraded() {
				return nil
			}
			return errors.New("delete by non-member")
		}
		// your own events, or anything for the owner. Ignored otherwise, so
		// a past delete of someone else's message reverts on the next fold.
		if a, ok := authors[string(body.Target)]; ok && (bytes.Equal(a, e.Author) || bytes.Equal(e.Author, st.Owner)) {
			st.Deleted[string(body.Target)] = true
		}
	case KindPin:
		if !st.IsMember(e.Author) {
			if st.Degraded() {
				return nil
			}
			return errors.New("pin by non-member")
		}
		// any member pins or unpins; the last event in canonical order wins
		if _, ok := authors[string(body.Target)]; ok {
			if body.Arch {
				delete(st.Pinned, string(body.Target))
			} else {
				st.Pinned[string(body.Target)] = true
			}
		}
	}
	return nil
}

// descends reports whether e has the applied event anc among its ancestors.
// Only events with a Lamport above anc's can lead to it.
func (f *Folder) descends(e *Event, anc []byte) bool {
	floor, ok := f.seen[string(anc)]
	if !ok {
		return false
	}
	queue, done := slices.Clone(e.Parents), map[string]bool{}
	for len(queue) > 0 {
		id := string(queue[len(queue)-1])
		queue = queue[:len(queue)-1]
		if id == string(anc) {
			return true
		}
		if done[id] || f.seen[id] <= floor {
			continue
		}
		done[id] = true
		queue = append(queue, f.parents[id]...)
	}
	return false
}

// checkLamport enforces Lamport = max(parents)+1 and presence of parents before
// child. Honest client computes this naturally (heads + 1), so nothing changes
// for valid histories — cuts off custom placement in fold order.
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

// removeTree: removal cascades to those introduced by removed member. Without this
// adding second identity suffices to survive kick —
// remains member, receiving all epoch keys including post-rotation ones.
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
