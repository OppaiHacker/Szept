// Package store: bbolt persistence. Event wire bytes are stored as-is (their
// bodies are already E2E-encrypted); everything else (chat names, epoch keys,
// friends, invites) is encrypted at rest with the identity-derived storage key.
package store

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
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
	bBlobs   = []byte("blobs")   // sha256(plaintext) -> enc(bytes) — treść załączników
	bAvatars = []byte("avatars") // pub -> enc(json AvatarMeta) — podręczny indeks awatarów
	bHidden  = []byte("hidden")  // nested: chatID -> (eventID -> nil) — schowane u nas
	bMeta    = []byte("meta")    // klucz -> enc(wartość): własny nick itd.
)

type ChatMeta struct {
	Name string `json:"name"`
}

type Friend struct {
	Pub       []byte `json:"pub"`
	Name      string `json:"name"`
	Connector string `json:"connector,omitempty"`
}

type Invite struct {
	Chat    []byte `json:"chat"`
	Used    bool   `json:"used"`
	PinHash []byte `json:"pin,omitempty"` // sha256 of the one-time PIN
	Exp     int64  `json:"exp,omitempty"` // unix; 0 = no expiry
}

func (i Invite) Expired(now int64) bool { return i.Exp > 0 && now > i.Exp }

type Store struct {
	db  *bolt.DB
	key []byte // storage key for at-rest value encryption
}

func Open(path string, storageKey []byte) (*Store, error) {
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bEvents, bHeads, bKeys, bChats, bFriends, bInvites, bBlobs, bAvatars, bHidden, bMeta} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
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

func (s *Store) seal(v []byte) []byte {
	box, err := crypt.Seal(s.key, v, nil)
	if err != nil {
		panic(err)
	}
	return box
}

func (s *Store) open(box []byte) ([]byte, error) { return crypt.Open(s.key, box, nil) }

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
		if err := evs.Put(id, event.Marshal(e)); err != nil {
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
	return event.Unmarshal(raw)
}

func (s *Store) Events(chat []byte) ([]*event.Event, error) {
	var out []*event.Event
	err := s.db.View(func(tx *bolt.Tx) error {
		b, _ := sub(tx, bEvents, chat, false)
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, v []byte) error {
			e, err := event.Unmarshal(v)
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
func (s *Store) NextLamport(chat []byte) uint64 {
	var max uint64
	for _, h := range s.Heads(chat) {
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
		return tx.Bucket(bChats).Put(chat, s.seal(j))
	})
}

func (s *Store) Chats() (map[string]ChatMeta, error) {
	out := map[string]ChatMeta{}
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bChats).ForEach(func(k, v []byte) error {
			pt, err := s.open(v)
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
		return nil
	})
}

// ---- schowane zdarzenia (soft delete tylko u nas) ----
//
// Historia w DAG-u jest podpisana i replikowana — nie da się z niej nic wyjąć
// bez zerwania łańcucha hashy. „Usuń u mnie” to więc lokalna lista zdarzeń,
// których UI nie rysuje. Bajty zostają, ale tylko na tym urządzeniu.

func (s *Store) HideEvent(chat, id []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := sub(tx, bHidden, chat, true)
		if err != nil {
			return err
		}
		return b.Put(id, nil)
	})
}

// ShowAllHidden odkrywa wszystko w danym czacie (przycisk „przywróć”).
func (s *Store) ShowAllHidden(chat []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket(bHidden).Bucket(chat) == nil {
			return nil
		}
		return tx.Bucket(bHidden).DeleteBucket(chat)
	})
}

// Hidden zwraca zbiór schowanych ID (klucz = string(eventID)).
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

// ---- meta: drobne ustawienia własnego profilu ----

func (s *Store) PutMeta(key string, val []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bMeta).Put([]byte(key), s.seal(val))
	})
}

