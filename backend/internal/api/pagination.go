package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
)

// Paged is the envelope for every list endpoint.
type Paged[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

const maxPageSize = 200

// parsePage reads ?limit= and ?offset= (or ?page= with 1-based numbering).
func parsePage(r *http.Request, def int) (limit, offset int) {
	q := r.URL.Query()
	limit, _ = strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = def
	}
	limit = min(limit, maxPageSize)
	offset, _ = strconv.Atoi(q.Get("offset"))
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 1 && q.Get("offset") == "" {
		offset = (p - 1) * limit
	}
	return limit, max(offset, 0)
}

func placeholders(args []any) (string, string) {
	return fmt.Sprintf("$%d", len(args)+1), fmt.Sprintf("$%d", len(args)+2)
}

func (s *Server) count(ctx context.Context, from, where string, args []any) (int, error) {
	var n int
	err := s.DB.QueryRow(ctx, "SELECT count(*) "+from+" "+where, args...).Scan(&n)
	return n, err
}

// ---- cases ----

func (s *Server) listCases(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit, offset := parsePage(r, 25)
	where, args := "", []any{}
	switch st := r.URL.Query().Get("status"); st {
	case "":
	case "open":
		where = " WHERE c.status IN ('intake','active','review')"
	default:
		where, args = " WHERE c.status = $1", append(args, st)
	}
	total, err := s.count(ctx, "FROM cases c", where, args)
	if err != nil {
		writeErr(w, err)
		return
	}
	l, o := placeholders(args)
	rows, err := s.DB.Query(ctx, caseSelect+where+" ORDER BY c.updated_at DESC LIMIT "+l+" OFFSET "+o, append(args, limit, offset)...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []Case{}
	for rows.Next() {
		c, err := scanCase(rows)
		if err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, c)
	}

	counts := map[string]int{"intake": 0, "active": 0, "review": 0, "closed": 0, "archived": 0}
	crows, err := s.DB.Query(ctx, `SELECT status, count(*) FROM cases GROUP BY status`)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer crows.Close()
	all := 0
	for crows.Next() {
		var st string
		var n int
		crows.Scan(&st, &n)
		counts[st] = n
		all += n
	}
	writeJSON(w, 200, map[string]any{
		"items": items, "total": total, "limit": limit, "offset": offset,
		"counts": counts, "all": all,
	})
}

// ---- evidence ----

const evidenceFrom = "FROM evidence e JOIN cases c ON c.id = e.case_id"

func (s *Server) pagedEvidence(ctx context.Context, where, order string, args []any, limit, offset int) (Paged[Evidence], error) {
	total, err := s.count(ctx, evidenceFrom, where, args)
	if err != nil {
		return Paged[Evidence]{}, err
	}
	l, o := placeholders(args)
	items, err := s.queryEvidence(ctx, where+" ORDER BY "+order+" LIMIT "+l+" OFFSET "+o, append(args, limit, offset)...)
	return Paged[Evidence]{Items: items, Total: total, Limit: limit, Offset: offset}, err
}

func (s *Server) listCaseEvidence(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	limit, offset := parsePage(r, 25)
	page, err := s.pagedEvidence(r.Context(), "WHERE e.case_id = $1", "e.number DESC", []any{id}, limit, offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, page)
}

func (s *Server) listAllEvidence(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePage(r, 25)
	page, err := s.pagedEvidence(r.Context(), "", "e.created_at DESC", nil, limit, offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, page)
}

// ---- custody ----

const eventFrom = "FROM custody_events e LEFT JOIN evidence ev ON ev.id = e.evidence_id"

func (s *Server) pagedEvents(w http.ResponseWriter, r *http.Request, where string, args []any) {
	ctx := r.Context()
	limit, offset := parsePage(r, 50)
	total, err := s.count(ctx, eventFrom, where, args)
	if err != nil {
		writeErr(w, err)
		return
	}
	l, o := placeholders(args)
	items, err := s.queryEvents(ctx, where+" ORDER BY e.seq DESC LIMIT "+l+" OFFSET "+o, append(args, limit, offset)...)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, Paged[EventView]{Items: items, Total: total, Limit: limit, Offset: offset})
}

func (s *Server) listCustody(w http.ResponseWriter, r *http.Request) {
	s.pagedEvents(w, r, "", nil)
}

func (s *Server) caseCustody(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.pagedEvents(w, r, "WHERE e.case_id = $1 OR ev.case_id = $1", []any{id})
}

func (s *Server) evidenceCustody(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.pagedEvents(w, r, "WHERE e.evidence_id = $1", []any{id})
}
