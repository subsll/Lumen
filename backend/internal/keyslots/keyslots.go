// Package keyslots protects the vault master key with hardware security keys.
//
// The master key (KEK) is never stored in the clear. Instead it is wrapped
// once per "slot". Each slot has a high-entropy secret that only its owner
// can produce:
//
//   - webauthn: the 32-byte output of the WebAuthn PRF extension (CTAP2
//     hmac-secret) for one credential and the slot's salt. Only the physical
//     security key that holds the credential can compute it.
//   - recovery: a random 160-bit recovery code shown to the user once.
//
// Wrapping key = HKDF-SHA256(secret, salt, "lumen keyslot v1 <type>"), and the
// KEK is sealed with AES-256-GCM using the slot ID as associated data, so a
// wrapped key cannot be moved to another slot.
package keyslots

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	TypeWebAuthn = "webauthn"
	TypeRecovery = "recovery"

	SecretSize = 32
	SaltSize   = 32
	KeySize    = 32
)

var (
	// ErrWrongSecret means the secret does not open the slot.
	ErrWrongSecret = errors.New("keyslots: secret does not unlock this slot")
	ErrNoSlot      = errors.New("keyslots: no such slot")
)

// Slot is one wrapped copy of the master key.
type Slot struct {
	ID           string     `json:"id"`
	Type         string     `json:"type"`
	Label        string     `json:"label"`
	CredentialID string     `json:"credential_id,omitempty"` // base64url, webauthn only
	RPID         string     `json:"rp_id,omitempty"`         // WebAuthn relying party (hostname) the key was registered on
	Salt         string     `json:"salt"`                    // base64url
	Nonce        string     `json:"nonce"`                   // base64url
	WrappedKey   string     `json:"wrapped_key"`             // base64url
	CreatedAt    time.Time  `json:"created_at"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
}

// Public is the part of a slot that is safe to show before unlocking.
type Public struct {
	ID           string     `json:"id"`
	Type         string     `json:"type"`
	Label        string     `json:"label"`
	CredentialID string     `json:"credential_id,omitempty"`
	RPID         string     `json:"rp_id,omitempty"`
	Salt         string     `json:"salt,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
}

func (s Slot) Public() Public {
	p := Public{ID: s.ID, Type: s.Type, Label: s.Label, CredentialID: s.CredentialID, RPID: s.RPID,
		CreatedAt: s.CreatedAt, LastUsedAt: s.LastUsedAt}
	if s.Type == TypeWebAuthn {
		p.Salt = s.Salt // the browser needs it as the PRF input
	}
	return p
}

var b64 = base64.RawURLEncoding

func wrappingKey(secret, salt []byte, typ string) ([]byte, error) {
	return hkdf.Key(sha256.New, secret, salt, "lumen keyslot v1 "+typ, KeySize)
}

func gcm(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// New wraps kek into a new slot. salt may be nil to generate one.
func New(typ, label string, kek, secret, salt []byte) (Slot, error) {
	if typ != TypeWebAuthn && typ != TypeRecovery {
		return Slot{}, fmt.Errorf("keyslots: unknown slot type %q", typ)
	}
	if len(kek) != KeySize {
		return Slot{}, errors.New("keyslots: master key must be 32 bytes")
	}
	if len(secret) < SecretSize {
		return Slot{}, errors.New("keyslots: secret must be at least 32 bytes")
	}
	if salt == nil {
		salt = make([]byte, SaltSize)
		rand.Read(salt)
	}
	if len(salt) != SaltSize {
		return Slot{}, errors.New("keyslots: salt must be 32 bytes")
	}
	s := Slot{ID: uuid.NewString(), Type: typ, Label: label, Salt: b64.EncodeToString(salt), CreatedAt: time.Now().UTC()}
	wk, err := wrappingKey(secret, salt, typ)
	if err != nil {
		return Slot{}, err
	}
	aead, err := gcm(wk)
	if err != nil {
		return Slot{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	rand.Read(nonce)
	s.Nonce = b64.EncodeToString(nonce)
	s.WrappedKey = b64.EncodeToString(aead.Seal(nil, nonce, kek, []byte(s.ID)))
	return s, nil
}

// Open unwraps the master key from s using secret.
func (s Slot) Open(secret []byte) ([]byte, error) {
	salt, err1 := b64.DecodeString(s.Salt)
	nonce, err2 := b64.DecodeString(s.Nonce)
	wrapped, err3 := b64.DecodeString(s.WrappedKey)
	if err := errors.Join(err1, err2, err3); err != nil {
		return nil, fmt.Errorf("keyslots: corrupt slot %s: %w", s.ID, err)
	}
	wk, err := wrappingKey(secret, salt, s.Type)
	if err != nil {
		return nil, err
	}
	aead, err := gcm(wk)
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, fmt.Errorf("keyslots: corrupt slot %s", s.ID)
	}
	kek, err := aead.Open(nil, nonce, wrapped, []byte(s.ID))
	if err != nil {
		return nil, ErrWrongSecret
	}
	return kek, nil
}

// ---- recovery codes ----

var codeEnc = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewRecoveryCode returns a human-friendly code (8 groups of 4 characters,
// 160 bits) and the secret derived from it.
func NewRecoveryCode() (code string, secret []byte) {
	raw := make([]byte, 20)
	rand.Read(raw)
	s := codeEnc.EncodeToString(raw) // 32 chars
	groups := make([]string, 0, 8)
	for i := 0; i < len(s); i += 4 {
		groups = append(groups, s[i:i+4])
	}
	code = strings.Join(groups, "-")
	secret, _ = RecoverySecret(code)
	return code, secret
}

// RecoverySecret normalises a typed recovery code and turns it into a slot
// secret. Case, spaces and dashes are ignored.
func RecoverySecret(code string) ([]byte, error) {
	norm := strings.Map(func(r rune) rune {
		switch {
		case r == '-' || r == ' ' || r == '\t' || r == '\n':
			return -1
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		}
		return r
	}, code)
	raw, err := codeEnc.DecodeString(norm)
	if err != nil || len(raw) != 20 {
		return nil, errors.New("keyslots: malformed recovery code")
	}
	sum := sha256.Sum256(append([]byte("lumen recovery code v1\x00"), raw...))
	return sum[:], nil
}

// ---- persistence ----

type file struct {
	Version int    `json:"version"`
	Slots   []Slot `json:"slots"`
}

// Load reads the slot file. A missing file means no slots.
func Load(path string) ([]Slot, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("keyslots: %s: %w", path, err)
	}
	if f.Version != 1 {
		return nil, fmt.Errorf("keyslots: %s: unsupported version %d", path, f.Version)
	}
	return f.Slots, nil
}

// Save atomically replaces the slot file.
func Save(path string, slots []Slot) error {
	b, err := json.MarshalIndent(file{Version: 1, Slots: slots}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".keyslots-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
