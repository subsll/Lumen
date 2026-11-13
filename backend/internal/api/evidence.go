package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lumenforensics/lumen/internal/vaultclient"
)

type Artifact struct {
	ID           string    `json:"id"`
	EvidenceID   string    `json:"evidence_id"`
	Role         string    `json:"role"`
	Filename     string    `json:"filename"`
	MimeType     string    `json:"mime_type"`
	SizeBytes    int64     `json:"size_bytes"`
	SHA256       string    `json:"sha256"`
	SHA1         string    `json:"sha1"`
	MD5          string    `json:"md5"`
	ClientSHA256 *string   `json:"client_sha256"`
	CreatedAt    time.Time `json:"created_at"`
}

type Evidence struct {
	ID                string          `json:"id"`
	CaseID            string          `json:"case_id"`
	CaseReference     string          `json:"case_reference"`
	CaseTitle         string          `json:"case_title"`
	Number            int             `json:"number"`
	ExhibitRef        string          `json:"exhibit_ref"`
	Kind              string          `json:"kind"`
	Title             string          `json:"title"`
	Description       string          `json:"description"`
	Source            string          `json:"source"`
	AcquisitionMethod string          `json:"acquisition_method"`
	AcquiredAt        time.Time       `json:"acquired_at"`
	Location          string          `json:"location"`
	Tags              []string        `json:"tags"`
	IntegrityStatus   string          `json:"integrity_status"`
	LastVerifiedAt    *time.Time      `json:"last_verified_at"`
	CreatedAt         time.Time       `json:"created_at"`
	TotalBytes        int64           `json:"total_bytes"`
	PrimarySHA256     string          `json:"primary_sha256"`
	PrimaryMime       string          `json:"primary_mime"`
	PrimaryArtifactID *string         `json:"primary_artifact_id"`
	ArtifactCount     int             `json:"artifact_count"`
	Artifacts         []Artifact      `json:"artifacts,omitempty"`
	Capture           json.RawMessage `json:"capture,omitempty"`
	FileMetadata      []FileMetadata  `json:"file_metadata,omitempty"`
}

func exhibitRef(caseRef string, n int) string { return fmt.Sprintf("%s-E%03d", caseRef, n) }

// The primary artifact is the one shown as the item's preview and headline hash.
const evidenceSelect = `SELECT e.id, e.case_id, c.reference, c.title, e.number, e.kind, e.title, e.description,
	e.source, e.acquisition_method, e.acquired_at, e.location, e.tags, e.integrity_status,
	e.last_verified_at, e.created_at,
	COALESCE((SELECT sum(size_bytes) FROM artifacts a WHERE a.evidence_id = e.id), 0),
	COALESCE(p.sha256, ''), COALESCE(p.mime_type, ''), p.id,
	(SELECT count(*) FROM artifacts a WHERE a.evidence_id = e.id)
	FROM evidence e
	JOIN cases c ON c.id = e.case_id
	LEFT JOIN LATERAL (
		SELECT id, sha256, mime_type FROM artifacts a WHERE a.evidence_id = e.id
		ORDER BY (a.role IN ('original', 'screenshot')) DESC, a.created_at LIMIT 1
	) p ON true`

func scanEvidence(row pgx.Row) (Evidence, error) {
	var e Evidence
	err := row.Scan(&e.ID, &e.CaseID, &e.CaseReference, &e.CaseTitle, &e.Number, &e.Kind, &e.Title, &e.Description,
		&e.Source, &e.AcquisitionMethod, &e.AcquiredAt, &e.Location, &e.Tags, &e.IntegrityStatus,
		&e.LastVerifiedAt, &e.CreatedAt, &e.TotalBytes, &e.PrimarySHA256, &e.PrimaryMime, &e.PrimaryArtifactID,
		&e.ArtifactCount)
	e.ExhibitRef = exhibitRef(e.CaseReference, e.Number)
	return e, err
}

