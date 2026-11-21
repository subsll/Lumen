package vaultcrypto

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
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

func TestRewrap(t *testing.T) {
	oldKEK, newKEK := make([]byte, 32), make([]byte, 32)
	rand.Read(oldKEK)
	rand.Read(newKEK)
	plain := bytes.Repeat([]byte("evidence "), 20000) // several chunks
	path := filepath.Join(t.TempDir(), "blob")
	f, _ := os.Create(path)
	if err := Encrypt(f, bytes.NewReader(plain), oldKEK); err != nil {
		t.Fatal(err)
	}
	f.Close()
	before, _ := os.ReadFile(path)

	f, _ = os.OpenFile(path, os.O_RDWR, 0)
	changed, err := Rewrap(f, oldKEK, newKEK)
	if err != nil || !changed {
		t.Fatalf("rewrap: %v %v", changed, err)
	}
	if changed, err := Rewrap(f, oldKEK, newKEK); err != nil || changed {
		t.Fatalf("second rewrap should be a no-op: %v %v", changed, err)
	}
	f.Close()
	after, _ := os.ReadFile(path)
	if len(after) != len(before) || !bytes.Equal(after[HeaderSize:], before[HeaderSize:]) {
		t.Fatal("payload changed")
	}

	var out bytes.Buffer
	if err := Decrypt(&out, bytes.NewReader(after), newKEK); err != nil || !bytes.Equal(out.Bytes(), plain) {
		t.Fatalf("new key: %v", err)
	}
	if err := Decrypt(io.Discard, bytes.NewReader(after), oldKEK); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("old key still decrypts: %v", err)
	}
	f, _ = os.OpenFile(path, os.O_RDWR, 0)
	defer f.Close()
	if _, err := Rewrap(f, make([]byte, 32), make([]byte, 32)); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("unrelated keys: %v", err)
	}
}
