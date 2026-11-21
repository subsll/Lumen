package keyslots

import (
	"bytes"
	"crypto/rand"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func random(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func TestWrapUnwrap(t *testing.T) {
	kek, secret := random(32), random(32)
	s, err := New(TypeWebAuthn, "YubiKey 5C", kek, secret, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Open(secret)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, kek) {
		t.Fatal("unwrapped key differs")
	}
}

func TestWrongSecret(t *testing.T) {
	kek := random(32)
	s, _ := New(TypeWebAuthn, "k", kek, random(32), nil)
	if _, err := s.Open(random(32)); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("want ErrWrongSecret, got %v", err)
	}
}

func TestSlotBoundToID(t *testing.T) {
	kek, secret := random(32), random(32)
	s, _ := New(TypeWebAuthn, "k", kek, secret, nil)
	s.ID = "00000000-0000-0000-0000-000000000000" // moving a wrapped key to another slot must fail
	if _, err := s.Open(secret); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("want ErrWrongSecret, got %v", err)
	}
}

func TestSlotBoundToType(t *testing.T) {
	kek, secret := random(32), random(32)
	s, _ := New(TypeWebAuthn, "k", kek, secret, nil)
	s.Type = TypeRecovery
	if _, err := s.Open(secret); !errors.Is(err, ErrWrongSecret) {
		t.Fatalf("want ErrWrongSecret, got %v", err)
	}
}

func TestRejectsShortInputs(t *testing.T) {
	if _, err := New(TypeWebAuthn, "k", random(16), random(32), nil); err == nil {
		t.Fatal("accepted a short master key")
	}
	if _, err := New(TypeWebAuthn, "k", random(32), random(16), nil); err == nil {
		t.Fatal("accepted a short secret")
	}
	if _, err := New(TypeWebAuthn, "k", random(32), random(32), random(8)); err == nil {
		t.Fatal("accepted a short salt")
	}
	if _, err := New("password", "k", random(32), random(32), nil); err == nil {
		t.Fatal("accepted an unknown type")
	}
}

func TestRecoveryCode(t *testing.T) {
	kek := random(32)
	code, secret := NewRecoveryCode()
	if len(strings.Split(code, "-")) != 8 {
		t.Fatalf("unexpected code format %q", code)
	}
	s, err := New(TypeRecovery, "Recovery code", kek, secret, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Users may type it in lower case, with spaces instead of dashes.
	typed := strings.ToLower(strings.ReplaceAll(code, "-", " "))
	sec, err := RecoverySecret(typed)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Open(sec)
	if err != nil || !bytes.Equal(got, kek) {
		t.Fatalf("recovery code did not unlock: %v", err)
	}
	if _, err := RecoverySecret("not-a-code"); err == nil {
		t.Fatal("accepted malformed code")
	}
}

func TestPublicHidesWrappedKey(t *testing.T) {
	s, _ := New(TypeRecovery, "r", random(32), random(32), nil)
	p := s.Public()
	if p.Salt != "" {
		t.Fatal("recovery salt should not be exposed")
	}
}

func TestLoadSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keyslots.json")
	if slots, err := Load(path); err != nil || slots != nil {
		t.Fatalf("missing file: %v %v", slots, err)
	}
	kek, secret := random(32), random(32)
	s, _ := New(TypeWebAuthn, "k", kek, secret, nil)
	if err := Save(path, []Slot{s}); err != nil {
		t.Fatal(err)
	}
	slots, err := Load(path)
	if err != nil || len(slots) != 1 {
		t.Fatalf("load: %v %v", slots, err)
	}
	if got, err := slots[0].Open(secret); err != nil || !bytes.Equal(got, kek) {
		t.Fatal("round trip failed")
	}
}
