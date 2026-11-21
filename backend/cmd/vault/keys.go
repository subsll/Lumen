package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lumenforensics/lumen/internal/keyslots"
	"github.com/lumenforensics/lumen/internal/vaultcrypto"
)

// keyState holds the master key while the vault is unlocked.
//
// Without key slots the vault behaves as before: the master key comes from
// LUMEN_MASTER_KEY and the vault is always unlocked.
//
// Registering the first security key replaces the master key: a new random
// key is generated, wrapped only by the security key and a recovery code, and
// every stored blob's data key is re-wrapped under it (see rotate). From then
// on the vault starts locked on every boot, and LUMEN_MASTER_KEY, or any copy
// of it, decrypts nothing.
type keyState struct {
	mu       sync.Mutex
	path     string // keyslots.json
	blobs    string // blob directory
	marker   string // exists while a rotation is unfinished
	slots    []keyslots.Slot
	master   []byte // nil while locked
	legacy   []byte // previous master key, only while a rotation is unfinished
	rotating bool
	envKey   bool // LUMEN_MASTER_KEY is (still) configured
}

func loadKeyState(path string) (*keyState, error) {
	slots, err := keyslots.Load(path)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	envKey, envErr := loadKey()
	ks := &keyState{path: path, blobs: filepath.Join(dir, "blobs"), marker: filepath.Join(dir, "rotation.pending"),
		slots: slots, envKey: envErr == nil}
	_, markerErr := os.Stat(ks.marker)
	ks.rotating = markerErr == nil
	if len(slots) == 0 {
		if ks.rotating {
			os.Remove(ks.marker) // interrupted before any slot was saved: nothing changed
			ks.rotating = false
		}
		if envErr != nil {
			return nil, envErr
		}
		ks.master = envKey
		return ks, nil
	}
	switch {
	case ks.rotating && envErr == nil:
		ks.legacy = envKey
		slog.Warn("re-encryption under the new master key was interrupted; it finishes on the next unlock. Keep LUMEN_MASTER_KEY in .env until then")
	case ks.rotating:
		slog.Error("re-encryption under the new master key was interrupted and LUMEN_MASTER_KEY is gone: files not yet re-encrypted cannot be read until it is restored")
	case envErr == nil:
		zero(envKey)
		slog.Info("LUMEN_MASTER_KEY is set but no longer used: it does not decrypt this vault. You can remove it from .env")
	}
	slog.Info("vault locked: unlock it with a security key in the Lumen web app", "slots", len(slots))
	return ks, nil
}

// rotate re-wraps every blob still under k.legacy to k.master, then clears the
// pending marker. It is idempotent and safe to repeat after a crash. Callers hold k.mu.
func (k *keyState) rotate() error {
	if !k.rotating || k.master == nil {
		return nil
	}
	if k.legacy == nil {
		return errors.New("the previous master key is needed to finish re-encryption: put LUMEN_MASTER_KEY back in .env and restart")
	}
	start := time.Now()
	var done, failed int
	err := filepath.WalkDir(k.blobs, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".lmv") {
			return err
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
		defer os.Chmod(path, 0o400)
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return err
		}
		defer f.Close()
		changed, err := vaultcrypto.Rewrap(f, k.legacy, k.master)
		if err != nil {
			failed++
			slog.Error("cannot re-encrypt blob", "path", path, "err", err)
			return nil
		}
		if changed {
			done++
			return f.Sync()
		}
		return nil
	})
	if err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("%d stored file(s) could not be re-encrypted; see the vault log", failed)
	}
	if err := os.Remove(k.marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	zero(k.legacy)
	k.legacy, k.rotating = nil, false
	slog.Info("master key replaced: stored files re-encrypted", "files", done, "ms", time.Since(start).Milliseconds())
	return nil
}

