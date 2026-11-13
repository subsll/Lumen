package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
)

// FileMetadata is the embedded metadata of one artifact, as extracted by the
// metadata service. Fields is the exact JSON whose SHA-256 is in the custody log.
type FileMetadata struct {
	ArtifactID   string          `json:"artifact_id"`
	Status       string          `json:"status"`
	Tool         string          `json:"tool"`
	ToolVersion  string          `json:"tool_version"`
	Fields       json.RawMessage `json:"fields,omitempty"`
	FieldsSHA256 *string         `json:"fields_sha256"`
	FieldCount   int             `json:"field_count"`
	Error        string          `json:"error,omitempty"`
	ExtractedAt  time.Time       `json:"extracted_at"`
}

// Metadata configures the connection to the metadata extraction service.
type Metadata struct {
	URL      string
	Token    string
	MaxBytes int64
	kick     chan struct{}
}

// NewMetadata configures extraction; an empty url disables it.
func NewMetadata(url, token string, maxBytes int64) *Metadata {
	return &Metadata{URL: url, Token: token, MaxBytes: maxBytes, kick: make(chan struct{}, 1)}
}

// errUnavailable means the extractor could not be reached; the artifact is
// retried later rather than recorded as failed.
var errUnavailable = errors.New("metadata service unavailable")

func (s *Server) loadFileMetadata(ctx context.Context, evidenceID string) ([]FileMetadata, error) {
	rows, err := s.DB.Query(ctx, `SELECT m.artifact_id, m.status, m.tool, m.tool_version, m.fields, m.fields_sha256,
		m.field_count, m.error, m.extracted_at
		FROM artifact_metadata m JOIN artifacts a ON a.id = m.artifact_id WHERE a.evidence_id = $1`, evidenceID)
	if err != nil {
		return nil, err
	}
	out := []FileMetadata{}
	for rows.Next() {
		var m FileMetadata
		var fields *string
		if err := rows.Scan(&m.ArtifactID, &m.Status, &m.Tool, &m.ToolVersion, &fields, &m.FieldsSHA256,
			&m.FieldCount, &m.Error, &m.ExtractedAt); err != nil {
			rows.Close()
			return nil, err
		}
		if fields != nil {
			m.Fields = json.RawMessage(*fields)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// KickMetadata wakes the extraction worker, e.g. right after an upload.
func (s *Server) KickMetadata() {
	if s.Metadata == nil || s.Metadata.kick == nil {
		return
	}
	select {
	case s.Metadata.kick <- struct{}{}:
	default:
	}
}

// RunMetadataWorker extracts metadata from original files that have none yet,
// including evidence registered before this feature existed. It runs until ctx ends.
func (s *Server) RunMetadataWorker(ctx context.Context) {
	if s.Metadata == nil || s.Metadata.URL == "" {
		return
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		n, err := s.extractPending(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Warn("metadata extraction", "err", err)
		} else if n > 0 {
			slog.Info("metadata extraction", "artifacts", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-s.Metadata.kick:
		case <-t.C:
		}
	}
}

type pendingArtifact struct {
	ID, EvidenceID, CaseID, Filename, SHA256 string
	Size                                     int64
}

func (s *Server) extractPending(ctx context.Context) (int, error) {
	done := 0
	for {
		rows, err := s.DB.Query(ctx, `SELECT a.id, a.evidence_id, e.case_id, a.filename, a.sha256, a.size_bytes
			FROM artifacts a JOIN evidence e ON e.id = a.evidence_id
			WHERE a.role = 'original' AND NOT EXISTS (SELECT 1 FROM artifact_metadata m WHERE m.artifact_id = a.id)
			ORDER BY a.created_at LIMIT 20`)
		if err != nil {
			return done, err
		}
		batch, err := pgx.CollectRows(rows, pgx.RowToStructByPos[pendingArtifact])
		if err != nil || len(batch) == 0 {
			return done, err
		}
		for _, a := range batch {
			if err := s.extractOne(ctx, a); err != nil {
				return done, err // service down: stop and retry on the next tick
			}
			done++
		}
	}
}

func (s *Server) extractOne(ctx context.Context, a pendingArtifact) error {
	m := FileMetadata{ArtifactID: a.ID, Tool: "ExifTool"}
	if a.Size > s.Metadata.MaxBytes {
		m.Status, m.Error = "skipped", fmt.Sprintf("file is larger than the %s extraction limit", humanBytes(s.Metadata.MaxBytes))
	} else {
		res, err := s.callExtractor(ctx, a)
		switch {
		case errors.Is(err, errUnavailable):
			return err
		case err != nil:
			m.Status, m.Error = "failed", err.Error()
		default:
			m.Status, m.ToolVersion, m.Fields = "extracted", res.Version, res.Fields
		}
	}
	var fields any
	details := map[string]any{
		"artifact_id": a.ID, "filename": a.Filename, "sha256": a.SHA256,
		"tool": m.Tool, "tool_version": m.ToolVersion, "status": m.Status,
	}
	if m.Status == "extracted" {
		var list []json.RawMessage
		json.Unmarshal(m.Fields, &list)
		m.FieldCount = len(list)
		h := sha256.Sum256(m.Fields)
		sum := hex.EncodeToString(h[:])
		m.FieldsSHA256 = &sum
		fields = string(m.Fields)
		details["field_count"], details["output_sha256"] = m.FieldCount, sum
	} else {
		details["error"] = m.Error
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO artifact_metadata (artifact_id, status, tool, tool_version, fields,
			fields_sha256, field_count, error) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (artifact_id) DO NOTHING`,
			a.ID, m.Status, m.Tool, m.ToolVersion, fields, m.FieldsSHA256, m.FieldCount, m.Error)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		_, err = s.record(ctx, tx, eventInput{CaseID: a.CaseID, EvidenceID: a.EvidenceID, Action: "artifact.metadata_extracted",
			Actor: SystemActor, Details: details})
		return err
	})
}

type extractResult struct {
	Version string          `json:"version"`
	Fields  json.RawMessage `json:"fields"`
}

func (s *Server) callExtractor(ctx context.Context, a pendingArtifact) (*extractResult, error) {
	body, err := s.Vault.Get(ctx, a.SHA256)
	if err != nil {
		return nil, fmt.Errorf("%w: reading from vault: %v", errUnavailable, err)
	}
	defer body.Close()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.Metadata.URL+"/v1/extract?filename="+url.QueryEscape(a.Filename), body)
	if err != nil {
		return nil, err
	}
	req.ContentLength = a.Size
	if s.Metadata.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.Metadata.Token)
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnprocessableEntity || resp.StatusCode == http.StatusRequestEntityTooLarge {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, errors.New(string(msg))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", errUnavailable, resp.StatusCode)
	}
	var out extractResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: bad response: %v", errUnavailable, err)
	}
	if len(out.Fields) == 0 || string(out.Fields) == "null" {
		out.Fields = json.RawMessage("[]")
	}
	return &out, nil
}
