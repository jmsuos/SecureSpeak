// Package e2e implements SecureSpeak's end-to-end encryption.
//
// Design
//
//   - Every client has a long-term Ed25519 identity key (stored locally, never an
//     account). Its fingerprint is what users compare to verify each other.
//   - Every connection generates a fresh X25519 ephemeral key, signed by the
//     identity key. Peers verify the signature before using it.
//   - Pairwise keys: X25519(ephA, ephB) -> HKDF-SHA256 -> 256-bit key. Because the
//     X25519 keys are per session, past sessions stay safe if an identity key
//     leaks later (forward secrecy between sessions).
//   - Text: each message gets a random AES-256 key; the body is AES-256-GCM
//     encrypted once and the message key is wrapped for each recipient with the
//     pairwise key. Wraps are bound to the ciphertext hash and recipient.
//   - Voice: each speaker has a random AES-256-GCM "sender key", distributed to
//     channel members over pairwise encryption and rotated whenever someone
//     leaves the channel, so departed members cannot decrypt later speech.
//
// The server only ever sees public keys, signatures and ciphertext.
package e2e

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

const (
	sigLabel      = "securespeak-ephemeral-v1"
	pairwiseLabel = "securespeak-pairwise-v1"
	fpLabel       = "securespeak-fingerprint-v1"
	KeySize       = 32
)

var ErrDecrypt = errors.New("decryption failed")

// ---- identity ---------------------------------------------------------------

type Identity struct {
	Priv ed25519.PrivateKey
	Pub  ed25519.PublicKey
}

func NewIdentity() *Identity {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	return &Identity{Priv: priv, Pub: pub}
}

func IdentityFromSeed(seed []byte) (*Identity, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, errors.New("bad identity seed")
	}
	priv := ed25519.NewKeyFromSeed(seed)
	return &Identity{Priv: priv, Pub: priv.Public().(ed25519.PublicKey)}, nil
}

func (i *Identity) Seed() []byte { return i.Priv.Seed() }

// Fingerprint returns a human-comparable fingerprint of an identity key,
// e.g. "3F2A 9C01 …" (10 groups of 4 hex digits = 160 bits).
func Fingerprint(pub []byte) string {
	h := sha256.Sum256(append([]byte(fpLabel), pub...))
	s := strings.ToUpper(hex.EncodeToString(h[:20]))
	var b strings.Builder
	for i := 0; i < len(s); i += 4 {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(s[i : i+4])
	}
	return b.String()
}

// ---- per-connection session keys -------------------------------------------

type Session struct {
	Identity *Identity
	eph      *ecdh.PrivateKey
	EphPub   []byte
	Sig      []byte
}

func NewSession(id *Identity) (*Session, error) {
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	pub := eph.PublicKey().Bytes()
	sig := ed25519.Sign(id.Priv, append([]byte(sigLabel), pub...))
	return &Session{Identity: id, eph: eph, EphPub: pub, Sig: sig}, nil
}

// VerifyPeer checks that a peer's ephemeral key is signed by its identity key.
func VerifyPeer(idPub, ephPub, sig []byte) bool {
	if len(idPub) != ed25519.PublicKeySize || len(ephPub) != 32 || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(idPub, append([]byte(sigLabel), ephPub...), sig)
}

// Pairwise derives the shared 256-bit key with a peer's ephemeral key.
func (s *Session) Pairwise(peerEph []byte) ([]byte, error) {
	pk, err := ecdh.X25519().NewPublicKey(peerEph)
	if err != nil {
		return nil, err
	}
	shared, err := s.eph.ECDH(pk)
	if err != nil {
		return nil, err
	}
	a, b := s.EphPub, peerEph
	if bytes.Compare(a, b) > 0 {
		a, b = b, a
	}
	info := pairwiseLabel + string(a) + string(b)
	return hkdf.Key(sha256.New, shared, nil, info, KeySize)
}

// ---- AEAD helpers -------------------------------------------------------------

func NewGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, errors.New("key must be 256 bits")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func RandomKey() []byte {
	k := make([]byte, KeySize)
	if _, err := rand.Read(k); err != nil {
		panic(err)
	}
	return k
}

// Seal encrypts with a random nonce and returns nonce||ciphertext.
func Seal(key, plaintext, aad []byte) ([]byte, error) {
	g, err := NewGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return g.Seal(nonce, nonce, plaintext, aad), nil
}