func (s *Store) Meta(key string) []byte {
	var out []byte
	s.db.View(func(tx *bolt.Tx) error {
		if v := tx.Bucket(bMeta).Get([]byte(key)); v != nil {
			if pt, err := s.open(v); err == nil {
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
		return b.Put(epochKey(epoch), s.seal(key))
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
			pt, err := s.open(v)
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
		return tx.Bucket(bFriends).Put(f.Pub, s.seal(j))
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
		return tx.Bucket(bFriends).ForEach(func(_, v []byte) error {
			pt, err := s.open(v)
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
		return tx.Bucket(bInvites).Put(secretHash, s.seal(j))
	})
}

// OpenInviteCount: number of unused, unexpired invites (drives the accept-gate).
func (s *Store) OpenInviteCount() int {
	n := 0
	now := time.Now().Unix()
	s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bInvites).ForEach(func(_, v []byte) error {
			if pt, err := s.open(v); err == nil {
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
// expired or the PIN doesn't match. A wrong PIN does not burn the invite.
// No attempt limit — 6-digit PIN over per-attempt P2P handshake is slow enough.
func (s *Store) TakeInvite(secretHash, pin []byte) (Invite, error) {
	var inv Invite
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bInvites)
		v := b.Get(secretHash)
		if v == nil {
			return errors.New("unknown invite")
		}
		pt, err := s.open(v)
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
				return errors.New("bad pin")
			}
		}
		inv.Used = true
		j, _ := json.Marshal(inv)
		return b.Put(secretHash, s.seal(j))
	})
	return inv, err
}

// ---- blobs: treść załączników i awatarów ----
//
// Adresowane treścią: klucz = sha256(jawnych bajtów), więc ta sama grafika
// wysłana w pięciu czatach leży raz, a odbiorca sam sprawdza, czy dostał to,
// co obiecywał podpisany event. Wartość jest zaszyfrowana kluczem magazynu.
// Bez odśmiecania — DeleteChat zostawia bloby. Sprzątanie dopiszemy,
// gdy komuś baza spuchnie; dziś skasowanie ich groziłoby usunięciem załącznika
// wciąż widocznego w innym czacie.

// PutBlob zapisuje treść pod jej własnym skrótem i ten skrót zwraca.
func (s *Store) PutBlob(data []byte) ([]byte, error) {
	h := sha256.Sum256(data)
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bBlobs)
		if b.Get(h[:]) != nil {
			return nil // ta sama treść = ten sam klucz, nie ma co nadpisywać
		}
		return b.Put(h[:], s.seal(data))
	})
	return h[:], err
}

func (s *Store) GetBlob(hash []byte) ([]byte, bool) {
	var out []byte
	s.db.View(func(tx *bolt.Tx) error {
		if v := tx.Bucket(bBlobs).Get(hash); v != nil {
			pt, err := s.open(v)
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

// ---- awatary ----

// AvatarMeta wskazuje blob z obrazkiem i jego typ (image/png albo image/gif).
type AvatarMeta struct {
	Blob []byte `json:"blob"`
	Mime string `json:"mime"`
}

// PutAvatar zapamiętuje awatar danego klucza publicznego. Prawdziwym źródłem
// prawdy są podpisane eventy w czatach — to tylko indeks, żeby lista znajomych
// nie musiała rozwijać całego DAG-a. Pusty Blob kasuje wpis.
func (s *Store) PutAvatar(pub []byte, a AvatarMeta) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bAvatars)
		if len(a.Blob) == 0 {
			return b.Delete(pub)
		}
		j, _ := json.Marshal(a)
		return b.Put(pub, s.seal(j))
	})
}

func (s *Store) Avatar(pub []byte) (AvatarMeta, bool) {
	var a AvatarMeta
	ok := false
	s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bAvatars).Get(pub)
		if v == nil {
			return nil
		}
		pt, err := s.open(v)
		if err != nil {
			return nil
		}
		ok = json.Unmarshal(pt, &a) == nil && len(a.Blob) > 0
		return nil
	})
	return a, ok
}

// quietKinds: zdarzenia profilowe (awatar, nick) nie są treścią rozmowy i nie
// mają nabijać odznaki „nieprzeczytane”. Rodzaj jest jawny w zapisie sieciowym,
// więc wystarczy szukanie po bajtach — bez JSON-a i bez odszyfrowania.
var quietKinds = [][]byte{
	[]byte(`"kind":"` + event.KindAvatar + `"`),
	[]byte(`"kind":"` + event.KindNick + `"`),
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
// Przejście po wartościach zamiast Stats().KeyN — bajty leżą w mmapie,
// więc to skan pamięci, nie odczyt z dysku. Gdyby historia urosła do setek
// tysięcy zdarzeń, trzymajmy licznik osobno zamiast liczyć go za każdym razem.
func (s *Store) EventCount(chat []byte) int {
	n := 0
	s.db.View(func(tx *bolt.Tx) error {
		b, _ := sub(tx, bEvents, chat, false)
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, v []byte) error {
			if !quietWire(v) {
				n++
			}
			return nil
		})
	})
	return n
}
