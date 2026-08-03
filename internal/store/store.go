// Package store: bbolt persistence. Every value is encrypted with the
// storage key (HKDF from identity seed) and bound to its location in the database
// (bucket + key) via associated data — events too, as their envelope
// (author, kind, epoch, relations) is chat metadata. Only record
// keys remain plaintext: event IDs, blob hashes, public keys.
package store

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	bolt "go.etcd.io/bbolt"

	"szept/internal/crypt"
	"szept/internal/event"
)

var (
	bEvents  = []byte("events")  // nested: chatID -> (eventID -> wire)
	bHeads   = []byte("heads")   // nested: chatID -> (eventID -> nil)
	bKeys    = []byte("keys")    // nested: chatID -> (epoch -> enc(key))
	bChats   = []byte("chats")   // chatID -> enc(json ChatMeta)
	bFriends = []byte("friends") // pub -> enc(json Friend)
	bInvites = []byte("invites") // sha256(secret) -> enc(json Invite)
	bBlobs   = []byte("blobs")   // sha256(plaintext) -> enc(bytes) — attachment content
	bAvatars = []byte("avatars") // pub -> enc(json ProfileMeta) — handy profile index
	bHidden  = []byte("hidden")  // nested: chatID -> (eventID -> nil) — hidden locally
	bMeta    = []byte("meta")    // key -> enc(value): own nick etc.
	// bBlobChat: nested chatID -> (blobHash -> nil). bBlobs is global and keyed by
	// content hash, so without this index "is the caller allowed to see this blob"
	// could only be answered for the chat named in the request, not the chat the
	// blob was actually announced in. Also makes blob GC on DeleteChat cheap.
	bBlobChat = []byte("blobchat")
)

type ChatMeta struct {
	Name string `json:"name"`
}

// Friend keeps two names apart on purpose. Name is what *this user* decided to
// call the person; PeerName is whatever the other side last announced over
// "hello". A compromised or malicious account can change what it calls itself —
// it must not be able to change what you called it, which is the whole setup of
// "hi, it's me, the admin".
type Friend struct {
	Pub       []byte `json:"pub"`
	Name      string `json:"name"`                // given locally — always wins
	PeerName  string `json:"peer_name,omitempty"` // last name from "hello"
	Connector string `json:"connector,omitempty"`
}

// Display is the name to show: the local one if there is one, otherwise the
// peer's own.
func (f Friend) Display() string {
	if f.Name != "" {
		return f.Name
	}
	return f.PeerName
}

// Alias returns the peer's self-chosen name when it differs from the local one,
// so a UI can show "introduces themselves as …" instead of silently hiding a
// rename. Empty when there is nothing to warn about.
func (f Friend) Alias() string {
	if f.Name == "" || f.PeerName == "" || f.Name == f.PeerName {
		return ""
	}
	return f.PeerName
}

type Invite struct {
	Chat    []byte `json:"chat"`
	Used    bool   `json:"used"`
	PinHash []byte `json:"pin,omitempty"` // sha256 of the one-time PIN
	Exp     int64  `json:"exp,omitempty"` // unix; 0 = no expiry
	Tries   int    `json:"tries,omitempty"`
}

// MaxPinTries: 6 digits is 10^6 combinations, and "redeem" is a regular message on
// an open channel — without rate limit the full space falls in seconds.
const MaxPinTries = 5

func (i Invite) Expired(now int64) bool { return i.Exp > 0 && now > i.Exp }

type Store struct {
	db  *bolt.DB
	key []byte // storage key for at-rest value encryption
}

// dbVersion: format version on disk. Key lives in plaintext (it's not secret) and serves
// only so a database in old format states so explicitly, instead of
// failing during decryption.
const dbVersion = "2"

var kVersion = []byte("dbver")

