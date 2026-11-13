package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lumenforensics/lumen/internal/custody"
)

// custodyLock serialises appends so the chain never forks.
const custodyLock = 0x4c554d454e // "LUMEN"

// SystemActor is used for actions Lumen performs on its own schedule.
const SystemActor = "Lumen (automated)"

type eventInput struct {
	CaseID     string
	EvidenceID string
	Action     string
	Actor      string // empty means the configured investigator
	Details    any
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// record appends a custody event inside tx. It must be called in the same
// transaction as the change it describes, so both commit or neither does.
func (s *Server) record(ctx context.Context, tx pgx.Tx, in eventInput) (custody.Event, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, custodyLock); err != nil {
		return custody.Event{}, err
	}
	var lastSeq int64
	lastHash := custody.GenesisHash
	err := tx.QueryRow(ctx, `SELECT seq, hash FROM custody_events ORDER BY seq DESC LIMIT 1`).Scan(&lastSeq, &lastHash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return custody.Event{}, err
	}
	actor := in.Actor
	if actor == "" {
		actor, err = investigator(ctx, tx)
		if err != nil {
			return custody.Event{}, err
		}
	}
	details := json.RawMessage("{}")
	if in.Details != nil {
		if details, err = json.Marshal(in.Details); err != nil {
			return custody.Event{}, err
		}
	}
	e := custody.Event{
		Seq:        lastSeq + 1,
		ID:         uuid.NewString(),
		CaseID:     strPtr(in.CaseID),
		EvidenceID: strPtr(in.EvidenceID),
		Action:     in.Action,
		Actor:      actor,
		Details:    details,
		OccurredAt: custody.CanonicalTime(time.Now()),
		PrevHash:   lastHash,
	}
	e.Hash = custody.ComputeHash(e)
	_, err = tx.Exec(ctx, `INSERT INTO custody_events
		(seq, id, case_id, evidence_id, action, actor, details, occurred_at, prev_hash, hash)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		e.Seq, e.ID, e.CaseID, e.EvidenceID, e.Action, e.Actor, string(e.Details), e.OccurredAt, e.PrevHash, e.Hash)
	return e, err
}

func investigator(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (string, error) {
	var name, org string
	err := q.QueryRow(ctx, `SELECT
		COALESCE((SELECT value FROM settings WHERE key='investigator_name'), ''),
		COALESCE((SELECT value FROM settings WHERE key='organization'), '')`).Scan(&name, &org)
	if err != nil {
		return "", err
	}
	if name == "" {
		name = "Investigator"
	}
	if org != "" {
		name += " (" + org + ")"
	}
	return name, nil
}

// EventView is a custody event enriched with display context.
type EventView struct {
	custody.Event
	CaseReference *string `json:"case_reference"`
	ExhibitRef    *string `json:"exhibit_ref"`
	EvidenceTitle *string `json:"evidence_title"`
}

const eventSelect = `SELECT e.seq, e.id, e.case_id, e.evidence_id, e.action, e.actor, e.details,
	e.occurred_at, e.prev_hash, e.hash, c.reference,
	CASE WHEN ev.id IS NULL THEN NULL ELSE ec.reference || '-E' || lpad(ev.number::text, 3, '0') END,
	ev.title
	FROM custody_events e
	LEFT JOIN cases c ON c.id = e.case_id
	LEFT JOIN evidence ev ON ev.id = e.evidence_id
	LEFT JOIN cases ec ON ec.id = ev.case_id`

func scanEvents(rows pgx.Rows) ([]EventView, error) {
	defer rows.Close()
	out := []EventView{}
	for rows.Next() {
		var v EventView
		var details string
		if err := rows.Scan(&v.Seq, &v.ID, &v.CaseID, &v.EvidenceID, &v.Action, &v.Actor, &details,
			&v.OccurredAt, &v.PrevHash, &v.Hash, &v.CaseReference, &v.ExhibitRef, &v.EvidenceTitle); err != nil {
			return nil, err
		}
		v.Details = json.RawMessage(details)
		v.OccurredAt = v.OccurredAt.UTC()
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Server) queryEvents(ctx context.Context, where string, args ...any) ([]EventView, error) {
	rows, err := s.DB.Query(ctx, eventSelect+" "+where, args...)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

// VerifyChain recomputes every hash in the custody log.
func (s *Server) VerifyChain(ctx context.Context) (custody.Result, error) {
	rows, err := s.DB.Query(ctx, `SELECT seq, id, case_id, evidence_id, action, actor, details,
		occurred_at, prev_hash, hash FROM custody_events ORDER BY seq`)
	if err != nil {
		return custody.Result{}, err
	}
	defer rows.Close()
	v := custody.NewVerifier()
	for rows.Next() {
		var e custody.Event
		var details string
		if err := rows.Scan(&e.Seq, &e.ID, &e.CaseID, &e.EvidenceID, &e.Action, &e.Actor, &details,
			&e.OccurredAt, &e.PrevHash, &e.Hash); err != nil {
			return custody.Result{}, err
		}
		e.Details = json.RawMessage(details)
		if !v.Add(e) {
			break
		}
	}
	return v.Result(), rows.Err()
}

func (s *Server) verifyChain(w http.ResponseWriter, r *http.Request) {
	res, err := s.VerifyChain(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}
