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

const magic = "SZEPT1"

type Identity struct {
	Priv ed25519.PrivateKey
	Pub  ed25519.PublicKey
	Name string
}

func Create() *Identity {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	return &Identity{Priv: priv, Pub: pub}
}

// Save: magic || salt(16) || box(seed || name).
func (id *Identity) Save(path string, pass []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	salt := crypt.RandBytes(16)
	box, err := crypt.Seal(crypt.PassKey(pass, salt), append(id.Priv.Seed(), []byte(id.Name)...), []byte(magic))
	if err != nil {
		return err
	}
	data := append([]byte(magic), salt...)
	return os.WriteFile(path, append(data, box...), 0o600)
}

func Load(path string, pass []byte) (*Identity, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) < len(magic)+16 || string(raw[:len(magic)]) != magic {
		return nil, errors.New("identity: bad file format")
	}
	salt := raw[len(magic) : len(magic)+16]
	pt, err := crypt.Open(crypt.PassKey(pass, salt), raw[len(magic)+16:], []byte(magic))
	if err != nil {
		return nil, errors.New("identity: wrong passphrase or corrupted file")
	}
	if len(pt) < ed25519.SeedSize {
		return nil, errors.New("identity: short seed")
	}
	priv := ed25519.NewKeyFromSeed(pt[:ed25519.SeedSize])
	return &Identity{
		Priv: priv,
		Pub:  priv.Public().(ed25519.PublicKey),
		Name: string(pt[ed25519.SeedSize:]),
	}, nil
}

// StorageKey: deterministic key for at-rest DB value encryption.
func (id *Identity) StorageKey() []byte {
	return crypt.Derive(id.Priv.Seed(), "szept-storage-v1")
}

func Fingerprint(pub []byte) string {
	h := sha256.Sum256(pub)
	s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(h[:10])
	return fmt.Sprintf("%s-%s-%s-%s", s[0:4], s[4:8], s[8:12], s[12:16])
}
