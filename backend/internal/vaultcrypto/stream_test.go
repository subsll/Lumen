package vaultcrypto

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	kek := make([]byte, 32)
	rand.Read(kek)
	for _, size := range []int{0, 1, ChunkSize - 1, ChunkSize, ChunkSize + 1, 3*ChunkSize + 17} {
		pt := make([]byte, size)
		rand.Read(pt)
		var enc, dec bytes.Buffer
		if err := Encrypt(&enc, bytes.NewReader(pt), kek); err != nil {
			t.Fatal(err)
		}
		if err := Decrypt(&dec, bytes.NewReader(enc.Bytes()), kek); err != nil {
			t.Fatalf("size %d: %v", size, err)
		}
		if !bytes.Equal(pt, dec.Bytes()) {
			t.Fatalf("size %d: mismatch", size)
		}
	}
}

func TestTamperAndTruncation(t *testing.T) {
	kek := make([]byte, 32)
	rand.Read(kek)
	pt := make([]byte, 2*ChunkSize+5)
	rand.Read(pt)
	var enc bytes.Buffer
	Encrypt(&enc, bytes.NewReader(pt), kek)
	blob := enc.Bytes()

	flipped := append([]byte(nil), blob...)
	flipped[len(flipped)/2] ^= 0xff
	if err := Decrypt(&bytes.Buffer{}, bytes.NewReader(flipped), kek); err == nil {
		t.Fatal("tampered blob decrypted")
	}
	// Cut after the first full chunk: must be rejected as truncated.
	cut := blob[8+12+48+5+ChunkSize+16:]
	cut = blob[:len(blob)-len(cut)]
	if err := Decrypt(&bytes.Buffer{}, bytes.NewReader(cut), kek); err == nil {
		t.Fatal("truncated blob decrypted")
	}
	wrong := make([]byte, 32)
	if err := Decrypt(&bytes.Buffer{}, bytes.NewReader(blob), wrong); err == nil {
		t.Fatal("wrong key decrypted")
	}
}
