// Lumen Vault: isolated, encrypted, content-addressed evidence storage.
//
// The vault runs on an internal-only Docker network and is reachable solely
// by the Lumen API. Blobs are addressed by the SHA-256 of their plaintext,
// encrypted at rest (see internal/vaultcrypto) and written once (mode 0400).
package main

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lumenforensics/lumen/internal/vaultcrypto"
)

var shaRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

type vault struct {
	dir   string
	kek   []byte
	token string
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)

	kek, err := loadKey()
	if err != nil {
		log.Error("master key", "err", err)
		os.Exit(1)
	}
	token := os.Getenv("VAULT_TOKEN")
	if len(token) < 32 {
		log.Error("VAULT_TOKEN must be set (at least 32 characters). Run ./scripts/setup.sh")
		os.Exit(1)
	}
	v := &vault{dir: env("VAULT_DATA_DIR", "/data"), kek: kek, token: token}
	for _, d := range []string{"blobs", "tmp"} {
		if err := os.MkdirAll(filepath.Join(v.dir, d), 0o700); err != nil {
			log.Error("data dir", "err", err)
			os.Exit(1)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("POST /v1/blobs", v.auth(v.put))
	mux.HandleFunc("GET /v1/blobs/{sha}", v.auth(v.get))
	mux.HandleFunc("POST /v1/blobs/{sha}/verify", v.auth(v.verify))

	addr := env("LISTEN_ADDR", ":9000")
	log.Info("lumen vault listening", "addr", addr, "dir", v.dir)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func loadKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv("LUMEN_MASTER_KEY"))
	if f := os.Getenv("LUMEN_MASTER_KEY_FILE"); raw == "" && f != "" {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		raw = strings.TrimSpace(string(b))
	}
	if raw == "" {
		return nil, errors.New("LUMEN_MASTER_KEY or LUMEN_MASTER_KEY_FILE is required. Run ./scripts/setup.sh")
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil, errors.New("master key must be 32 bytes, base64 encoded")
	}
	return key, nil
}

func (v *vault) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(v.token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (v *vault) path(sha string) string {
	return filepath.Join(v.dir, "blobs", sha[:2], sha[2:4], sha+".lmv")
}

type putResult struct {
	SHA256       string `json:"sha256"`
	SHA1         string `json:"sha1"`
	MD5          string `json:"md5"`
	Size         int64  `json:"size"`
	DetectedType string `json:"detected_type"`
	Deduplicated bool   `json:"deduplicated"`
}

// sniffer captures the first 512 bytes for content type detection.
type sniffer struct{ buf []byte }

func (s *sniffer) Write(p []byte) (int, error) {
	if room := 512 - len(s.buf); room > 0 {
		s.buf = append(s.buf, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

type counter struct{ n int64 }

func (c *counter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

func (v *vault) put(w http.ResponseWriter, r *http.Request) {
	tmp, err := os.CreateTemp(filepath.Join(v.dir, "tmp"), "upload-*")
	if err != nil {
		httpErr(w, err, http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	s256, s1, m5 := sha256.New(), sha1.New(), md5.New()
	sniff, count := &sniffer{}, &counter{}
	src := io.TeeReader(r.Body, io.MultiWriter(s256, s1, m5, sniff, count))
	if err := vaultcrypto.Encrypt(tmp, src, v.kek); err != nil {
		httpErr(w, err, http.StatusBadRequest)
		return
	}
	if err := tmp.Sync(); err != nil {
		httpErr(w, err, http.StatusInternalServerError)
		return
	}
	tmp.Close()

	res := putResult{
		SHA256:       hex.EncodeToString(s256.Sum(nil)),
		SHA1:         hex.EncodeToString(s1.Sum(nil)),
		MD5:          hex.EncodeToString(m5.Sum(nil)),
		Size:         count.n,
		DetectedType: http.DetectContentType(sniff.buf),
	}
	final := v.path(res.SHA256)
	if _, err := os.Stat(final); err == nil {
		res.Deduplicated = true
	} else {
		if err := os.MkdirAll(filepath.Dir(final), 0o700); err != nil {
			httpErr(w, err, http.StatusInternalServerError)
			return
		}
		if err := os.Chmod(tmp.Name(), 0o400); err != nil {
			httpErr(w, err, http.StatusInternalServerError)
			return
		}
		if err := os.Rename(tmp.Name(), final); err != nil {
			httpErr(w, err, http.StatusInternalServerError)
			return
		}
	}
	slog.Info("blob stored", "sha256", res.SHA256, "size", res.Size, "dedup", res.Deduplicated)
	writeJSON(w, res)
}

func (v *vault) open(w http.ResponseWriter, r *http.Request) (*os.File, bool) {
	sha := r.PathValue("sha")
	if !shaRe.MatchString(sha) {
		http.Error(w, "invalid hash", http.StatusBadRequest)
		return nil, false
	}
	f, err := os.Open(v.path(sha))
	if errors.Is(err, os.ErrNotExist) {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, false
	} else if err != nil {
		httpErr(w, err, http.StatusInternalServerError)
		return nil, false
	}
	return f, true
}

func (v *vault) get(w http.ResponseWriter, r *http.Request) {
	f, ok := v.open(w, r)
	if !ok {
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	if err := vaultcrypto.Decrypt(w, f, v.kek); err != nil {
		// Headers may already be sent; the client detects the broken stream.
		slog.Error("decrypt failed", "sha256", r.PathValue("sha"), "err", err)
		panic(http.ErrAbortHandler)
	}
}

func (v *vault) verify(w http.ResponseWriter, r *http.Request) {
	f, ok := v.open(w, r)
	if !ok {
		return
	}
	defer f.Close()
	h, count := sha256.New(), &counter{}
	err := vaultcrypto.Decrypt(io.MultiWriter(h, count), f, v.kek)
	sha := r.PathValue("sha")
	res := map[string]any{"sha256": sha, "size": count.n, "checked_at": time.Now().UTC()}
	if err != nil {
		res["ok"], res["error"] = false, err.Error()
	} else {
		got := hex.EncodeToString(h.Sum(nil))
		res["computed_sha256"], res["ok"] = got, got == sha
	}
	writeJSON(w, res)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, err error, code int) {
	slog.Error("request failed", "err", err)
	http.Error(w, err.Error(), code)
}