func Open(path string, storageKey []byte) (*Store, error) {
	// Timeout: without it a second instance hangs indefinitely on flock
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 3 * time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bEvents, bHeads, bKeys, bChats, bFriends, bInvites, bBlobs, bAvatars, bHidden, bMeta, bBlobChat} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		meta := tx.Bucket(bMeta)
		switch v := meta.Get(kVersion); {
		case v == nil && tx.Bucket(bChats).Stats().KeyN == 0 && tx.Bucket(bEvents).Stats().BucketN <= 1:
			return meta.Put(kVersion, []byte(dbVersion)) // fresh DB
		case v == nil || string(v) != dbVersion:
			return fmt.Errorf("baza %s jest w starszym formacie (%q, teraz %q) — skasuj plik i zaloguj się ponownie",
				path, v, dbVersion)
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, key: storageKey}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Snapshot writes a consistent copy of the open database (a bbolt read
// transaction) — the basis of a working backup without closing the app.
func (s *Store) Snapshot(w io.Writer) error {
	return s.db.View(func(tx *bolt.Tx) error {
		_, err := tx.WriteTo(w)
		return err
	})
}

// ad binds a value to its location in DB (bucket + keys). Without this, possessing
// the DB file allows swapping encrypted values between records — replacing
// a blob under a hash or moving an epoch key to another chat — even though
// none of them can be decrypted.
func ad(parts ...[]byte) []byte {
	var b []byte
	for _, p := range parts {
		b = binary.BigEndian.AppendUint32(b, uint32(len(p)))
		b = append(b, p...)
	}
	return b
}

func (s *Store) seal(ad, v []byte) []byte {
	box, err := crypt.Seal(s.key, v, ad)
	if err != nil {
		panic(err)
	}
	return box
}

func (s *Store) open(ad, box []byte) ([]byte, error) { return crypt.Open(s.key, box, ad) }

func sub(tx *bolt.Tx, root, name []byte, create bool) (*bolt.Bucket, error) {
	r := tx.Bucket(root)
	if create {
		return r.CreateBucketIfNotExists(name)
	}
	b := r.Bucket(name)
	if b == nil {
		return nil, nil
	}
	return b, nil
}

// AddEvent persists a verified event and maintains heads.
// Caller guarantees parents are already stored (causal ingestion).
// Returns false if the event was already known.
func (s *Store) AddEvent(chat []byte, e *event.Event) (bool, error) {
	id := e.ID()
	fresh := false
	err := s.db.Update(func(tx *bolt.Tx) error {
		evs, err := sub(tx, bEvents, chat, true)
		if err != nil {
			return err
		}
		if evs.Get(id) != nil {
			return nil
		}
		fresh = true
		// events are encrypted too: body was encrypted always, but
		// envelope (author, kind, epoch, relations) was plaintext, revealing
		// "who with whom and how much" graph without any key
		if err := evs.Put(id, s.seal(ad(bEvents, chat, id), event.Marshal(e))); err != nil {
			return err
		}
		heads, err := sub(tx, bHeads, chat, true)
		if err != nil {
			return err
		}
		for _, p := range e.Parents {
			if err := heads.Delete(p); err != nil {
				return err
			}
		}
		return heads.Put(id, nil)
	})
	return fresh, err
}

func (s *Store) HasEvent(chat, id []byte) bool {
	ok := false
	s.db.View(func(tx *bolt.Tx) error {
		if b, _ := sub(tx, bEvents, chat, false); b != nil {
			ok = b.Get(id) != nil
		}
		return nil
	})
	return ok
}

func (s *Store) GetEvent(chat, id []byte) (*event.Event, error) {
	var raw []byte
	s.db.View(func(tx *bolt.Tx) error {
		if b, _ := sub(tx, bEvents, chat, false); b != nil {
			if v := b.Get(id); v != nil {
				raw = bytes.Clone(v)
			}
		}
		return nil
	})
	if raw == nil {
		return nil, fmt.Errorf("event %x not found", id[:6])
	}
	pt, err := s.open(ad(bEvents, chat, id), raw)
	if err != nil {
		return nil, err
	}
	return event.Unmarshal(pt)
}

func (s *Store) Events(chat []byte) ([]*event.Event, error) {
	var out []*event.Event
	err := s.db.View(func(tx *bolt.Tx) error {
		b, _ := sub(tx, bEvents, chat, false)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			pt, err := s.open(ad(bEvents, chat, k), v)
			if err != nil {
				return err
			}
			e, err := event.Unmarshal(pt)
			if err != nil {
				return err
			}
			out = append(out, e)
			return nil
		})
	})
	return out, err
}

func (s *Store) Heads(chat []byte) [][]byte {
	var out [][]byte
	s.db.View(func(tx *bolt.Tx) error {
		b, _ := sub(tx, bHeads, chat, false)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, _ []byte) error {
			out = append(out, bytes.Clone(k))
			return nil
		})
	})
	return out
}

// NextLamport: max lamport among heads + 1.
func (s *Store) NextLamport(chat []byte) uint64 { return s.LamportAfter(chat, s.Heads(chat)) }

