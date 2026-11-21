package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lumenforensics/lumen/internal/vaultcrypto"
)

const testToken = "test-token-test-token-test-token-0123"

func rnd(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func enc(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// newTestVault boots a vault the way main does, from LUMEN_MASTER_KEY and dir.
func newTestVault(t *testing.T, dir string) http.Handler {
	_, h := bootVault(t, dir)
	return h
}

func bootVault(t *testing.T, dir string) (*vault, http.Handler) {
	t.Helper()
	for _, d := range []string{"blobs", "tmp"} {
		os.MkdirAll(filepath.Join(dir, d), 0o700)
	}
	keys, err := loadKeyState(filepath.Join(dir, "keyslots.json"))
	if err != nil {
		t.Fatal(err)
	}
	v := &vault{dir: dir, token: testToken, keys: keys}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/blobs", v.auth(v.put))
	mux.HandleFunc("GET /v1/blobs/{sha}", v.auth(v.get))
	mux.HandleFunc("GET /v1/keys", v.auth(v.keyStatus))
	mux.HandleFunc("POST /v1/keys/unlock", v.auth(v.unlock))
	mux.HandleFunc("POST /v1/keys/seal", v.auth(v.seal))
	mux.HandleFunc("POST /v1/keys/slots", v.auth(v.addSlot))
	mux.HandleFunc("DELETE /v1/keys/slots/{id}", v.auth(v.removeSlot))
	mux.HandleFunc("POST /v1/keys/recovery", v.auth(v.rotateRecovery))
	mux.HandleFunc("POST /v1/blobs/{sha}/verify", v.auth(v.verify))
	return v, mux
}

func blobPath(dir, sha string) string {
	return filepath.Join(dir, "blobs", sha[:2], sha[2:4], sha+".lmv")
}

// decryptsWith reports whether key decrypts the stored blob on disk.
func decryptsWith(t *testing.T, dir, sha string, key []byte) bool {
	t.Helper()
	f, err := os.Open(blobPath(dir, sha))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	return vaultcrypto.Decrypt(io.Discard, f, key) == nil
}

func call(t *testing.T, h http.Handler, method, path string, body any) (int, map[string]any, []byte) {
	t.Helper()
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		r = bytes.NewReader(b)
	default:
		j, _ := json.Marshal(b)
		r = bytes.NewReader(j)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	raw := rec.Body.Bytes()
	var m map[string]any
	json.Unmarshal(raw, &m)
	return rec.Code, m, raw
}

func TestHardwareKeyLifecycle(t *testing.T) {
	dir := t.TempDir()
	envKey := rnd(32)
	t.Setenv("LUMEN_MASTER_KEY", base64.StdEncoding.EncodeToString(envKey))
	v := newTestVault(t, dir)

	// Legacy mode: unlocked from .env, cannot be locked.
	if code, m, _ := call(t, v, "GET", "/v1/keys", nil); code != 200 || m["protected"] != false || m["sealed"] != false {
		t.Fatalf("initial status %d %v", code, m)
	}
	if code, _, _ := call(t, v, "POST", "/v1/keys/seal", nil); code != http.StatusConflict {
		t.Fatalf("seal without keys: %d", code)
	}

	// Store a blob with the .env key.
	plain := []byte("exhibit A")
	sum := sha256.Sum256(plain)
	sha := hex.EncodeToString(sum[:])
	if code, _, raw := call(t, v, "POST", "/v1/blobs", plain); code != 200 {
		t.Fatalf("put: %d %s", code, raw)
	}

	// Enroll a security key; the first one also creates a recovery code.
	credID, salt, prf := enc(rnd(48)), rnd(32), rnd(32)
	code, m, raw := call(t, v, "POST", "/v1/keys/slots", map[string]string{
		"label": "YubiKey 5C", "credential_id": credID, "rp_id": "localhost", "salt": enc(salt), "secret": enc(prf)})
	if code != 200 {
		t.Fatalf("enroll: %d %s", code, raw)
	}
	recovery, _ := m["recovery_code"].(string)
	if recovery == "" {
		t.Fatal("first enrollment did not return a recovery code")
	}
	slot := m["slot"].(map[string]any)
	if _, leaked := slot["wrapped_key"]; leaked {
		t.Fatal("wrapped key exposed")
	}
	if m["rotation_error"] != nil {
		t.Fatalf("rotation failed: %v", m["rotation_error"])
	}
	// The first key replaced the master key: the .env key decrypts nothing any more.
	if decryptsWith(t, dir, sha, envKey) {
		t.Fatal("LUMEN_MASTER_KEY still decrypts stored evidence after enrollment")
	}
	if code, _, raw := call(t, v, "GET", "/v1/blobs/"+sha, nil); code != 200 || !bytes.Equal(raw, plain) {
		t.Fatalf("blob unreadable after rotation: %d %q", code, raw)
	}
	if _, err := os.Stat(filepath.Join(dir, "rotation.pending")); !os.IsNotExist(err) {
		t.Fatal("rotation marker left behind")
	}
	if code, _, _ := call(t, v, "POST", "/v1/keys/slots", map[string]string{
		"credential_id": credID, "salt": enc(salt), "secret": enc(prf)}); code != http.StatusConflict {
		t.Fatalf("duplicate credential: %d", code)
	}

	// Lock: blobs are refused.
	if code, _, _ := call(t, v, "POST", "/v1/keys/seal", nil); code != 200 {
		t.Fatalf("seal: %d", code)
	}
	if code, _, _ := call(t, v, "GET", "/v1/blobs/"+sha, nil); code != http.StatusLocked {
		t.Fatalf("get while locked: %d", code)
	}
	if code, _, _ := call(t, v, "POST", "/v1/blobs", []byte("x")); code != http.StatusLocked {
		t.Fatalf("put while locked: %d", code)
	}

	// Wrong secret, and the right secret for an unknown credential, fail.
	if code, _, _ := call(t, v, "POST", "/v1/keys/unlock", map[string]string{"credential_id": credID, "secret": enc(rnd(32))}); code != 401 {
		t.Fatalf("wrong secret: %d", code)
	}
	if code, _, _ := call(t, v, "POST", "/v1/keys/unlock", map[string]string{"credential_id": enc(rnd(48)), "secret": enc(prf)}); code != 401 {
		t.Fatalf("unknown credential: %d", code)
	}

	// Right key unlocks and the old blob decrypts.
	if code, _, raw := call(t, v, "POST", "/v1/keys/unlock", map[string]string{"credential_id": credID, "secret": enc(prf)}); code != 200 {
		t.Fatalf("unlock: %d %s", code, raw)
	}
	if code, _, raw := call(t, v, "GET", "/v1/blobs/"+sha, nil); code != 200 || !bytes.Equal(raw, plain) {
		t.Fatalf("get after unlock: %d %q", code, raw)
	}

	// The last security key cannot be removed; the recovery code cannot be removed.
	if code, _, _ := call(t, v, "DELETE", "/v1/keys/slots/"+slot["id"].(string), nil); code != http.StatusConflict {
		t.Fatalf("remove last key: %d", code)
	}

	// Restart: .env key is ignored, the vault boots locked.
	v = newTestVault(t, dir)
	if code, m, _ := call(t, v, "GET", "/v1/keys", nil); m["protected"] != true || m["sealed"] != true || m["env_key_present"] != true {
		t.Fatalf("after restart %d %v", code, m)
	}
	if code, _, _ := call(t, v, "GET", "/v1/blobs/"+sha, nil); code != http.StatusLocked {
		t.Fatalf("env key still works after enrollment: %d", code)
	}

	// Recovery code unlocks, typed sloppily.
	typed := strings.ToLower(strings.ReplaceAll(recovery, "-", " "))
	if code, _, raw := call(t, v, "POST", "/v1/keys/unlock", map[string]string{"recovery_code": typed}); code != 200 {
		t.Fatalf("recovery unlock: %d %s", code, raw)
	}
	if code, _, raw := call(t, v, "GET", "/v1/blobs/"+sha, nil); code != 200 || !bytes.Equal(raw, plain) {
		t.Fatalf("get after recovery: %d %q", code, raw)
	}

	// Replacing the recovery code invalidates the old one.
	code, m, _ = call(t, v, "POST", "/v1/keys/recovery", nil)
	if code != 200 || m["recovery_code"] == recovery {
		t.Fatalf("rotate: %d", code)
	}
	call(t, v, "POST", "/v1/keys/seal", nil)
	if code, _, _ := call(t, v, "POST", "/v1/keys/unlock", map[string]string{"recovery_code": recovery}); code != 401 {
		t.Fatalf("old recovery code still works: %d", code)
	}
	if code, _, _ := call(t, v, "POST", "/v1/keys/unlock", map[string]string{"recovery_code": m["recovery_code"].(string)}); code != 200 {
		t.Fatalf("new recovery code: %d", code)
	}

	// The slot file never contains the master key in the clear.
	b, _ := os.ReadFile(filepath.Join(dir, "keyslots.json"))
	if strings.Contains(string(b), os.Getenv("LUMEN_MASTER_KEY")) {
		t.Fatal("master key stored in the clear")
	}
	if fi, _ := os.Stat(filepath.Join(dir, "keyslots.json")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("keyslots.json mode %v", fi.Mode().Perm())
	}
}

func TestBootsLockedWithoutEnvKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LUMEN_MASTER_KEY", base64.StdEncoding.EncodeToString(rnd(32)))
	v := newTestVault(t, dir)
	call(t, v, "POST", "/v1/keys/slots", map[string]string{"credential_id": "abc", "salt": enc(rnd(32)), "secret": enc(rnd(32))})

	// Operator followed the instructions and removed the key from .env.
	t.Setenv("LUMEN_MASTER_KEY", "")
	v = newTestVault(t, dir)
	if _, m, _ := call(t, v, "GET", "/v1/keys", nil); m["sealed"] != true || m["env_key_present"] != false {
		t.Fatalf("status %v", m)
	}
}

// A crash after the new key slots were saved but before every blob was
// re-wrapped must not lose data: the vault finishes the job on the next unlock.
func TestRotationResumesAfterCrash(t *testing.T) {
	dir := t.TempDir()
	envKey := rnd(32)
	t.Setenv("LUMEN_MASTER_KEY", base64.StdEncoding.EncodeToString(envKey))
	_, h := bootVault(t, dir)
	var shas []string
	for i := range 3 {
		body := []byte("exhibit " + string(rune('A'+i)))
		_, m, _ := call(t, h, "POST", "/v1/blobs", body)
		shas = append(shas, m["sha256"].(string))
	}
	credID, prf := enc(rnd(48)), rnd(32)
	v, h := bootVault(t, dir)
	call(t, h, "POST", "/v1/keys/slots", map[string]string{"credential_id": credID, "salt": enc(rnd(32)), "secret": enc(prf)})

	// Simulate the crash: put two blobs back under the old key and leave the marker.
	newKey := slices.Clone(v.keys.master)
	for _, sha := range shas[:2] {
		os.Chmod(blobPath(dir, sha), 0o600)
		f, _ := os.OpenFile(blobPath(dir, sha), os.O_RDWR, 0)
		if changed, err := vaultcrypto.Rewrap(f, newKey, envKey); err != nil || !changed {
			t.Fatalf("simulate: %v %v", changed, err)
		}
		f.Close()
	}
	writeMarker(filepath.Join(dir, "rotation.pending"))

	// Reboot: locked, rotation pending.
	v, h = bootVault(t, dir)
	if _, m, _ := call(t, h, "GET", "/v1/keys", nil); m["sealed"] != true || m["rotation_pending"] != true {
		t.Fatalf("after crash: %v", m)
	}
	if code, _, raw := call(t, h, "POST", "/v1/keys/unlock", map[string]string{"credential_id": credID, "secret": enc(prf)}); code != 200 {
		t.Fatalf("unlock: %d %s", code, raw)
	}
	if _, m, _ := call(t, h, "GET", "/v1/keys", nil); m["rotation_pending"] != false {
		t.Fatalf("rotation not finished on unlock: %v", m)
	}
	for _, sha := range shas {
		if decryptsWith(t, dir, sha, envKey) {
			t.Fatalf("blob %s still under the old key", sha[:8])
		}
		if code, _, _ := call(t, h, "GET", "/v1/blobs/"+sha, nil); code != 200 {
			t.Fatalf("blob %s unreadable: %d", sha[:8], code)
		}
		if fi, _ := os.Stat(blobPath(dir, sha)); fi.Mode().Perm() != 0o400 {
			t.Fatalf("blob mode %v", fi.Mode().Perm())
		}
	}
}

// If the old key is gone while a rotation is unfinished, unreadable blobs must
// be reported as locked, not as failed integrity.
func TestUnfinishedRotationIsNotTampering(t *testing.T) {
	dir := t.TempDir()
	envKey := rnd(32)
	t.Setenv("LUMEN_MASTER_KEY", base64.StdEncoding.EncodeToString(envKey))
	_, h := bootVault(t, dir)
	_, m, _ := call(t, h, "POST", "/v1/blobs", []byte("exhibit"))
	sha := m["sha256"].(string)
	credID, prf := enc(rnd(48)), rnd(32)
	v, h := bootVault(t, dir)
	call(t, h, "POST", "/v1/keys/slots", map[string]string{"credential_id": credID, "salt": enc(rnd(32)), "secret": enc(prf)})
	os.Chmod(blobPath(dir, sha), 0o600)
	f, _ := os.OpenFile(blobPath(dir, sha), os.O_RDWR, 0)
	vaultcrypto.Rewrap(f, v.keys.master, envKey)
	f.Close()
	writeMarker(filepath.Join(dir, "rotation.pending"))

	t.Setenv("LUMEN_MASTER_KEY", "")
	_, h = bootVault(t, dir)
	call(t, h, "POST", "/v1/keys/unlock", map[string]string{"credential_id": credID, "secret": enc(prf)})
	if code, _, _ := call(t, h, "POST", "/v1/blobs/"+sha+"/verify", nil); code != http.StatusLocked {
		t.Fatalf("verify during unfinished rotation: %d, want 423", code)
	}
}
