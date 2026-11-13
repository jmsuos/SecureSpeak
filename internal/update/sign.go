package update

import (
	"crypto/ed25519"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

//go:embed signing-keys.pem
var signingKeysPEM []byte

// TrustedKeys are the release signing keys built into this binary.
var TrustedKeys = mustParseKeys(signingKeysPEM)

func mustParseKeys(b []byte) []ed25519.PublicKey {
	k, err := ParseKeys(b)
	if err != nil {
		panic("update: bad signing-keys.pem: " + err.Error())
	}
	return k
}

// ParseKeys reads Ed25519 public keys ("-----BEGIN PUBLIC KEY-----" blocks).
// Text outside the blocks (comments) is ignored.
func ParseKeys(b []byte) ([]ed25519.PublicKey, error) {
	var keys []ed25519.PublicKey
	for {
		var blk *pem.Block
		blk, b = pem.Decode(b)
		if blk == nil {
			return keys, nil
		}
		if blk.Type != "PUBLIC KEY" {
			return nil, fmt.Errorf("unexpected PEM block %q (only public keys belong here)", blk.Type)
		}
		k, err := x509.ParsePKIXPublicKey(blk.Bytes)
		if err != nil {
			return nil, err
		}
		ek, ok := k.(ed25519.PublicKey)
		if !ok {
			return nil, errors.New("signing key is not an Ed25519 key")
		}
		keys = append(keys, ek)
	}
}

// VerifySums checks a base64 Ed25519 signature over the SHA256SUMS bytes
// against any of the keys.
func VerifySums(sums, sig []byte, keys []ed25519.PublicKey) error {
	if len(keys) == 0 {
		return errors.New("no signing keys configured")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return errors.New("the release signature is malformed")
	}
	for _, k := range keys {
		if ed25519.Verify(k, sums, raw) {
			return nil
		}
	}
	return errors.New("the release signature is not valid: this update was not signed by the SecureSpeak developers")
}