// LamportAfter: max lamport among the given parents + 1. Fold strictly requires
// this exact value, so parents and lamport must come from a single heads read.
func (s *Store) LamportAfter(chat []byte, parents [][]byte) uint64 {
	var max uint64
	for _, h := range parents {
		if e, err := s.GetEvent(chat, h); err == nil && e.Lamport > max {
			max = e.Lamport
		}
	}
	return max + 1
}

// ---- chats ----

func (s *Store) PutChat(chat []byte, m ChatMeta) error {
	j, _ := json.Marshal(m)
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bChats).Put(chat, s.seal(ad(bChats, chat), j))
	})
}

func (s *Store) Chats() (map[string]ChatMeta, error) {
	out := map[string]ChatMeta{}
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bChats).ForEach(func(k, v []byte) error {
			pt, err := s.open(ad(bChats, k), v)
			if err != nil {
				return err
			}
			var m ChatMeta
			if err := json.Unmarshal(pt, &m); err != nil {
				return err
			}
			out[string(bytes.Clone(k))] = m
			return nil
		})
	})
	return out, err
}

func (s *Store) DeleteChat(chat []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		tx.Bucket(bChats).Delete(chat)
		tx.Bucket(bEvents).DeleteBucket(chat)
		tx.Bucket(bHeads).DeleteBucket(chat)
		tx.Bucket(bKeys).DeleteBucket(chat)
		tx.Bucket(bHidden).DeleteBucket(chat)
		// blobs of this chat go too, unless another chat still announces them
		// (content addressing means one file can be shared by several chats)
		root := tx.Bucket(bBlobChat)
		mine := root.Bucket(chat)
		if mine == nil {
			return nil
		}
		var orphaned [][]byte
		mine.ForEach(func(hash, _ []byte) error {
			if !blobUsedElsewhere(root, chat, hash) {
				orphaned = append(orphaned, bytes.Clone(hash))
			}
			return nil
		})
		if err := root.DeleteBucket(chat); err != nil {
			return err
		}
		for _, h := range orphaned {
			if err := tx.Bucket(bBlobs).Delete(h); err != nil {
				return err
			}
		}
		return nil
	})
}

func blobUsedElsewhere(root *bolt.Bucket, chat, hash []byte) bool {
	used := false
	root.ForEachBucket(func(other []byte) error {
		if used || bytes.Equal(other, chat) {
			return nil
		}
		if b := root.Bucket(other); b != nil && b.Get(hash) != nil {
			used = true
		}
		return nil
	})
	return used
}

// NoteBlobChat records that this chat announced this blob. Called wherever an
// attachment, avatar or banner enters a chat.
func (s *Store) NoteBlobChat(chat, hash []byte) error {
	if len(hash) == 0 || len(chat) == 0 {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := sub(tx, bBlobChat, chat, true)
		if err != nil {
			return err
		}
		return b.Put(hash, nil)
	})
}

// BlobInChat reports whether this blob was announced in this chat. Serving a
// blob to a chat member requires it — a shared chat must not become a key to
// files from every other chat.
func (s *Store) BlobInChat(chat, hash []byte) bool {
	ok := false
	s.db.View(func(tx *bolt.Tx) error {
		if b, _ := sub(tx, bBlobChat, chat, false); b != nil {
			ok = b.Get(hash) != nil
		}
		return nil
	})
	return ok
}

// ---- hidden events (soft delete locally only) ----
//
// DAG history is signed and replicated — nothing can be removed
// without breaking the hash chain. "Hide locally" is a local list of events
// that the UI doesn't render. Bytes stay, but on this device only.

func (s *Store) HideEvent(chat, id []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := sub(tx, bHidden, chat, true)
		if err != nil {
			return err
		}
		return b.Put(id, nil)
	})
}

// ShowAllHidden unhides everything in given chat ("restore" button).
func (s *Store) ShowAllHidden(chat []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket(bHidden).Bucket(chat) == nil {
			return nil
		}
		return tx.Bucket(bHidden).DeleteBucket(chat)
	})
}

// Hidden returns set of hidden IDs (key = string(eventID)).
func (s *Store) Hidden(chat []byte) map[string]bool {
	out := map[string]bool{}
	s.db.View(func(tx *bolt.Tx) error {
		b, _ := sub(tx, bHidden, chat, false)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, _ []byte) error {
			out[string(k)] = true
			return nil
		})
	})
	return out
}