// decrypt decrypts f with the master key, falling back to the previous key
// for blobs a rotation has not reached yet.
func (v *vault) decrypt(w io.Writer, f *os.File, kek []byte) error {
	err := vaultcrypto.Decrypt(w, f, kek)
	if !errors.Is(err, vaultcrypto.ErrWrongKey) {
		return err
	}
	v.keys.mu.Lock()
	legacy := slices.Clone(v.keys.legacy)
	v.keys.mu.Unlock()
	if legacy == nil {
		return err
	}
	defer zero(legacy)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return vaultcrypto.Decrypt(w, f, legacy)
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func (k *keyState) protected() bool { return len(k.slots) > 0 }

// kek returns a copy of the master key, or writes 423 Locked.
func (v *vault) kek(w http.ResponseWriter) ([]byte, bool) {
	v.keys.mu.Lock()
	defer v.keys.mu.Unlock()
	if v.keys.master == nil {
		jsonErr(w, http.StatusLocked, "vault is locked")
		return nil, false
	}
	return slices.Clone(v.keys.master), true
}

func jsonErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(v); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func b64(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

type keyStatus struct {
	Protected       bool              `json:"protected"`
	Sealed          bool              `json:"sealed"`
	EnvKeyPresent   bool              `json:"env_key_present"`
	RotationPending bool              `json:"rotation_pending"`
	Slots           []keyslots.Public `json:"slots"`
}

func (k *keyState) status() keyStatus {
	st := keyStatus{Protected: k.protected(), Sealed: k.master == nil, EnvKeyPresent: k.envKey, RotationPending: k.rotating,
		Slots: []keyslots.Public{}}
	for _, s := range k.slots {
		st.Slots = append(st.Slots, s.Public())
	}
	return st
}

func (v *vault) keyStatus(w http.ResponseWriter, r *http.Request) {
	v.keys.mu.Lock()
	defer v.keys.mu.Unlock()
	writeJSON(w, v.keys.status())
}

// commit persists slots and swaps them in only if the write succeeded.
func (k *keyState) commit(slots []keyslots.Slot) error {
	if err := keyslots.Save(k.path, slots); err != nil {
		return err
	}
	k.slots = slots
	return nil
}

func (v *vault) unlock(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CredentialID string `json:"credential_id"`
		Secret       string `json:"secret"` // base64url PRF output
		RecoveryCode string `json:"recovery_code"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	k := v.keys
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.protected() {
		jsonErr(w, http.StatusConflict, "no security keys are enrolled")
		return
	}

	var secret []byte
	var candidates []int
	if in.RecoveryCode != "" {
		s, err := keyslots.RecoverySecret(in.RecoveryCode)
		if err != nil {
			v.unlockFailed(w, "malformed recovery code")
			return
		}
		secret = s
		for i, s := range k.slots {
			if s.Type == keyslots.TypeRecovery {
				candidates = append(candidates, i)
			}
		}
	} else {
		s, err := b64(in.Secret)
		if err != nil || len(s) != keyslots.SecretSize {
			jsonErr(w, http.StatusBadRequest, "secret must be 32 bytes, base64url")
			return
		}
		secret = s
		for i, s := range k.slots {
			if s.Type == keyslots.TypeWebAuthn && s.CredentialID == strings.TrimRight(in.CredentialID, "=") {
				candidates = append(candidates, i)
			}
		}
	}
	for _, i := range candidates {
		master, err := k.slots[i].Open(secret)
		if err != nil {
			continue
		}
		if k.master != nil {
			zero(k.master)
		}
		k.master = master
		slots := slices.Clone(k.slots)
		now := time.Now().UTC()
		slots[i].LastUsedAt = &now
		if err := k.commit(slots); err != nil {
			slog.Warn("could not record key use", "err", err)
		}
		slog.Info("vault unlocked", "slot", k.slots[i].ID, "type", k.slots[i].Type)
		if err := k.rotate(); err != nil {
			slog.Error("finishing re-encryption", "err", err)
		}
		writeJSON(w, map[string]any{"slot": k.slots[i].Public()})
		return
	}
	v.unlockFailed(w, "this key or code does not unlock the vault")
}

func (v *vault) unlockFailed(w http.ResponseWriter, msg string) {
	slog.Warn("vault unlock failed", "reason", msg)
	time.Sleep(time.Second) // slow down guessing; the lock is held, so attempts are serialised
	jsonErr(w, http.StatusUnauthorized, msg)
}

func (v *vault) seal(w http.ResponseWriter, r *http.Request) {
	k := v.keys
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.protected() {
		jsonErr(w, http.StatusConflict, "no security keys are enrolled, so the vault cannot be locked")
		return
	}
	if k.master != nil {
		zero(k.master)
		k.master = nil
		slog.Info("vault locked")
	}
	writeJSON(w, k.status())
}

func newRecoverySlot(master []byte) (keyslots.Slot, string, error) {
	code, secret := keyslots.NewRecoveryCode()
	s, err := keyslots.New(keyslots.TypeRecovery, "Recovery code", master, secret, nil)
	return s, code, err
}

func (v *vault) addSlot(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Label        string `json:"label"`
		CredentialID string `json:"credential_id"`
		RPID         string `json:"rp_id"`
		Salt         string `json:"salt"`
		Secret       string `json:"secret"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	in.Label = strings.TrimSpace(in.Label)
	if in.Label == "" {
		in.Label = "Security key"
	}
	in.CredentialID = strings.TrimRight(in.CredentialID, "=")
	salt, err1 := b64(in.Salt)
	secret, err2 := b64(in.Secret)
	switch {
	case utf8.RuneCountInString(in.Label) > 64:
		jsonErr(w, http.StatusBadRequest, "label is too long (64 characters at most)")
		return
	case in.CredentialID == "" || len(in.CredentialID) > 1024:
		jsonErr(w, http.StatusBadRequest, "credential_id is required")
		return
	case err1 != nil || len(salt) != keyslots.SaltSize:
		jsonErr(w, http.StatusBadRequest, "salt must be 32 bytes, base64url")
		return
	case err2 != nil || len(secret) != keyslots.SecretSize:
		jsonErr(w, http.StatusBadRequest, "secret must be 32 bytes, base64url")
		return
	}

	k := v.keys
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.master == nil {
		jsonErr(w, http.StatusLocked, "vault is locked")
		return
	}
	for _, s := range k.slots {
		if s.CredentialID == in.CredentialID {
			jsonErr(w, http.StatusConflict, "this security key is already registered")
			return
		}
	}
	// The first key replaces the master key, so the one in .env stops working.
	first := !k.protected()
	master := k.master
	if first {
		master = make([]byte, keyslots.KeySize)
		if _, err := rand.Read(master); err != nil {
			httpErr(w, err, http.StatusInternalServerError)
			return
		}
	}
	slot, err := keyslots.New(keyslots.TypeWebAuthn, in.Label, master, secret, salt)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	slot.CredentialID, slot.RPID = in.CredentialID, in.RPID
	// Prove the slot opens before relying on it.
	if got, err := slot.Open(secret); err != nil || !slices.Equal(got, master) {
		jsonErr(w, http.StatusInternalServerError, "slot self-check failed")
		return
	}

	slots := append(slices.Clone(k.slots), slot)
	out := map[string]any{"slot": slot.Public()}
	if !slices.ContainsFunc(k.slots, func(s keyslots.Slot) bool { return s.Type == keyslots.TypeRecovery }) {
		rec, code, err := newRecoverySlot(master)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		slots = append(slots, rec)
		out["recovery_code"] = code
	}
	if first {
		// Order matters for crash safety: marker, then the new slots, then re-encryption.
		if err := writeMarker(k.marker); err != nil {
			httpErr(w, err, http.StatusInternalServerError)
			return
		}
	}
	if err := k.commit(slots); err != nil {
		if first {
			os.Remove(k.marker)
		}
		httpErr(w, err, http.StatusInternalServerError)
		return
	}
	slog.Info("security key enrolled", "slot", slot.ID, "label", slot.Label)
	if first {
		k.legacy, k.master, k.rotating = k.master, master, true
		if err := k.rotate(); err != nil {
			// The key and recovery code are saved and must reach the user; re-encryption resumes on the next unlock.
			slog.Error("re-encrypting under the new master key", "err", err)
			out["rotation_error"] = err.Error()
		}
	}
	writeJSON(w, out)
}

func (v *vault) removeSlot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	k := v.keys
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.master == nil {
		jsonErr(w, http.StatusLocked, "vault is locked")
		return
	}
	i := slices.IndexFunc(k.slots, func(s keyslots.Slot) bool { return s.ID == id })
	if i < 0 {
		jsonErr(w, http.StatusNotFound, "no such key")
		return
	}
	victim := k.slots[i]
	if victim.Type == keyslots.TypeRecovery {
		jsonErr(w, http.StatusConflict, "the recovery code cannot be removed, only replaced")
		return
	}
	hw := 0
	for _, s := range k.slots {
		if s.Type == keyslots.TypeWebAuthn {
			hw++
		}
	}
	if hw <= 1 {
		jsonErr(w, http.StatusConflict, "this is the last security key; register another one before removing it")
		return
	}
	if err := k.commit(slices.Delete(slices.Clone(k.slots), i, i+1)); err != nil {
		httpErr(w, err, http.StatusInternalServerError)
		return
	}
	slog.Info("security key removed", "slot", victim.ID, "label", victim.Label)
	writeJSON(w, map[string]any{"slot": victim.Public()})
}

func (v *vault) rotateRecovery(w http.ResponseWriter, r *http.Request) {
	k := v.keys
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.master == nil {
		jsonErr(w, http.StatusLocked, "vault is locked")
		return
	}
	if !k.protected() {
		jsonErr(w, http.StatusConflict, "register a security key first")
		return
	}
	rec, code, err := newRecoverySlot(k.master)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	slots := slices.DeleteFunc(slices.Clone(k.slots), func(s keyslots.Slot) bool { return s.Type == keyslots.TypeRecovery })
	if err := k.commit(append(slots, rec)); err != nil {
		httpErr(w, err, http.StatusInternalServerError)
		return
	}
	slog.Info("recovery code replaced")
	writeJSON(w, map[string]any{"slot": rec.Public(), "recovery_code": code})
}

func writeMarker(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(time.Now().UTC().Format(time.RFC3339) + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
