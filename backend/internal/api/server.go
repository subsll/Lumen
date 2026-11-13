// Package api implements the Lumen HTTP API.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lumenforensics/lumen/internal/vaultclient"
)

const Version = "0.1.0"

type Server struct {
	DB         *pgxpool.Pool
	Vault      *vaultclient.Client
	CaptureURL string
	Metadata   *Metadata
	HTTP       *http.Client
	// AllowedHosts are the Host header values accepted (without port).
	AllowedHosts map[string]bool
}

func (s *Server) Routes() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/health", s.health)
	m.HandleFunc("GET /api/dashboard", s.dashboard)
	m.HandleFunc("GET /api/settings", s.getSettings)
	m.HandleFunc("PUT /api/settings", s.putSettings)
	m.HandleFunc("GET /api/search", s.search)

	m.HandleFunc("GET /api/cases", s.listCases)
	m.HandleFunc("POST /api/cases", s.createCase)
	m.HandleFunc("GET /api/cases/{id}", s.getCase)
	m.HandleFunc("PATCH /api/cases/{id}", s.updateCase)
	m.HandleFunc("POST /api/cases/{id}/status", s.changeStatus)
	m.HandleFunc("GET /api/cases/{id}/evidence", s.listCaseEvidence)
	m.HandleFunc("POST /api/cases/{id}/evidence", s.uploadEvidence)
	m.HandleFunc("POST /api/cases/{id}/captures", s.captureWeb)
	m.HandleFunc("GET /api/cases/{id}/notes", s.listNotes)
	m.HandleFunc("POST /api/cases/{id}/notes", s.addNote)
	m.HandleFunc("GET /api/cases/{id}/custody", s.caseCustody)
	m.HandleFunc("GET /api/cases/{id}/export", s.exportCase)

	m.HandleFunc("GET /api/evidence", s.listAllEvidence)
	m.HandleFunc("GET /api/evidence/{id}", s.getEvidence)
	m.HandleFunc("PATCH /api/evidence/{id}", s.updateEvidence)
	m.HandleFunc("POST /api/evidence/{id}/verify", s.verifyEvidenceHandler)
	m.HandleFunc("GET /api/evidence/{id}/custody", s.evidenceCustody)
	m.HandleFunc("GET /api/artifacts/{id}/content", s.artifactContent)

	m.HandleFunc("GET /api/custody", s.listCustody)
	m.HandleFunc("GET /api/custody/verify", s.verifyChain)
	return logRequests(s.guard(m))
}

// guard protects an unauthenticated, locally hosted API:
//   - Host allowlist: defeats DNS-rebinding attacks from malicious websites.
//   - Writes require the X-Lumen-Request header. Browsers cannot attach custom
//     headers cross-origin without a CORS preflight, which Lumen never grants,
//     so other websites (including pages being captured) cannot forge writes.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if len(s.AllowedHosts) > 0 && !s.AllowedHosts[strings.ToLower(host)] {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "host " + host + " is not allowed; add it to LUMEN_ALLOWED_HOSTS"})
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Lumen-Request") != "1" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "missing X-Lumen-Request header"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "ms", time.Since(start).Milliseconds())
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) { w.status = code; w.ResponseWriter.WriteHeader(code) }
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// ---- helpers ----

type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string { return e.Message }

func badRequest(format string, a ...any) error {
	return &apiError{http.StatusBadRequest, fmt.Sprintf(format, a...)}
}

var errNotFound = &apiError{http.StatusNotFound, "not found"}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	var ae *apiError
	switch {
	case errors.As(err, &ae):
		writeJSON(w, ae.Status, map[string]string{"error": ae.Message})
	case errors.Is(err, pgx.ErrNoRows):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	default:
		slog.Error("internal error", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		return badRequest("invalid JSON body: %v", err)
	}
	return nil
}

func pathID(r *http.Request) (string, error) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		return "", errNotFound
	}
	return id, nil
}

func cleanTags(tags []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// inTx runs fn in a transaction.
func (s *Server) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok", "version": Version})
}