// ---- meta: minor settings of own profile ----

func (s *Store) PutMeta(key string, val []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bMeta).Put([]byte(key), s.seal(ad(bMeta, []byte(key)), val))
	})
}

func (s *Store) Meta(key string) []byte {
	var out []byte
	s.db.View(func(tx *bolt.Tx) error {
		if v := tx.Bucket(bMeta).Get([]byte(key)); v != nil {
			if pt, err := s.open(ad(bMeta, []byte(key)), v); err == nil {
				out = pt
			}
		}
		return nil
	})
	return out
}

// ---- epoch keys ----

func epochKey(ep uint32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], ep)
	return b[:]
}

func (s *Store) PutChatKey(chat []byte, epoch uint32, key []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := sub(tx, bKeys, chat, true)
		if err != nil {
			return err
		}
		return b.Put(epochKey(epoch), s.seal(ad(bKeys, chat, epochKey(epoch)), key))
	})
}

func (s *Store) ChatKeys(chat []byte) (map[uint32][]byte, error) {
	out := map[uint32][]byte{}
	err := s.db.View(func(tx *bolt.Tx) error {
		b, _ := sub(tx, bKeys, chat, false)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			pt, err := s.open(ad(bKeys, chat, k), v)
			if err != nil {
				return err
			}
			out[binary.BigEndian.Uint32(k)] = pt
			return nil
		})
	})
	return out, err
}

// ---- friends ----

func (s *Store) PutFriend(f Friend) error {
	j, _ := json.Marshal(f)
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bFriends).Put(f.Pub, s.seal(ad(bFriends, f.Pub), j))
	})
}

func (s *Store) DelFriend(pub []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bFriends).Delete(pub)
	})
}

func (s *Store) Friends() ([]Friend, error) {
	var out []Friend
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bFriends).ForEach(func(k, v []byte) error {
			pt, err := s.open(ad(bFriends, k), v)
			if err != nil {
				return err
			}
			var f Friend
			if err := json.Unmarshal(pt, &f); err != nil {
				return err
			}
			out = append(out, f)
			return nil
		})
	})
	return out, err
}

func (s *Store) IsFriend(pub []byte) bool {
	ok := false
	s.db.View(func(tx *bolt.Tx) error {
		ok = tx.Bucket(bFriends).Get(pub) != nil
		return nil
	})
	return ok
}

// ---- one-time invites (inviter side) ----

func (s *Store) PutInvite(secretHash []byte, inv Invite) error {
	j, _ := json.Marshal(inv)
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bInvites).Put(secretHash, s.seal(ad(bInvites, secretHash), j))
	})
}

// OpenInviteCount: number of unused, unexpired invites (drives the accept-gate).
func (s *Store) OpenInviteCount() int {
	n := 0
	now := time.Now().Unix()
	s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bInvites).ForEach(func(k, v []byte) error {
			if pt, err := s.open(ad(bInvites, k), v); err == nil {
				var inv Invite
				if json.Unmarshal(pt, &inv) == nil && !inv.Used && !inv.Expired(now) {
					n++
				}
			}
			return nil
		})
	})
	return n
}

// TakeInvite atomically consumes an unused invite. Errors if unknown, used,
// expired or the PIN doesn't match. Wrong PIN does not redeem invite, but
// consumes an attempt — after MaxPinTries the invite is burned.
func (s *Store) TakeInvite(secretHash, pin []byte) (Invite, error) {
	var inv Invite
	var reject error // error returned AFTER commit: return from Update rolls back write
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bInvites)
		v := b.Get(secretHash)
		if v == nil {
			return errors.New("unknown invite")
		}
		pt, err := s.open(ad(bInvites, secretHash), v)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(pt, &inv); err != nil {
			return err
		}
		if inv.Used {
			return errors.New("invite already used")
		}
		if inv.Expired(time.Now().Unix()) {
			return errors.New("invite expired")
		}
		if len(inv.PinHash) > 0 {
			h := sha256.Sum256(pin)
			if subtle.ConstantTimeCompare(h[:], inv.PinHash) != 1 {
				inv.Tries++
				reject = errors.New("bad pin")
				if inv.Tries >= MaxPinTries {
					inv.Used = true
					reject = errors.New("too many bad pins")
				}
				j, _ := json.Marshal(inv)
				return b.Put(secretHash, s.seal(ad(bInvites, secretHash), j))
			}
		}
		inv.Used = true
		j, _ := json.Marshal(inv)
		return b.Put(secretHash, s.seal(ad(bInvites, secretHash), j))
	})
	if err != nil {
		return inv, err
	}
	return inv, reject
}

