package event

import (
	"bytes"
	"crypto/ed25519"
)

// Linked devices. One person (an account) may use szept on several devices.
// Each device keeps its own key and is an ordinary member of every chat; a
// link proof says which account a device belongs to, so clients can show
// the devices as one person. The proof is signed by both keys: by the account,
// so nobody can claim a device into their account, and by the device, so an
// account cannot claim someone else's key and have their messages shown under
// its name.
//
// Proof layout: account pub (32) || account sig (64) || device sig (64), both
// signatures over LinkStatement(account, device).
const LinkProofSize = ed25519.PublicKeySize + 2*ed25519.SignatureSize

// LinkStatement is what both keys sign.
func LinkStatement(account, device []byte) []byte {
	return append(append([]byte("szept-link-v1\x00"), account...), device...)
}

// LinkProof joins the account pub and the two signatures into one proof.
func LinkProof(account, accountSig, deviceSig []byte) []byte {
	return append(append(append([]byte(nil), account...), accountSig...), deviceSig...)
}

// VerifyLink returns the account a device belongs to, or nil when proof is
// not a valid link of device.
func VerifyLink(proof, device []byte) []byte {
	if len(proof) != LinkProofSize || len(device) != ed25519.PublicKeySize {
		return nil
	}
	acct := proof[:ed25519.PublicKeySize]
	if bytes.Equal(acct, device) {
		return nil
	}
	msg := LinkStatement(acct, device)
	asig := proof[ed25519.PublicKeySize : ed25519.PublicKeySize+ed25519.SignatureSize]
	dsig := proof[ed25519.PublicKeySize+ed25519.SignatureSize:]
	if !ed25519.Verify(acct, msg, asig) || !ed25519.Verify(device, msg, dsig) {
		return nil
	}
	return bytes.Clone(acct)
}
