package p2p

// Signal sealing: the signed offer/answer (SDP with every candidate IP, the
// DTLS fingerprint and the obfuscation key) is encrypted to the addressee
// before it touches a connector. A hostile connector, or a DPI box that owns
// one, then sees only From/To and a random-looking blob, so it cannot hand
// the obfs key to a censor or harvest IPs from SDP.
//
// ECIES over X25519: the addressee's Ed25519 key is mapped to its Montgomery
// form, the sender uses a fresh ephemeral key per signal. Sender authenticity
// still comes from the Ed25519 signature inside.

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"errors"
	"math/big"

	"szept/internal/crypt"
)

var curveP, _ = new(big.Int).SetString("7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffed", 16)

// edPubToX converts an Ed25519 public key to X25519: u = (1+y)/(1-y) mod p.
func edPubToX(pub []byte) (*ecdh.PublicKey, error) {
	if len(pub) != ed25519.PublicKeySize {
		return nil, errors.New("p2p: bad public key")
	}
	le := append([]byte(nil), pub...)
	le[31] &= 0x7f
	y := new(big.Int).SetBytes(reverse(le))
	den := new(big.Int).Sub(big.NewInt(1), y)
	den.Mod(den, curveP)
	if den.Sign() == 0 {
		return nil, errors.New("p2p: bad public key")
	}
	u := new(big.Int).Add(big.NewInt(1), y)
	u.Mul(u, den.ModInverse(den, curveP))
	u.Mod(u, curveP)
	out := make([]byte, 32)
	u.FillBytes(out)
	return ecdh.X25519().NewPublicKey(reverse(out))
}

// edPrivToX: the X25519 scalar is the hashed Ed25519 seed (ecdh clamps it).
func edPrivToX(priv ed25519.PrivateKey) (*ecdh.PrivateKey, error) {
	h := sha512.Sum512(priv.Seed())
	return ecdh.X25519().NewPrivateKey(h[:32])
}

func reverse(b []byte) []byte {
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return b
}

func sealKey(shared, epub, to []byte) []byte {
	return crypt.Derive(shared, "szept-signal\x00"+string(epub)+string(to))
}

// sealTo: ephemeral X25519 pub (32B) || XChaCha20-Poly1305 box.
func sealTo(to, msg []byte) ([]byte, error) {
	rx, err := edPubToX(to)
	if err != nil {
		return nil, err
	}
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	shared, err := eph.ECDH(rx)
	if err != nil {
		return nil, err
	}
	epub := eph.PublicKey().Bytes()
	box, err := crypt.Seal(sealKey(shared, epub, to), msg, epub)
	if err != nil {
		return nil, err
	}
	return append(epub, box...), nil
}

func openFrom(priv ed25519.PrivateKey, blob []byte) ([]byte, error) {
	if len(blob) < 32 {
		return nil, errors.New("p2p: sealed signal too short")
	}
	x, err := edPrivToX(priv)
	if err != nil {
		return nil, err
	}
	epub, err := ecdh.X25519().NewPublicKey(blob[:32])
	if err != nil {
		return nil, err
	}
	shared, err := x.ECDH(epub)
	if err != nil {
		return nil, err
	}
	me := priv.Public().(ed25519.PublicKey)
	return crypt.Open(sealKey(shared, blob[:32], me), blob[32:], blob[:32])
}
