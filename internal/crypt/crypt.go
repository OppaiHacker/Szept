// Package crypt: symmetric crypto helpers. XChaCha20-Poly1305 everywhere,
// argon2id for passphrase KDF, HKDF for subkey derivation.
package crypt

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const KeySize = chacha20poly1305.KeySize // 32

func NewKey() []byte {
	k := make([]byte, KeySize)
	if _, err := rand.Read(k); err != nil {
		panic(err)
	}
	return k
}

// Seal: nonce || AEAD ciphertext.
func Seal(key, plaintext, ad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, ad), nil
}

func Open(key, box, ad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	if len(box) < aead.NonceSize() {
		return nil, errors.New("crypt: box too short")
	}
	return aead.Open(nil, box[:aead.NonceSize()], box[aead.NonceSize():], ad)
}

// PassKey derives a key from a passphrase. Salt is caller-stored.
func PassKey(pass, salt []byte) []byte {
	return argon2.IDKey(pass, salt, 3, 64*1024, 2, KeySize)
}

// Derive expands a master secret into a labeled subkey.
func Derive(master []byte, label string) []byte {
	k := make([]byte, KeySize)
	r := hkdf.New(sha256.New, master, nil, []byte(label))
	if _, err := io.ReadFull(r, k); err != nil {
		panic(err)
	}
	return k
}

func RandBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
