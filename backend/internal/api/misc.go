package api

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

var hexRe = regexp.MustCompile(`^[0-9a-fA-F]{8,64}$`)

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, 200, map[string]any{"cases": []Case{}, "evidence": []Evidence{}})
		return
	}
	ctx := r.Context()
	like := "%" + strings.NewReplacer("%", `\%`, "_", `\_`).Replace(q) + "%"

	rows, err := s.DB.Query(ctx, caseSelect+` WHERE c.search @@ websearch_to_tsquery('simple', $1)
		OR c.title ILIKE $2 OR c.reference ILIKE $2 ORDER BY c.updated_at DESC LIMIT 10`, q, like)
	if err != nil {
		writeErr(w, err)
		return
	}
	cases := []Case{}
	for rows.Next() {
		c, err := scanCase(rows)
		if err != nil {
			rows.Close()
			writeErr(w, err)
			return
		}
		cases = append(cases, c)
	}
	rows.Close()

	where := `WHERE e.search @@ websearch_to_tsquery('simple', $1) OR e.title ILIKE $2 OR e.source ILIKE $2
		OR EXISTS (SELECT 1 FROM artifacts a WHERE a.evidence_id = e.id AND a.filename ILIKE $2)
		OR EXISTS (SELECT 1 FROM artifacts a JOIN artifact_metadata m ON m.artifact_id = a.id
			WHERE a.evidence_id = e.id AND m.fields ILIKE $2)`
	args := []any{q, like}
	if hexRe.MatchString(q) {
		where += ` OR EXISTS (SELECT 1 FROM artifacts a WHERE a.evidence_id = e.id
			AND (a.sha256 LIKE $3 OR a.sha1 LIKE $3 OR a.md5 LIKE $3))`
		args = append(args, strings.ToLower(q)+"%")
	}
	evidence, err := s.queryEvidence(ctx, where+" ORDER BY e.created_at DESC LIMIT 15", args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"cases": cases, "evidence": evidence})
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := map[string]any{}
	byStatus := map[string]int{"intake": 0, "active": 0, "review": 0, "closed": 0, "archived": 0}
	rows, err := s.DB.Query(ctx, `SELECT status, count(*) FROM cases GROUP BY status`)
	if err != nil {
		writeErr(w, err)
		return
	}
	total := 0
	for rows.Next() {
		var st string
		var n int
		rows.Scan(&st, &n)
		byStatus[st] = n
		total += n
	}
	rows.Close()
	out["cases_by_status"], out["total_cases"] = byStatus, total

	var evCount, verified, unverified, failed int
	var bytes int64
	err = s.DB.QueryRow(ctx, `SELECT count(*),
		count(*) FILTER (WHERE integrity_status='verified'),
		count(*) FILTER (WHERE integrity_status='unverified'),
		count(*) FILTER (WHERE integrity_status='failed'),
		COALESCE((SELECT sum(size_bytes) FROM artifacts), 0)
		FROM evidence`).Scan(&evCount, &verified, &unverified, &failed, &bytes)
	if err != nil {
		writeErr(w, err)
		return
	}
	out["total_evidence"], out["total_bytes"] = evCount, bytes
	out["integrity"] = map[string]int{"verified": verified, "unverified": unverified, "failed": failed}

	recent, err := s.queryEvents(ctx, "ORDER BY e.seq DESC LIMIT 12")
	if err != nil {
		writeErr(w, err)
		return
	}
	out["recent_activity"] = recent

	var headSeq int64
	var headHash string
	err = s.DB.QueryRow(ctx, `SELECT seq, hash FROM custody_events ORDER BY seq DESC LIMIT 1`).Scan(&headSeq, &headHash)
	if err != nil && err != pgx.ErrNoRows {
		writeErr(w, err)
		return
	}
	out["chain_head"] = map[string]any{"seq": headSeq, "hash": headHash}

	active, err := s.DB.Query(ctx, caseSelect+` WHERE c.status IN ('intake','active','review') ORDER BY c.updated_at DESC LIMIT 6`)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer active.Close()
	list := []Case{}
	for active.Next() {
		c, err := scanCase(active)
		if err != nil {
			writeErr(w, err)
			return
		}
		list = append(list, c)
	}
	out["open_cases"] = list
	writeJSON(w, 200, out)
}

var settingKeys = map[string]bool{"investigator_name": true, "organization": true, "case_prefix": true}
var prefixRe = regexp.MustCompile(`^[A-Z0-9]{2,8}$`)

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(r.Context(), `SELECT key, value FROM settings`)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		out[k] = v
	}
	out["version"] = Version
	writeJSON(w, 200, out)
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	ctx := r.Context()
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		changes := map[string]any{}
		for k, v := range in {
			if !settingKeys[k] {
				continue
			}
			v = strings.TrimSpace(v)
			if k == "case_prefix" {
				v = strings.ToUpper(v)
				if !prefixRe.MatchString(v) {
					return badRequest("case prefix must be 2-8 letters or digits")
				}
			}
			var cur string
			tx.QueryRow(ctx, `SELECT value FROM settings WHERE key=$1`, k).Scan(&cur)
			if cur == v {
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO settings (key, value) VALUES ($1,$2)
				ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, k, v); err != nil {
				return err
			}
			changes[k] = map[string]string{"from": cur, "to": v}
		}
		if len(changes) == 0 {
			return nil
		}
		_, err := s.record(ctx, tx, eventInput{Action: "settings.updated", Details: changes})
		return err
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	s.getSettings(w, r)
}
