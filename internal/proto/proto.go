// Package proto defines the SecureSpeak wire protocol shared by client and server.
//
// Control traffic runs over TLS 1.3 as length-prefixed frames:
//
//	[uint32 BE length][1 byte kind][payload]
//
// kind 0 = JSON message, kind 1 = voice packet (TCP fallback when UDP is blocked).
//
// Voice normally runs over UDP. Every UDP datagram is sealed with a per-session
// transport key (AES-256-GCM) handed out over TLS; the voice payload inside is
// additionally end-to-end encrypted with the speaker's sender key, which the
// server never sees.
package proto

import (
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const (
	Version     = 3
	DefaultPort = 9987

	MaxFrameSize = 1 << 20

	FrameJSON  byte = 0
	FrameVoice byte = 1
	FrameVideo byte = 2 // screen share: [signed inner] up, [sender u32][signed inner] down

	UDPHello    byte = 1
	UDPHelloAck byte = 2
	UDPVoice    byte = 3
	UDPPing     byte = 4
	UDPPong     byte = 5

	DirClientToServer byte = 0
	DirServerToClient byte = 1

	// Voice codecs (encoding/decoding happens in the client UI layer).
	CodecADPCM byte = 1 // 24 kHz IMA-ADPCM fallback
	CodecOpus  byte = 2 // 48 kHz Opus, 20 ms frames

	// Screen share video codecs (WebCodecs in the client UI).
	CodecVP8 byte = 10
	CodecVP9 byte = 11
	// Screen share sound: 48 kHz stereo Opus, carried with the video frames.
	CodecOpusShare byte = 12

	// Voice routing modes (first byte of a client voice payload).
	VoiceChannel byte = 0 // [0][signed inner]
	VoiceWhisper byte = 1 // [1][n][n x u32 target ids][signed inner]

	MaxWhisperTargets = 64
	MaxVoicePacket    = 4096

	FileChunkSize = 48 * 1024

	// AuthEKMLabel is the TLS exporter label for identity authentication.
	AuthEKMLabel = "EXPORTER-securespeak-auth"

	MaxNickLen = 32
	MaxChatLen = 4000
)

var ErrFrameTooLarge = errors.New("frame too large")

// WriteFrame writes one frame. Callers must serialise writes themselves.
func WriteFrame(w io.Writer, kind byte, payload []byte) error {
	if len(payload) > MaxFrameSize {
		return ErrFrameTooLarge
	}
	buf := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(buf, uint32(len(payload)))
	buf[4] = kind
	copy(buf[5:], payload)
	_, err := w.Write(buf)
	return err
}

// ReadFrame reads one frame.
func ReadFrame(r io.Reader) (byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:4])
	if n > MaxFrameSize {
		return 0, nil, ErrFrameTooLarge
	}
	p := make([]byte, n)
	if _, err := io.ReadFull(r, p); err != nil {
		return 0, nil, err
	}
	return hdr[4], p, nil
}

// Message is the JSON envelope for control messages.
type Message struct {
	T string          `json:"t"`
	D json.RawMessage `json:"d,omitempty"`
}

func Encode(t string, v any) ([]byte, error) {
	var d []byte
	if v != nil {
		var err error
		if d, err = json.Marshal(v); err != nil {
			return nil, err
		}
	}
	return json.Marshal(Message{T: t, D: d})
}

// ---- UDP sealing -----------------------------------------------------------

// SealUDP builds [type][id u32][nonce 12][ciphertext]. The header is authenticated.
func SealUDP(aead cipher.AEAD, typ byte, id uint32, dir byte, ctr uint64, payload []byte) []byte {
	out := make([]byte, 17, 17+len(payload)+aead.Overhead())
	out[0] = typ
	binary.BigEndian.PutUint32(out[1:5], id)
	var nonce [12]byte
	nonce[0] = dir
	binary.BigEndian.PutUint64(nonce[4:], ctr)
	copy(out[5:17], nonce[:])
	return aead.Seal(out, nonce[:], payload, out[:5])
}

// UDPHeader parses the cleartext header of a datagram.
func UDPHeader(pkt []byte) (typ byte, id uint32, ok bool) {
	if len(pkt) < 17+16 {
		return 0, 0, false
	}
	return pkt[0], binary.BigEndian.Uint32(pkt[1:5]), true
}

// OpenUDP authenticates and decrypts a datagram, returning its counter.
func OpenUDP(aead cipher.AEAD, pkt []byte, wantDir byte) (uint64, []byte, error) {
	if len(pkt) < 17+aead.Overhead() {
		return 0, nil, errors.New("short packet")
	}
	nonce := pkt[5:17]
	if nonce[0] != wantDir {
		return 0, nil, errors.New("wrong direction")
	}
	pt, err := aead.Open(nil, nonce, pkt[17:], pkt[:5])
	if err != nil {
		return 0, nil, err
	}
	return binary.BigEndian.Uint64(nonce[4:]), pt, nil
}

// ReplayWindow rejects duplicate / very old counters (64-entry sliding window).
type ReplayWindow struct {
	max  uint64
	bits uint64
	init bool
}

func (w *ReplayWindow) Check(n uint64) bool {
	if !w.init {
		w.init, w.max, w.bits = true, n, 1
		return true
	}
	if n > w.max {
		shift := n - w.max
		if shift >= 64 {
			w.bits = 1
		} else {
			w.bits = w.bits<<shift | 1
		}
		w.max = n
		return true
	}
	off := w.max - n
	if off >= 64 {
		return false
	}
	if w.bits&(1<<off) != 0 {
		return false
	}
	w.bits |= 1 << off
	return true
}

// CertFingerprint formats the SHA-256 of a DER certificate as AA:BB:...
func CertFingerprint(der []byte) string {
	h := sha256.Sum256(der)
	s := strings.ToUpper(hex.EncodeToString(h[:]))
	var b strings.Builder
	for i := 0; i < len(s); i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(s[i : i+2])
	}
	return b.String()
}
