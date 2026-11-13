// Lumen Metadata: extracts embedded file metadata (EXIF, XMP, IPTC, PDF info,
// Office properties, text encoding and more) with ExifTool.
//
// Evidence files are hostile input and ExifTool is a large parser, so this
// service runs alone in a container with no internet access, a read-only root
// filesystem and no capabilities. It never sees the vault or the database:
// the API streams one file in and gets JSON back.
package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxOutput = 8 << 20 // ExifTool JSON larger than this is truncated garbage, not metadata

var (
	token    = os.Getenv("METADATA_TOKEN")
	maxBytes = envInt("METADATA_MAX_BYTES", 1<<30)
	timeout  = 60 * time.Second
	extRe    = regexp.MustCompile(`^\.[A-Za-z0-9]{1,10}$`)
)

func envInt(k string, def int64) int64 {
	if v, err := strconv.ParseInt(os.Getenv(k), 10, 64); err == nil && v > 0 {
		return v
	}
	return def
}

// Field is one metadata tag, in the order ExifTool reported it.
type Field struct {
	Group string          `json:"group"`
	Tag   string          `json:"tag"`
	Value json.RawMessage `json:"value"`
}

type result struct {
	Tool    string  `json:"tool"`
	Version string  `json:"version"`
	Fields  []Field `json:"fields"`
}

func exiftool(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// -config "" must come first: never load a user config file.
	cmd := exec.CommandContext(ctx, "exiftool", append([]string{"-config", ""}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout = &limitWriter{w: &out, n: maxOutput}
	cmd.Stderr = &limitWriter{w: &errb, n: 4096}
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("exiftool timed out after %s", timeout)
	}
	// ExifTool exits 1 with valid JSON when a file has minor errors; trust the output if there is any.
	if out.Len() == 0 && err != nil {
		return nil, fmt.Errorf("exiftool: %v: %s", err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

type limitWriter struct {
	w io.Writer
	n int64
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return len(p), nil
	}
	q := p
	if int64(len(q)) > l.n {
		q = q[:l.n]
	}
	l.n -= int64(len(q))
	l.w.Write(q)
	return len(p), nil
}

// parse reads the first object of ExifTool's -j -G1 output, keeping tag order
// and dropping the System group, which describes the temporary copy rather
// than the evidence (its name, directory, permissions and timestamps).
func parse(raw []byte) ([]Field, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if t, err := dec.Token(); err != nil || t != json.Delim('[') {
		return nil, errors.New("unexpected exiftool output")
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("unexpected exiftool output")
	}
	fields := []Field{}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := t.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		group, tag, ok := strings.Cut(key, ":")
		if !ok || group == "System" || group == "ExifTool" && tag == "ExifToolVersion" {
			continue // SourceFile, temp-file details, and the version (reported separately)
		}
		fields = append(fields, Field{Group: group, Tag: tag, Value: v})
	}
	return fields, nil
}

func extract(w http.ResponseWriter, r *http.Request) {
	if token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// The extension helps ExifTool pick a parser for formats without magic bytes (e.g. .txt, .csv).
	ext := filepath.Ext(r.URL.Query().Get("filename"))
	if !extRe.MatchString(ext) {
		ext = ""
	}
	dir, err := os.MkdirTemp("", "meta-")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "evidence"+strings.ToLower(ext))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	n, err := io.Copy(f, io.LimitReader(r.Body, maxBytes+1))
	f.Close()
	if err != nil {
		http.Error(w, "reading upload: "+err.Error(), 400)
		return
	}
	if n > maxBytes {
		http.Error(w, fmt.Sprintf("file exceeds METADATA_MAX_BYTES (%d)", maxBytes), http.StatusRequestEntityTooLarge)
		return
	}

	raw, err := exiftool(r.Context(),
		"-json", "-G1", "-a", "-struct", "-charset", "UTF8",
		"-api", "LargeFileSupport=1",
		"-c", "%+.6f", // decimal, signed GPS coordinates
		"--", path)
	if err != nil {
		slog.Warn("extract failed", "err", err)
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	fields, err := parse(raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result{Tool: "ExifTool", Version: version, Fields: fields})
}

var version string

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	out, err := exiftool(context.Background(), "-ver")
	if err != nil {
		slog.Error("exiftool not available", "err", err)
		os.Exit(1)
	}
	version = strings.TrimSpace(string(out))
	if token == "" {
		slog.Warn("METADATA_TOKEN not set; relying on network isolation only")
	}

	m := http.NewServeMux()
	m.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "tool": "ExifTool", "version": version})
	})
	m.HandleFunc("POST /v1/extract", extract)
	addr := ":9200"
	if v := os.Getenv("LISTEN_ADDR"); v != "" {
		addr = v
	}
	slog.Info("lumen metadata listening", "addr", addr, "exiftool", version)
	srv := &http.Server{Addr: addr, Handler: m, ReadHeaderTimeout: 15 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}