func Open(key, data, aad []byte) ([]byte, error) {
	g, err := NewGCM(key)
	if err != nil {
		return nil, err
	}
	if len(data) < g.NonceSize()+g.Overhead() {
		return nil, ErrDecrypt
	}
	pt, err := g.Open(nil, data[:g.NonceSize()], data[g.NonceSize():], aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

// ---- text messages ----------------------------------------------------------

// ChatAAD binds a message to its kind, target and sender.
func ChatAAD(kind string, target, from uint32) []byte {
	b := []byte("securespeak-chat-v1|" + kind + "|")
	b = binary.BigEndian.AppendUint32(b, target)
	b = binary.BigEndian.AppendUint32(b, from)
	return b
}

func wrapAAD(nonce, ct []byte, recipient uint32) []byte {
	h := sha256.New()
	h.Write(nonce)
	h.Write(ct)
	b := append([]byte("securespeak-wrap-v1"), h.Sum(nil)...)
	return binary.BigEndian.AppendUint32(b, recipient)
}

// EncryptMessage encrypts body once and wraps the message key for every recipient.
// recipients maps client id -> pairwise key.
func EncryptMessage(body, aad []byte, recipients map[uint32][]byte) (nonce, ct []byte, wraps map[string][]byte, err error) {
	k := RandomKey()
	g, err := NewGCM(k)
	if err != nil {
		return nil, nil, nil, err
	}
	nonce = make([]byte, g.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return
	}
	ct = g.Seal(nil, nonce, body, aad)
	wraps = make(map[string][]byte, len(recipients))
	for id, pk := range recipients {
		w, err := Seal(pk, k, wrapAAD(nonce, ct, id))
		if err != nil {
			return nil, nil, nil, err
		}
		wraps[strconv.FormatUint(uint64(id), 10)] = w
	}
	return nonce, ct, wraps, nil
}

func DecryptMessage(nonce, ct, aad, wrap, pairwise []byte, myID uint32) ([]byte, error) {
	k, err := Open(pairwise, wrap, wrapAAD(nonce, ct, myID))
	if err != nil {
		return nil, err
	}
	g, err := NewGCM(k)
	if err != nil {
		return nil, err
	}
	if len(nonce) != g.NonceSize() {
		return nil, ErrDecrypt
	}
	pt, err := g.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

// ---- voice sender keys -------------------------------------------------------

type SenderKey struct {
	ID   byte
	Key  []byte
	aead cipher.AEAD
}

func NewSenderKey(id byte) *SenderKey {
	k, err := SenderKeyFrom(id, RandomKey())
	if err != nil {
		panic(err)
	}
	return k
}

func SenderKeyFrom(id byte, key []byte) (*SenderKey, error) {
	g, err := NewGCM(key)
	if err != nil {
		return nil, err
	}
	return &SenderKey{ID: id, Key: append([]byte(nil), key...), aead: g}, nil
}

// VoiceHeaderLen is the size of [keyID][counter u64].
const VoiceHeaderLen = 9

func voiceNonce(keyID byte, ctr uint64) []byte {
	n := make([]byte, 12)
	n[0] = keyID
	binary.BigEndian.PutUint64(n[4:], ctr)
	return n
}

func voiceAAD(sender uint32, hdr []byte) []byte {
	b := binary.BigEndian.AppendUint32(make([]byte, 0, 4+len(hdr)), sender)
	return append(b, hdr...)
}

// SealVoice returns [keyID][ctr][ciphertext]. The counter must never repeat
// for the same key.
func (k *SenderKey) SealVoice(sender uint32, ctr uint64, plain []byte) []byte {
	out := make([]byte, VoiceHeaderLen, VoiceHeaderLen+len(plain)+16)
	out[0] = k.ID
	binary.BigEndian.PutUint64(out[1:], ctr)
	return k.aead.Seal(out, voiceNonce(k.ID, ctr), plain, voiceAAD(sender, out[:VoiceHeaderLen]))
}

// ParseVoice splits an inner voice payload (without sender id).
func ParseVoice(p []byte) (keyID byte, ctr uint64, ok bool) {
	if len(p) < VoiceHeaderLen+16 {
		return 0, 0, false
	}
	return p[0], binary.BigEndian.Uint64(p[1:9]), true
}

func (k *SenderKey) OpenVoice(sender uint32, p []byte) ([]byte, error) {
	if len(p) < VoiceHeaderLen+16 || p[0] != k.ID {
		return nil, ErrDecrypt
	}
	ctr := binary.BigEndian.Uint64(p[1:9])
	pt, err := k.aead.Open(nil, voiceNonce(k.ID, ctr), p[VoiceHeaderLen:], voiceAAD(sender, p[:VoiceHeaderLen]))
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

func keyDistAAD(from, to uint32) []byte {
	b := []byte("securespeak-keydist-v1")
	b = binary.BigEndian.AppendUint32(b, from)
	return binary.BigEndian.AppendUint32(b, to)
}

// WrapSenderKey encrypts a sender key for one recipient.
func WrapSenderKey(pairwise []byte, k *SenderKey, from, to uint32) ([]byte, error) {
	return Seal(pairwise, append([]byte{k.ID}, k.Key...), keyDistAAD(from, to))
}

func UnwrapSenderKey(pairwise, blob []byte, from, to uint32) (*SenderKey, error) {
	pt, err := Open(pairwise, blob, keyDistAAD(from, to))
	if err != nil {
		return nil, err
	}
	if len(pt) != 1+KeySize {
		return nil, ErrDecrypt
	}
	return SenderKeyFrom(pt[0], pt[1:])
}

// ---- voice signatures (anti-spoofing) -------------------------------------------
//
// Every voice packet is signed with the speaker's Ed25519 identity key after
// encryption. Holding someone's AES sender key (every channel member does) is
// therefore not enough to inject audio in their name, and neither is control
// of the server.

const VoiceSigLen = ed25519.SignatureSize

func voiceSigMsg(sender uint32, inner []byte) []byte {
	b := make([]byte, 0, 32+len(inner))
	b = append(b, "securespeak-voice-sig-v1"...)
	b = binary.BigEndian.AppendUint32(b, sender)
	return append(b, inner...)
}

// SignVoice appends a signature to an encrypted voice payload.
func SignVoice(id *Identity, sender uint32, inner []byte) []byte {
	sig := ed25519.Sign(id.Priv, voiceSigMsg(sender, inner))
	return append(inner, sig...)
}

// VerifyVoice checks the signature and returns the encrypted payload.
func VerifyVoice(idPub []byte, sender uint32, signed []byte) ([]byte, bool) {
	if len(signed) < VoiceHeaderLen+16+VoiceSigLen || len(idPub) != ed25519.PublicKeySize {
		return nil, false
	}
	inner := signed[:len(signed)-VoiceSigLen]
	if !ed25519.Verify(idPub, voiceSigMsg(sender, inner), signed[len(inner):]) {
		return nil, false
	}
	return inner, true
}

// ---- files -----------------------------------------------------------------------

func fileNonce(idx uint32) []byte {
	n := make([]byte, 12)
	binary.BigEndian.PutUint32(n[8:], idx)
	return n
}

// FileCipher encrypts the chunks of one transfer (one random key per transfer
// and recipient, so nonces derived from the chunk index never repeat).
type FileCipher struct{ aead cipher.AEAD }

func NewFileCipher(key []byte) (*FileCipher, error) {
	g, err := NewGCM(key)
	if err != nil {
		return nil, err
	}
	return &FileCipher{g}, nil
}

func (f *FileCipher) Seal(id string, idx uint32, data []byte) []byte {
	return f.aead.Seal(nil, fileNonce(idx), data, []byte("securespeak-file-v1|"+id))
}

func (f *FileCipher) Open(id string, idx uint32, ct []byte) ([]byte, error) {
	pt, err := f.aead.Open(nil, fileNonce(idx), ct, []byte("securespeak-file-v1|"+id))
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

// CtlAAD binds file-control messages to sender and recipient.
func CtlAAD(from, to uint32) []byte {
	b := []byte("securespeak-filectl-v1")
	b = binary.BigEndian.AppendUint32(b, from)
	return binary.BigEndian.AppendUint32(b, to)
}

// ---- connection authentication -----------------------------------------------------

const authLabel = "securespeak-auth-v1"

// AuthSign proves possession of the identity key for one TLS connection.
// ekm is keying material exported from that connection (RFC 5705/8446), so
// the signature is useless on any other connection.
func AuthSign(id *Identity, ekm []byte) []byte {
	return ed25519.Sign(id.Priv, append([]byte(authLabel), ekm...))
}

func AuthVerify(idPub, ekm, sig []byte) bool {
	if len(idPub) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize || len(ekm) < 32 {
		return false
	}
	return ed25519.Verify(idPub, append([]byte(authLabel), ekm...), sig)
}
