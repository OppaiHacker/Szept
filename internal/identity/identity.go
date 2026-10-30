// Package identity: Ed25519 keypair = who you are. Stored encrypted at rest
// (argon2id passphrase -> XChaCha20-Poly1305). Fingerprint = short hash of pub.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"szept/internal/crypt"
)

// magic: v2 adds a random database key next to the seed. v1 files (no key,
// the database key was derived from the seed) still load.
const (
	magic   = "SZEPT2"
	magicV1 = "SZEPT1"
)

type Identity struct {
	Priv ed25519.PrivateKey
	Pub  ed25519.PublicKey
	Name string
	// DBKey encrypts the local database. Random, not derived from the seed, so
	// a password change can replace it: an old copy of this file opened with
	// the old password then no longer opens the current database.
	DBKey []byte
}

func Create() *Identity {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	return &Identity{Priv: priv, Pub: pub, DBKey: crypt.NewKey()}
}

// Save: magic || salt(16) || box(seed || dbkey || name). Written to a temp file
// and renamed, so a crash never leaves a half-written identity.
func (id *Identity) Save(path string, pass []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	salt := crypt.RandBytes(16)
	pt := append(append(id.Priv.Seed(), id.DBKey...), id.Name...)
	box, err := crypt.Seal(crypt.PassKey(pass, salt), pt, []byte(magic))
	if err != nil {
		return err
	}
	data := append([]byte(magic), salt...)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, box...), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func Load(path string, pass []byte) (*Identity, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) < len(magic)+16 {
		return nil, errors.New("identity: bad file format")
	}
	m := string(raw[:len(magic)])
	if m != magic && m != magicV1 {
		return nil, errors.New("identity: bad file format")
	}
	salt := raw[len(magic) : len(magic)+16]
	pt, err := crypt.Open(crypt.PassKey(pass, salt), raw[len(magic)+16:], []byte(m))
	if err != nil {
		return nil, errors.New("identity: wrong passphrase or corrupted file")
	}
	n := ed25519.SeedSize
	if m == magic {
		n += crypt.KeySize
	}
	if len(pt) < n {
		return nil, errors.New("identity: short seed")
	}
	priv := ed25519.NewKeyFromSeed(pt[:ed25519.SeedSize])
	id := &Identity{
		Priv:  priv,
		Pub:   priv.Public().(ed25519.PublicKey),
		Name:  string(pt[n:]),
		DBKey: pt[ed25519.SeedSize:n],
	}
	if m == magicV1 {
		id.DBKey = crypt.Derive(id.Priv.Seed(), "szept-storage-v1")
	}
	return id, nil
}

// StorageKey: key for at-rest DB value encryption.
func (id *Identity) StorageKey() []byte { return id.DBKey }

func Fingerprint(pub []byte) string {
	h := sha256.Sum256(pub)
	s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(h[:10])
	return fmt.Sprintf("%s-%s-%s-%s", s[0:4], s[4:8], s[8:12], s[12:16])
}