// ---- blobs: attachment and avatar content ----
//
// Content-addressed: key = sha256(plaintext bytes), so the same image
// sent in five chats is stored once, and recipient verifies if they got what
// signed event promised. Value is encrypted with storage key.
// DeleteChat drops the blobs of the deleted chat, keeping any the bBlobChat
// index still shows in another conversation.

// PutBlob saves content under its hash and returns the hash.
func (s *Store) PutBlob(data []byte) ([]byte, error) {
	h := sha256.Sum256(data)
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bBlobs)
		if b.Get(h[:]) != nil {
			return nil // same content = same key, nothing to overwrite
		}
		return b.Put(h[:], s.seal(ad(bBlobs, h[:]), data))
	})
	return h[:], err
}

func (s *Store) GetBlob(hash []byte) ([]byte, bool) {
	var out []byte
	s.db.View(func(tx *bolt.Tx) error {
		if v := tx.Bucket(bBlobs).Get(hash); v != nil {
			pt, err := s.open(ad(bBlobs, hash), v)
			if err == nil {
				out = pt
			}
		}
		return nil
	})
	return out, out != nil
}

func (s *Store) HasBlob(hash []byte) bool {
	ok := false
	s.db.View(func(tx *bolt.Tx) error {
		ok = tx.Bucket(bBlobs).Get(hash) != nil
		return nil
	})
	return ok
}

// ---- avatars ----

// ProfileMeta: someone's profile card — avatar blob with its mime type
// (image/png or image/gif), status line, description and banner blob.
type ProfileMeta struct {
	Blob       []byte `json:"blob"`
	Mime       string `json:"mime"`
	Status     string `json:"status,omitempty"`
	Bio        string `json:"bio,omitempty"`
	Banner     []byte `json:"banner,omitempty"`
	BannerMime string `json:"bmime,omitempty"`
}

func (p ProfileMeta) empty() bool {
	return len(p.Blob) == 0 && len(p.Banner) == 0 && p.Status == "" && p.Bio == ""
}

// PutProfile stores the profile card for given public key. Real source
// of truth is signed events in chats — this is an index so friend list
// doesn't have to traverse entire DAG. An empty card deletes the entry.
func (s *Store) PutProfile(pub []byte, a ProfileMeta) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bAvatars)
		if a.empty() {
			return b.Delete(pub)
		}
		j, _ := json.Marshal(a)
		return b.Put(pub, s.seal(ad(bAvatars, pub), j))
	})
}

func (s *Store) Profile(pub []byte) (ProfileMeta, bool) {
	var a ProfileMeta
	ok := false
	s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bAvatars).Get(pub)
		if v == nil {
			return nil
		}
		pt, err := s.open(ad(bAvatars, pub), v)
		if err != nil {
			return nil
		}
		ok = json.Unmarshal(pt, &a) == nil && !a.empty()
		return nil
	})
	return a, ok
}

// quietKinds: profile events (avatar, nick) are not conversation content and should not
// increment "unread" badge. Kind is plaintext in wire format,
// so after decryption searching by bytes suffices — without JSON.
var quietKinds = [][]byte{
	[]byte(`"kind":"` + event.KindAvatar + `"`),
	[]byte(`"kind":"` + event.KindNick + `"`),
	[]byte(`"kind":"` + event.KindProfile + `"`),
}

func quietWire(v []byte) bool {
	for _, k := range quietKinds {
		if bytes.Contains(v, k) {
			return true
		}
	}
	return false
}

// EventCount: cheap event tally for unread badges (no decrypt).
// ponytail: value iteration instead of Stats().KeyN — bytes live in mmap,
// so this is a memory scan, not disk I/O. If history grows to hundreds of
// thousands of events, keep counter separately instead of recomputing each time.
func (s *Store) EventCount(chat []byte) int {
	n := 0
	s.db.View(func(tx *bolt.Tx) error {
		b, _ := sub(tx, bEvents, chat, false)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			pt, err := s.open(ad(bEvents, chat, k), v)
			if err != nil {
				return err
			}
			if !quietWire(pt) {
				n++
			}
			return nil
		})
	})
	return n
}
