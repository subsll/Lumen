package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/lumenforensics/lumen/internal/vaultclient"
)

// Hardware-key lock.
//
// Once a security key is enrolled, the vault starts locked and every API route
// except health, status and unlock requires a session. A session is created by
// unlocking the vault with a security key (WebAuthn PRF) or the recovery code,
// and expires after IdleTimeout without requests. When the last session
// expires, the vault is locked again and forgets the master key.

const sessionCookie = "lumen_session"

type Lock struct {
	IdleTimeout time.Duration

	op       sync.Mutex // serialises unlock, lock and the idle check
	mu       sync.Mutex
	sessions map[string]time.Time // sha256(token) -> expiry
	cached   *vaultclient.KeyStatus
	cachedAt time.Time
}

func NewLock(idle time.Duration) *Lock {
	return &Lock{IdleTimeout: idle, sessions: map[string]time.Time{}}
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func (l *Lock) newSession() string {
	b := make([]byte, 32)
	rand.Read(b)
	token := base64.RawURLEncoding.EncodeToString(b)
	l.mu.Lock()
	l.sessions[hashToken(token)] = time.Now().Add(l.IdleTimeout)
	l.mu.Unlock()
	return token
}

// valid reports whether token is a live session and extends it.
func (l *Lock) valid(token string, touch bool) bool {
	if token == "" {
		return false
	}
	h := hashToken(token)
	l.mu.Lock()
	defer l.mu.Unlock()
	exp, ok := l.sessions[h]
	if !ok || time.Now().After(exp) {
		delete(l.sessions, h)
		return false
	}
	if touch {
		l.sessions[h] = time.Now().Add(l.IdleTimeout)
	}
	return true
}

func (l *Lock) end(token string) {
	l.mu.Lock()
	delete(l.sessions, hashToken(token))
	l.mu.Unlock()
}

func (l *Lock) endAll() {
	l.mu.Lock()
	clear(l.sessions)
	l.mu.Unlock()
}

func (l *Lock) active() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for h, exp := range l.sessions {
		if time.Now().After(exp) {
			delete(l.sessions, h)
		} else {
			n++
		}
	}
	return n
}

func (l *Lock) invalidate() {
	l.mu.Lock()
	l.cached = nil
	l.mu.Unlock()
}

// vaultState returns the vault's key status, cached for a few seconds.
func (s *Server) vaultState(ctx context.Context) (*vaultclient.KeyStatus, error) {
	l := s.Lock
	l.mu.Lock()
	if l.cached != nil && time.Since(l.cachedAt) < 3*time.Second {
		st := l.cached
		l.mu.Unlock()
		return st, nil
	}
	l.mu.Unlock()
	st, err := s.Vault.KeyStatus(ctx)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	l.cached, l.cachedAt = st, time.Now()
	l.mu.Unlock()
	return st, nil
}

func sessionToken(r *http.Request) string {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"), MaxAge: 0,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

var errLocked = &apiError{http.StatusUnauthorized, "Lumen is locked. Unlock it with your security key."}

func writeLocked(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, map[string]any{"error": errLocked.Message, "locked": true})
}

// public routes are reachable while locked.
func publicRoute(r *http.Request) bool {
	switch r.Method + " " + r.URL.Path {
	case "GET /api/health", "GET /api/vault", "POST /api/vault/unlock":
		return true
	}
	return false
}

// requireUnlocked enforces the session once security keys are enrolled.
// It fails closed: if the vault cannot be asked, nothing is served.
func (s *Server) requireUnlocked(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicRoute(r) {
			next.ServeHTTP(w, r)
			return
		}
		st, err := s.vaultState(r.Context())
		if err != nil {
			slog.Error("vault status", "err", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the vault is not reachable"})
			return
		}
		if st.Protected {
			if st.Sealed {
				s.Lock.endAll()
				writeLocked(w)
				return
			}
			if !s.Lock.valid(sessionToken(r), true) {
				writeLocked(w)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---- handlers ----

type vaultStatusResponse struct {
	Protected          bool                  `json:"protected"`
	Sealed             bool                  `json:"sealed"`
	Authenticated      bool                  `json:"authenticated"`
	EnvKeyPresent      bool                  `json:"env_key_present"`
	RotationPending    bool                  `json:"rotation_pending"`
	IdleTimeoutSeconds int                   `json:"idle_timeout_seconds"`
	Keys               []vaultclient.KeySlot `json:"keys"`
}

func (s *Server) vaultStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.vaultState(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the vault is not reachable"})
		return
	}
	out := vaultStatusResponse{
		Protected: st.Protected, Sealed: st.Sealed, EnvKeyPresent: st.EnvKeyPresent, RotationPending: st.RotationPending,
		IdleTimeoutSeconds: int(s.Lock.IdleTimeout.Seconds()), Keys: st.Slots,
	}
	out.Authenticated = !st.Protected || (!st.Sealed && s.Lock.valid(sessionToken(r), false))
	if out.Keys == nil {
		out.Keys = []vaultclient.KeySlot{}
	}
	writeJSON(w, 200, out)
}

func (s *Server) unlockVault(w http.ResponseWriter, r *http.Request) {
	var in vaultclient.UnlockRequest
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if in.RecoveryCode == "" && (in.CredentialID == "" || in.Secret == "") {
		writeErr(w, badRequest("credential_id and secret, or recovery_code, are required"))
		return
	}
	ctx := r.Context()
	s.Lock.op.Lock()
	defer s.Lock.op.Unlock()
	res, err := s.Vault.Unlock(ctx, in)
	s.Lock.invalidate()
	if err != nil {
		writeErr(w, err)
		return
	}
	method := "security key"
	if in.RecoveryCode != "" {
		method = "recovery code"
	}
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		_, err := s.record(ctx, tx, eventInput{Action: "vault.unlocked", Details: map[string]any{
			"method": method, "key": res.Slot.Label, "key_id": res.Slot.ID}})
		return err
	})
	if err != nil {
		// No audit trail, no access: lock again.
		s.Vault.Seal(context.WithoutCancel(ctx))
		writeErr(w, err)
		return
	}
	setSessionCookie(w, r, s.Lock.newSession())
	go s.verifyOverdue()
	writeJSON(w, 200, map[string]any{"key": res.Slot})
}

func (s *Server) lockVault(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	s.Lock.op.Lock()
	defer s.Lock.op.Unlock()
	if err := s.sealAndRecord(ctx, "locked manually", ""); err != nil {
		writeErr(w, err)
		return
	}
	clearSessionCookie(w, r)
	writeJSON(w, 200, map[string]bool{"locked": true})
}

// sealAndRecord locks the vault, ends every session and logs it. Callers hold Lock.op.
func (s *Server) sealAndRecord(ctx context.Context, reason, actor string) error {
	err := s.Vault.Seal(ctx)
	s.Lock.invalidate()
	s.Lock.endAll()
	if err != nil {
		return err
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		_, err := s.record(ctx, tx, eventInput{Action: "vault.locked", Actor: actor, Details: map[string]any{"reason": reason}})
		return err
	})
}

