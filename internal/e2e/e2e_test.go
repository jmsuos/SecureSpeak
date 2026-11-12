package e2e

import (
	"bytes"
	"testing"
)

func TestPairwiseAndMessages(t *testing.T) {
	a, b, eve := NewIdentity(), NewIdentity(), NewIdentity()
	sa, _ := NewSession(a)
	sb, _ := NewSession(b)
	se, _ := NewSession(eve)
	if !VerifyPeer(b.Pub, sb.EphPub, sb.Sig) {
		t.Fatal("valid signature rejected")
	}
	if VerifyPeer(a.Pub, sb.EphPub, sb.Sig) {
		t.Fatal("signature accepted for the wrong identity")
	}
	kab, _ := sa.Pairwise(sb.EphPub)
	kba, _ := sb.Pairwise(sa.EphPub)
	if !bytes.Equal(kab, kba) || len(kab) != 32 {
		t.Fatal("pairwise keys differ")
	}
	kae, _ := sa.Pairwise(se.EphPub)

	aad := ChatAAD("channel", 7, 1)
	nonce, ct, wraps, err := EncryptMessage([]byte("hi"), aad, map[uint32][]byte{2: kab, 3: kae})
	if err != nil {
		t.Fatal(err)
	}
	pt, err := DecryptMessage(nonce, ct, aad, wraps["2"], kba, 2)
	if err != nil || string(pt) != "hi" {
		t.Fatalf("decrypt: %v", err)
	}
	// a wrap for someone else must not work for Bob
	if _, err := DecryptMessage(nonce, ct, aad, wraps["3"], kba, 2); err == nil {
		t.Fatal("decrypted with another recipient's wrap")
	}
	// tampered ciphertext / wrong sender in AAD
	ct2 := append([]byte(nil), ct...)
	ct2[0] ^= 1
	if _, err := DecryptMessage(nonce, ct2, aad, wraps["2"], kba, 2); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	if _, err := DecryptMessage(nonce, ct, ChatAAD("channel", 7, 9), wraps["2"], kba, 2); err == nil {
		t.Fatal("spoofed sender accepted")
	}
}

func TestVoiceSenderKey(t *testing.T) {
	k := NewSenderKey(5)
	pkt := k.SealVoice(42, 1000, []byte("voice"))
	pt, err := k.OpenVoice(42, pkt)
	if err != nil || string(pt) != "voice" {
		t.Fatal("voice roundtrip failed")
	}
	if _, err := k.OpenVoice(43, pkt); err == nil {
		t.Fatal("voice accepted from wrong sender id")
	}
	sa, _ := NewSession(NewIdentity())
	sb, _ := NewSession(NewIdentity())
	pk, _ := sa.Pairwise(sb.EphPub)
	blob, _ := WrapSenderKey(pk, k, 1, 2)
	k2, err := UnwrapSenderKey(pk, blob, 1, 2)
	if err != nil || k2.ID != 5 || !bytes.Equal(k2.Key, k.Key) {
		t.Fatal("sender key unwrap failed")
	}
	if _, err := UnwrapSenderKey(pk, blob, 1, 3); err == nil {
		t.Fatal("sender key accepted by wrong recipient")
	}
}

func TestVoiceSignatureStopsSpoofing(t *testing.T) {
	alice, mallory := NewIdentity(), NewIdentity()
	k := NewSenderKey(1) // Mallory is in the channel, so she knows Alice's sender key
	inner := k.SealVoice(7, 1, []byte("real"))
	signed := SignVoice(alice, 7, inner)
	if got, ok := VerifyVoice(alice.Pub, 7, signed); !ok || !bytes.Equal(got, inner) {
		t.Fatal("valid signature rejected")
	}
	// Mallory forges audio as Alice (id 7) with the shared sender key
	forged := SignVoice(mallory, 7, k.SealVoice(7, 2, []byte("fake")))
	if _, ok := VerifyVoice(alice.Pub, 7, forged); ok {
		t.Fatal("forged voice accepted")
	}
	// server relabels Alice's packet as coming from someone else
	if _, ok := VerifyVoice(alice.Pub, 8, signed); ok {
		t.Fatal("relabelled packet accepted")
	}
	tampered := append([]byte(nil), signed...)
	tampered[12] ^= 1
	if _, ok := VerifyVoice(alice.Pub, 7, tampered); ok {
		t.Fatal("tampered packet accepted")
	}
}

func TestFileCipher(t *testing.T) {
	c, _ := NewFileCipher(RandomKey())
	ct := c.Seal("f1", 3, []byte("chunk"))
	if pt, err := c.Open("f1", 3, ct); err != nil || string(pt) != "chunk" {
		t.Fatal("roundtrip")
	}
	if _, err := c.Open("f1", 4, ct); err == nil {
		t.Fatal("reordered chunk accepted")
	}
	if _, err := c.Open("f2", 3, ct); err == nil {
		t.Fatal("chunk from other file accepted")
	}
}
