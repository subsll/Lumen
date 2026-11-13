package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var validStatus = map[string]bool{"intake": true, "active": true, "review": true, "closed": true, "archived": true}
var validPriority = map[string]bool{"low": true, "medium": true, "high": true, "critical": true}

type Case struct {
	ID            string    `json:"id"`
	Reference     string    `json:"reference"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	Status        string    `json:"status"`
	Priority      string    `json:"priority"`
	Tags          []string  `json:"tags"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	EvidenceCount int       `json:"evidence_count"`
	TotalBytes    int64     `json:"total_bytes"`
}

const caseSelect = `SELECT c.id, c.reference, c.title, c.description, c.status, c.priority, c.tags,
	c.created_at, c.updated_at,
	(SELECT count(*) FROM evidence e WHERE e.case_id = c.id),
	COALESCE((SELECT sum(a.size_bytes) FROM artifacts a JOIN evidence e ON e.id = a.evidence_id WHERE e.case_id = c.id), 0)
	FROM cases c`

func scanCase(row pgx.Row) (Case, error) {
	var c Case
	err := row.Scan(&c.ID, &c.Reference, &c.Title, &c.Description, &c.Status, &c.Priority, &c.Tags,
		&c.CreatedAt, &c.UpdatedAt, &c.EvidenceCount, &c.TotalBytes)
	return c, err
}

func (s *Server) loadCase(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id string) (Case, error) {
	return scanCase(q.QueryRow(ctx, caseSelect+" WHERE c.id = $1", id))
}

type caseInput struct {
	Title       *string   `json:"title"`
	Description *string   `json:"description"`
	Priority    *string   `json:"priority"`
	Tags        *[]string `json:"tags"`
	Reference   *string   `json:"reference"`
}

func (s *Server) createCase(w http.ResponseWriter, r *http.Request) {
	var in caseInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if in.Title == nil || strings.TrimSpace(*in.Title) == "" {
		writeErr(w, badRequest("title is required"))
		return
	}
	priority := "medium"
	if in.Priority != nil {
		priority = *in.Priority
	}
	if !validPriority[priority] {
		writeErr(w, badRequest("invalid priority"))
		return
	}
	var tags []string
	if in.Tags != nil {
		tags = *in.Tags
	}
	desc := ""
	if in.Description != nil {
		desc = strings.TrimSpace(*in.Description)
	}

	var c Case
	err := s.inTx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		ref := ""
		if in.Reference != nil {
			ref = strings.TrimSpace(*in.Reference)
		}
		if ref == "" {
			var prefix string
			if err := tx.QueryRow(ctx, `SELECT value FROM settings WHERE key='case_prefix'`).Scan(&prefix); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `LOCK TABLE cases IN SHARE ROW EXCLUSIVE MODE`); err != nil {
				return err
			}
			base := fmt.Sprintf("%s-%d-", prefix, time.Now().UTC().Year())
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM cases WHERE reference LIKE $1 || '%'`, base).Scan(&n); err != nil {
				return err
			}
			ref = fmt.Sprintf("%s%04d", base, n+1)
		}
		id := uuid.NewString()
		_, err := tx.Exec(ctx, `INSERT INTO cases (id, reference, title, description, priority, tags)
			VALUES ($1,$2,$3,$4,$5,$6)`, id, ref, strings.TrimSpace(*in.Title), desc, priority, cleanTags(tags))
		if err != nil {
			if strings.Contains(err.Error(), "cases_reference_key") {
				return badRequest("case reference %q already exists", ref)
			}
			return err
		}
		if _, err := s.record(ctx, tx, eventInput{CaseID: id, Action: "case.opened", Details: map[string]any{
			"reference": ref, "title": strings.TrimSpace(*in.Title), "priority": priority, "status": "intake",
		}}); err != nil {
			return err
		}
		c, err = s.loadCase(ctx, tx, id)
		return err
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, c)
}

func (s *Server) getCase(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	c, err := s.loadCase(r.Context(), s.DB, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, c)
}

func (s *Server) updateCase(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in caseInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	var c Case
	err = s.inTx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		cur, err := s.loadCase(ctx, tx, id)
		if err != nil {
			return err
		}
		changes := map[string]any{}
		next := cur
		if in.Title != nil && strings.TrimSpace(*in.Title) != cur.Title {
			if strings.TrimSpace(*in.Title) == "" {
				return badRequest("title cannot be empty")
			}
			next.Title = strings.TrimSpace(*in.Title)
			changes["title"] = map[string]string{"from": cur.Title, "to": next.Title}
		}
		if in.Description != nil && strings.TrimSpace(*in.Description) != cur.Description {
			next.Description = strings.TrimSpace(*in.Description)
			changes["description"] = map[string]string{"from": cur.Description, "to": next.Description}
		}
		if in.Priority != nil && *in.Priority != cur.Priority {
			if !validPriority[*in.Priority] {
				return badRequest("invalid priority")
			}
			next.Priority = *in.Priority
			changes["priority"] = map[string]string{"from": cur.Priority, "to": next.Priority}
		}
		if in.Tags != nil {
			t := cleanTags(*in.Tags)
			if strings.Join(t, ",") != strings.Join(cur.Tags, ",") {
				next.Tags = t
				changes["tags"] = map[string]any{"from": cur.Tags, "to": t}
			}
		}
		if len(changes) == 0 {
			c = cur
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE cases SET title=$2, description=$3, priority=$4, tags=$5, updated_at=now() WHERE id=$1`,
			id, next.Title, next.Description, next.Priority, next.Tags); err != nil {
			return err
		}
		if _, err := s.record(ctx, tx, eventInput{CaseID: id, Action: "case.updated", Details: changes}); err != nil {
			return err
		}
		c, err = s.loadCase(ctx, tx, id)
		return err
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, c)
}