func (s *Server) queryEvidence(ctx context.Context, where string, args ...any) ([]Evidence, error) {
	rows, err := s.DB.Query(ctx, evidenceSelect+" "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Evidence{}
	for rows.Next() {
		e, err := scanEvidence(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Server) loadArtifacts(ctx context.Context, evidenceID string) ([]Artifact, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, evidence_id, role, filename, mime_type, size_bytes,
		sha256, sha1, md5, client_sha256, created_at FROM artifacts WHERE evidence_id=$1
		ORDER BY array_position(ARRAY['original','screenshot','mhtml','html','dns','metadata'], role), created_at`, evidenceID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Artifact])
}

func (s *Server) getEvidence(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	list, err := s.queryEvidence(r.Context(), "WHERE e.id = $1", id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(list) == 0 {
		writeErr(w, errNotFound)
		return
	}
	e := list[0]
	if e.Artifacts, err = s.loadArtifacts(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	var capture []byte
	if err := s.DB.QueryRow(r.Context(), `SELECT capture::text FROM evidence WHERE id=$1`, id).Scan(&capture); err != nil {
		writeErr(w, err)
		return
	}
	e.Capture = capture
	if e.FileMetadata, err = s.loadFileMetadata(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, e)
}

// ---- registration ----

type newEvidence struct {
	CaseID            string
	Kind              string
	Title             string
	Description       string
	Source            string
	AcquisitionMethod string
	AcquiredAt        time.Time
	Location          string
	Tags              []string
	Capture           json.RawMessage
}

type newArtifact struct {
	Role         string
	Filename     string
	MimeType     string
	Put          *vaultclient.PutResult
	ClientSHA256 string
}

// insertEvidence registers an evidence item and its artifacts inside tx and
// returns the new id and exhibit reference.
func (s *Server) insertEvidence(ctx context.Context, tx pgx.Tx, ev newEvidence, arts []newArtifact) (string, string, error) {
	var caseRef, status string
	if err := tx.QueryRow(ctx, `SELECT reference, status FROM cases WHERE id=$1 FOR UPDATE`, ev.CaseID).Scan(&caseRef, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", &apiError{http.StatusNotFound, "case not found"}
		}
		return "", "", err
	}
	if status == "closed" || status == "archived" {
		return "", "", badRequest("case is %s; reopen it before adding evidence", status)
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(number), 0) + 1 FROM evidence WHERE case_id=$1`, ev.CaseID).Scan(&n); err != nil {
		return "", "", err
	}
	id := uuid.NewString()
	var capture any
	if len(ev.Capture) > 0 {
		capture = string(ev.Capture)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO evidence (id, case_id, number, kind, title, description, source,
		acquisition_method, acquired_at, location, tags, capture) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb)`,
		id, ev.CaseID, n, ev.Kind, ev.Title, ev.Description, ev.Source, ev.AcquisitionMethod,
		ev.AcquiredAt, ev.Location, cleanTags(ev.Tags), capture); err != nil {
		return "", "", err
	}
	for _, a := range arts {
		if _, err := tx.Exec(ctx, `INSERT INTO artifacts (id, evidence_id, role, filename, mime_type, size_bytes,
			sha256, sha1, md5, client_sha256) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			uuid.NewString(), id, a.Role, a.Filename, a.MimeType, a.Put.Size, a.Put.SHA256, a.Put.SHA1, a.Put.MD5,
			strPtr(a.ClientSHA256)); err != nil {
			return "", "", err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE cases SET updated_at=now() WHERE id=$1`, ev.CaseID); err != nil {
		return "", "", err
	}
	return id, exhibitRef(caseRef, n), nil
}

func artifactDetails(arts []newArtifact) []map[string]any {
	out := []map[string]any{}
	for _, a := range arts {
		d := map[string]any{
			"role": a.Role, "filename": a.Filename, "mime_type": a.MimeType, "size_bytes": a.Put.Size,
			"sha256": a.Put.SHA256, "sha1": a.Put.SHA1, "md5": a.Put.MD5,
		}
		if a.ClientSHA256 != "" {
			d["client_sha256"] = a.ClientSHA256
			d["client_hash_match"] = a.ClientSHA256 == a.Put.SHA256
		}
		out = append(out, d)
	}
	return out
}

func parseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Now().UTC(), nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, badRequest("invalid acquired_at %q", s)
}

func safeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || name == "" {
		return "upload.bin"
	}
	return name
}

// uploadEvidence accepts multipart/form-data with metadata fields first and
// the file part last, and streams the file straight into the vault.
func (s *Server) uploadEvidence(w http.ResponseWriter, r *http.Request) {
	caseID, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	ctx := r.Context()
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, badRequest("expected multipart/form-data"))
		return
	}
	fields := map[string]string{}
	var art *newArtifact
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeErr(w, badRequest("malformed upload: %v", err))
			return
		}
		if part.FormName() == "file" {
			filename := safeFilename(part.FileName())
			put, err := s.Vault.Put(ctx, part)
			if err != nil {
				writeErr(w, err)
				return
			}
			mt := part.Header.Get("Content-Type")
			if mt == "" || mt == "application/octet-stream" {
				if byExt := mime.TypeByExtension(filepath.Ext(filename)); byExt != "" {
					mt = byExt
				} else {
					mt = put.DetectedType
				}
			}
			art = &newArtifact{Role: "original", Filename: filename, MimeType: mt, Put: put}
			break
		}
		b, _ := io.ReadAll(io.LimitReader(part, 64<<10))
		fields[part.FormName()] = string(b)
	}
	if art == nil {
		writeErr(w, badRequest("no file part in upload (the file must be the last field)"))
		return
	}
	art.ClientSHA256 = strings.ToLower(strings.TrimSpace(fields["client_sha256"]))

	if art.ClientSHA256 != "" && art.ClientSHA256 != art.Put.SHA256 {
		// The bytes changed between the investigator's machine and the vault.
		s.inTx(ctx, func(tx pgx.Tx) error {
			_, err := s.record(ctx, tx, eventInput{CaseID: caseID, Action: "evidence.upload_rejected", Details: map[string]any{
				"reason": "client and server SHA-256 differ", "filename": art.Filename,
				"client_sha256": art.ClientSHA256, "server_sha256": art.Put.SHA256,
			}})
			return err
		})
		writeErr(w, &apiError{http.StatusUnprocessableEntity,
			"integrity check failed: the file changed in transit (client and server SHA-256 differ). Nothing was registered."})
		return
	}

	acquiredAt, err := parseTime(fields["acquired_at"])
	if err != nil {
		writeErr(w, err)
		return
	}
	title := strings.TrimSpace(fields["title"])
	if title == "" {
		title = art.Filename
	}
	method := strings.TrimSpace(fields["acquisition_method"])
	if method == "" {
		method = "Manual upload"
	}
	ev := newEvidence{
		CaseID: caseID, Kind: "file", Title: title,
		Description:       strings.TrimSpace(fields["description"]),
		Source:            strings.TrimSpace(fields["source"]),
		AcquisitionMethod: method,
		AcquiredAt:        acquiredAt,
		Location:          strings.TrimSpace(fields["location"]),
		Tags:              strings.Split(fields["tags"], ","),
	}
	var evID string
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		id, ref, err := s.insertEvidence(ctx, tx, ev, []newArtifact{*art})
		if err != nil {
			return err
		}
		evID = id
		_, err = s.record(ctx, tx, eventInput{CaseID: caseID, EvidenceID: id, Action: "evidence.registered", Details: map[string]any{
			"exhibit": ref, "title": ev.Title, "source": ev.Source, "acquisition_method": ev.AcquisitionMethod,
			"acquired_at": ev.AcquiredAt, "location": ev.Location, "artifacts": artifactDetails([]newArtifact{*art}),
		}})
		return err
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	s.KickMetadata()
	list, err := s.queryEvidence(ctx, "WHERE e.id = $1", evID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, list[0])
}

func (s *Server) updateEvidence(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Title       *string   `json:"title"`
		Description *string   `json:"description"`
		Location    *string   `json:"location"`
		Tags        *[]string `json:"tags"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	ctx := r.Context()
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		var caseID, title, desc, loc string
		var tags []string
		if err := tx.QueryRow(ctx, `SELECT case_id, title, description, location, tags FROM evidence WHERE id=$1 FOR UPDATE`, id).
			Scan(&caseID, &title, &desc, &loc, &tags); err != nil {
			return err
		}
		changes := map[string]any{}
		set := func(field string, cur *string, next *string) {
			if next != nil && strings.TrimSpace(*next) != *cur {
				changes[field] = map[string]string{"from": *cur, "to": strings.TrimSpace(*next)}
				*cur = strings.TrimSpace(*next)
			}
		}
		set("title", &title, in.Title)
		set("description", &desc, in.Description)
		set("location", &loc, in.Location)
		if in.Tags != nil {
			t := cleanTags(*in.Tags)
			if strings.Join(t, ",") != strings.Join(tags, ",") {
				changes["tags"] = map[string]any{"from": tags, "to": t}
				tags = t
			}
		}
		if title == "" {
			return badRequest("title cannot be empty")
		}
		if len(changes) == 0 {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE evidence SET title=$2, description=$3, location=$4, tags=$5 WHERE id=$1`,
			id, title, desc, loc, tags); err != nil {
			return err
		}
		_, err := s.record(ctx, tx, eventInput{CaseID: caseID, EvidenceID: id, Action: "evidence.metadata_updated", Details: changes})
		return err
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	s.getEvidence(w, r)
}

// ---- integrity ----

type artifactCheck struct {
	ArtifactID string `json:"artifact_id"`
	Role       string `json:"role"`
	Filename   string `json:"filename"`
	SHA256     string `json:"sha256"`
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
}

// VerifyEvidence re-hashes every artifact of an evidence item in the vault
// and records the outcome in the chain of custody.
func (s *Server) VerifyEvidence(ctx context.Context, id, actor string) (string, []artifactCheck, error) {
	arts, err := s.loadArtifacts(ctx, id)
	if err != nil {
		return "", nil, err
	}
	status := "verified"
	checks := []artifactCheck{}
	for _, a := range arts {
		c := artifactCheck{ArtifactID: a.ID, Role: a.Role, Filename: a.Filename, SHA256: a.SHA256}
		res, err := s.Vault.Verify(ctx, a.SHA256)
		switch {
		case err != nil:
			c.Error = err.Error()
		case !res.OK:
			c.Error = res.Error
			if c.Error == "" {
				c.Error = "computed hash " + res.ComputedSHA256 + " does not match"
			}
		case res.Size != a.SizeBytes:
			c.Error = fmt.Sprintf("size mismatch: recorded %d, stored %d", a.SizeBytes, res.Size)
		default:
			c.OK = true
		}
		if !c.OK {
			status = "failed"
		}
		checks = append(checks, c)
	}
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		var caseID string
		if err := tx.QueryRow(ctx, `UPDATE evidence SET integrity_status=$2, last_verified_at=now() WHERE id=$1 RETURNING case_id`,
			id, status).Scan(&caseID); err != nil {
			return err
		}
		action := "evidence.integrity_verified"
		if status == "failed" {
			action = "evidence.integrity_failed"
		}
		_, err := s.record(ctx, tx, eventInput{CaseID: caseID, EvidenceID: id, Action: action, Actor: actor,
			Details: map[string]any{"algorithm": "SHA-256", "artifacts": checks}})
		return err
	})
	return status, checks, err
}

func (s *Server) verifyEvidenceHandler(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	status, checks, err := s.VerifyEvidence(r.Context(), id, "")
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": status, "artifacts": checks})
}

// RunScheduledVerification re-verifies evidence not checked within interval.
func (s *Server) RunScheduledVerification(ctx context.Context, interval time.Duration) (int, error) {
	rows, err := s.DB.Query(ctx, `SELECT id FROM evidence
		WHERE last_verified_at IS NULL OR last_verified_at < now() - $1::interval ORDER BY last_verified_at NULLS FIRST LIMIT 200`,
		fmt.Sprintf("%d seconds", int(interval.Seconds())))
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if _, _, err := s.VerifyEvidence(ctx, id, SystemActor); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// ---- content access ----

func inlineSafe(mt string) bool {
	base, _, _ := mime.ParseMediaType(mt)
	return strings.HasPrefix(base, "image/") && base != "image/svg+xml" ||
		base == "application/pdf" || base == "text/plain" || strings.HasPrefix(base, "video/") || strings.HasPrefix(base, "audio/")
}

func (s *Server) artifactContent(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	ctx := r.Context()
	var a Artifact
	var caseID string
	err = s.DB.QueryRow(ctx, `SELECT a.id, a.evidence_id, a.role, a.filename, a.mime_type, a.size_bytes, a.sha256, e.case_id
		FROM artifacts a JOIN evidence e ON e.id = a.evidence_id WHERE a.id=$1`, id).
		Scan(&a.ID, &a.EvidenceID, &a.Role, &a.Filename, &a.MimeType, &a.SizeBytes, &a.SHA256, &caseID)
	if err != nil {
		writeErr(w, err)
		return
	}
	inline := r.URL.Query().Get("disposition") == "inline" && inlineSafe(a.MimeType)
	action, disp := "artifact.downloaded", "attachment"
	if inline {
		action, disp = "artifact.viewed", "inline"
	}
	if err := s.inTx(ctx, func(tx pgx.Tx) error {
		_, err := s.record(ctx, tx, eventInput{CaseID: caseID, EvidenceID: a.EvidenceID, Action: action, Details: map[string]any{
			"artifact_id": a.ID, "role": a.Role, "filename": a.Filename, "sha256": a.SHA256,
		}})
		return err
	}); err != nil {
		writeErr(w, err)
		return
	}
	rc, err := s.Vault.Get(ctx, a.SHA256)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rc.Close()
	h := w.Header()
	h.Set("Content-Type", a.MimeType)
	if !inline {
		h.Set("Content-Type", "application/octet-stream")
	}
	h.Set("Content-Length", fmt.Sprint(a.SizeBytes))
	h.Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": a.Filename}))
	// Evidence is hostile content: never let it run in the app's origin.
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self' data:; media-src 'self'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Lumen-SHA256", a.SHA256)
	io.Copy(w, rc)
}