func (s *Server) addVaultKey(w http.ResponseWriter, r *http.Request) {
	var in vaultclient.EnrollRequest
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	ctx := r.Context()
	s.Lock.op.Lock()
	defer s.Lock.op.Unlock()
	before, err := s.vaultState(ctx)
	if err != nil {
		writeErr(w, err)
		return
	}
	res, err := s.Vault.Enroll(ctx, in)
	s.Lock.invalidate()
	if err != nil {
		writeErr(w, err)
		return
	}
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := s.record(ctx, tx, eventInput{Action: "vault.key_added", Details: map[string]any{
			"key": res.Slot.Label, "key_id": res.Slot.ID, "type": "FIDO2 security key (WebAuthn PRF)", "rp_id": res.Slot.RPID}}); err != nil {
			return err
		}
		if res.RecoveryCode != "" {
			if _, err := s.record(ctx, tx, eventInput{Action: "vault.recovery_code_created"}); err != nil {
				return err
			}
		}
		if !before.Protected {
			details := map[string]any{"reason": "first security key registered; the previous master key no longer decrypts the vault"}
			if res.RotationError != "" {
				details["status"] = "unfinished: " + res.RotationError
			}
			_, err := s.record(ctx, tx, eventInput{Action: "vault.master_key_replaced", Details: details})
			return err
		}
		return nil
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	// Enrolling the first key switches protection on; keep this browser signed in.
	if !before.Protected {
		setSessionCookie(w, r, s.Lock.newSession())
	}
	writeJSON(w, 200, map[string]any{"key": res.Slot, "recovery_code": res.RecoveryCode, "rotation_error": res.RotationError})
}

func (s *Server) removeVaultKey(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	ctx := r.Context()
	s.Lock.op.Lock()
	defer s.Lock.op.Unlock()
	res, err := s.Vault.RemoveSlot(ctx, id)
	s.Lock.invalidate()
	if err != nil {
		writeErr(w, err)
		return
	}
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		_, err := s.record(ctx, tx, eventInput{Action: "vault.key_removed", Details: map[string]any{
			"key": res.Slot.Label, "key_id": res.Slot.ID}})
		return err
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"key": res.Slot})
}

func (s *Server) rotateRecoveryCode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	s.Lock.op.Lock()
	defer s.Lock.op.Unlock()
	res, err := s.Vault.RotateRecovery(ctx)
	s.Lock.invalidate()
	if err != nil {
		writeErr(w, err)
		return
	}
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		_, err := s.record(ctx, tx, eventInput{Action: "vault.recovery_code_replaced"})
		return err
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"recovery_code": res.RecoveryCode})
}

// RunIdleLock locks the vault once no session is left. It also locks a vault
// that is open without any session, for example after the API restarted.
func (s *Server) RunIdleLock(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.Lock.op.Lock()
		s.Lock.invalidate()
		st, err := s.vaultState(ctx)
		if err == nil && st.Protected && !st.Sealed && s.Lock.active() == 0 {
			if err := s.sealAndRecord(ctx, "no activity for "+s.Lock.IdleTimeout.String(), SystemActor); err != nil {
				slog.Error("idle lock", "err", err)
			} else {
				slog.Info("vault locked after inactivity")
			}
		}
		s.Lock.op.Unlock()
	}
}

func (s *Server) verifyOverdue() {
	n, err := s.RunScheduledVerification(context.Background(), s.VerifyInterval)
	if err != nil && !errors.Is(err, vaultclient.ErrLocked) {
		slog.Error("verification after unlock", "err", err)
	} else if n > 0 {
		slog.Info("verification after unlock", "evidence_checked", n)
	}
}