func (s *Server) changeStatus(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if !validStatus[in.Status] {
		writeErr(w, badRequest("invalid status"))
		return
	}
	var c Case
	err = s.inTx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		var cur string
		if err := tx.QueryRow(ctx, `SELECT status FROM cases WHERE id=$1 FOR UPDATE`, id).Scan(&cur); err != nil {
			return err
		}
		if cur != in.Status {
			if _, err := tx.Exec(ctx, `UPDATE cases SET status=$2, updated_at=now() WHERE id=$1`, id, in.Status); err != nil {
				return err
			}
			if _, err := s.record(ctx, tx, eventInput{CaseID: id, Action: "case.status_changed", Details: map[string]string{
				"from": cur, "to": in.Status, "reason": strings.TrimSpace(in.Reason),
			}}); err != nil {
				return err
			}
		}
		var err error
		c, err = s.loadCase(ctx, tx, id)
		return err
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, c)
}

type Note struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Server) listNotes(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	rows, err := s.DB.Query(r.Context(), `SELECT id, body, created_at FROM case_notes WHERE case_id=$1 ORDER BY created_at DESC`, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	notes, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Note])
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, notes)
}

func (s *Server) addNote(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	body := strings.TrimSpace(in.Body)
	if body == "" {
		writeErr(w, badRequest("note cannot be empty"))
		return
	}
	n := Note{ID: uuid.NewString(), Body: body}
	err = s.inTx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		if err := tx.QueryRow(ctx, `INSERT INTO case_notes (id, case_id, body) VALUES ($1,$2,$3) RETURNING created_at`,
			n.ID, id, body).Scan(&n.CreatedAt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE cases SET updated_at=now() WHERE id=$1`, id); err != nil {
			return err
		}
		_, err := s.record(ctx, tx, eventInput{CaseID: id, Action: "case.note_added", Details: map[string]string{
			"note_id": n.ID, "body": body,
		}})
		return err
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, n)
}
