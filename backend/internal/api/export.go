package api

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

//go:embed report.html.tmpl
var reportTmpl string

var reportTemplate = template.Must(template.New("report").Funcs(template.FuncMap{
	"bytes": humanBytes,
	"ts":    func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05 UTC") },
	"tsp": func(t *time.Time) string {
		if t == nil {
			return "never"
		}
		return t.UTC().Format("2006-01-02 15:04:05 UTC")
	},
	"short":  func(s string) string { return s[:min(16, len(s))] + "…" },
	"action": actionLabel,
	"upper":  strings.ToUpper,
}).Parse(reportTmpl))

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func actionLabel(a string) string {
	labels := map[string]string{
		"case.opened": "Case opened", "case.updated": "Case details updated",
		"case.status_changed": "Case status changed", "case.note_added": "Note added",
		"case.exported": "Case exported", "evidence.registered": "Evidence registered",
		"evidence.captured": "Web page captured", "evidence.metadata_updated": "Evidence details updated",
		"evidence.integrity_verified": "Integrity verified", "evidence.integrity_failed": "Integrity check FAILED",
		"evidence.upload_rejected": "Upload rejected (hash mismatch)",
		"artifact.viewed":          "Artifact viewed", "artifact.downloaded": "Artifact downloaded",
		"artifact.metadata_extracted": "File metadata extracted",
	}
	if l, ok := labels[a]; ok {
		return l
	}
	return a
}

const exportReadme = `LUMEN EVIDENCE EXPORT
=====================

Produced by Lumen, an OSINT Evidence Management System by Lumen Forensics.

Contents
  manifest.json        Case, evidence items and artifact hashes (SHA-256, SHA-1, MD5)
  custody-log.jsonl    Chain-of-custody events for this case, one JSON object per line
  report.html          Printable chain-of-custody report
  evidence/            The evidence files, exactly as stored, plus
                       <file>.metadata.json with embedded metadata (ExifTool)
  SHA256SUMS           SHA-256 of every file above

Verifying this export
  1. Check every file against SHA256SUMS:
       cd <this folder> && sha256sum -c SHA256SUMS        (Linux)
       cd <this folder> && shasum -a 256 -c SHA256SUMS    (macOS)
  2. Check the custody log and the files together with the Lumen verifier:
       lumen-verify <export.zip>

Each custody event's "hash" is the SHA-256 of its canonical encoding
(see internal/custody/chain.go in the Lumen source), which includes the
hash of the previous event. Changing any event changes its hash.
`

type exportArtifact struct {
	Artifact
	Path string `json:"path"`
}

type exportEvidence struct {
	Evidence
	Files []exportArtifact `json:"files"`
}

func (s *Server) exportCase(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	ctx := r.Context()
	c, err := s.loadCase(ctx, s.DB, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	items, err := s.queryEvidence(ctx, "WHERE e.case_id = $1 ORDER BY e.number", id)
	if err != nil {
		writeErr(w, err)
		return
	}
	evs := []exportEvidence{}
	var artCount int
	var total int64
	for _, e := range items {
		arts, err := s.loadArtifacts(ctx, e.ID)
		if err != nil {
			writeErr(w, err)
			return
		}
		ee := exportEvidence{Evidence: e}
		for _, a := range arts {
			ee.Files = append(ee.Files, exportArtifact{Artifact: a,
				Path: fmt.Sprintf("evidence/E%03d/%s", e.Number, a.Filename)})
			artCount++
			total += a.SizeBytes
		}
		evs = append(evs, ee)
	}

	filename := fmt.Sprintf("%s_export_%s.zip", c.Reference, time.Now().UTC().Format("20060102T150405Z"))
	if err := s.inTx(ctx, func(tx pgx.Tx) error {
		_, err := s.record(ctx, tx, eventInput{CaseID: id, Action: "case.exported", Details: map[string]any{
			"filename": filename, "format": "lumen-export-1", "evidence_count": len(evs),
			"artifact_count": artCount, "total_bytes": total,
		}})
		return err
	}); err != nil {
		writeErr(w, err)
		return
	}
	events, err := s.queryEvents(ctx, "WHERE e.case_id = $1 OR ev.case_id = $1 ORDER BY e.seq", id)
	if err != nil {
		writeErr(w, err)
		return
	}
	actor, _ := investigator(ctx, s.DB)
	generated := time.Now().UTC()

	manifest, _ := json.MarshalIndent(map[string]any{
		"format":        "lumen-export-1",
		"generator":     "Lumen " + Version + " by Lumen Forensics",
		"generated_at":  generated,
		"generated_by":  actor,
		"case":          c,
		"evidence":      evs,
		"custody_log":   "custody-log.jsonl",
		"custody_count": len(events),
	}, "", "  ")
	var log bytes.Buffer
	for _, e := range events {
		b, _ := json.Marshal(e)
		log.Write(b)
		log.WriteByte('\n')
	}
	var report bytes.Buffer
	chain, _ := s.VerifyChain(ctx)
	if err := reportTemplate.Execute(&report, map[string]any{
		"Case": c, "Evidence": evs, "Events": events, "GeneratedAt": generated, "GeneratedBy": actor,
		"Version": Version, "Chain": chain, "TotalBytes": total, "ArtifactCount": artCount,
	}); err != nil {
		writeErr(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.Header().Set("Cache-Control", "no-store")

	root := c.Reference + "/"
	zw := zip.NewWriter(w)
	sums := map[string]string{}
	addBytes := func(name string, data []byte, hashed bool) error {
		f, err := zw.CreateHeader(&zip.FileHeader{Name: root + name, Method: zip.Deflate, Modified: generated})
		if err != nil {
			return err
		}
		if hashed {
			h := sha256.Sum256(data)
			sums[name] = hex.EncodeToString(h[:])
		}
		_, err = f.Write(data)
		return err
	}
	for _, f := range []struct {
		name   string
		data   []byte
		hashed bool
	}{
		{"README.txt", []byte(exportReadme), false},
		{"manifest.json", manifest, true},
		{"custody-log.jsonl", log.Bytes(), true},
		{"report.html", report.Bytes(), true},
	} {
		if err := addBytes(f.name, f.data, f.hashed); err != nil {
			slog.Error("export", "err", err)
			return
		}
	}

	var warnings []string
	for _, e := range evs {
		for _, a := range e.Files {
			got, err := s.copyArtifact(ctx, zw, root+a.Path, a, generated)
			if err != nil {
				slog.Error("export aborted", "artifact", a.ID, "err", err)
				panic(http.ErrAbortHandler) // leaves a truncated, obviously-invalid zip
			}
			sums[a.Path] = a.SHA256 // the recorded hash: verification exposes any mismatch
			if got != a.SHA256 {
				warnings = append(warnings, fmt.Sprintf("%s: recorded %s, exported bytes hash to %s", a.Path, a.SHA256, got))
			}
		}
		metas, err := s.loadFileMetadata(ctx, e.ID)
		if err != nil {
			slog.Error("export aborted", "evidence", e.ID, "err", err)
			panic(http.ErrAbortHandler)
		}
		for _, m := range metas {
			for _, a := range e.Files {
				if a.ID == m.ArtifactID && m.Status == "extracted" {
					// Same bytes as hashed in the custody log's artifact.metadata_extracted event.
					if err := addBytes(a.Path+".metadata.json", m.Fields, true); err != nil {
						slog.Error("export", "err", err)
						return
					}
				}
			}
		}
	}
	if len(warnings) > 0 {
		addBytes("INTEGRITY_WARNING.txt", []byte("The following files did not match their recorded hashes:\n\n"+strings.Join(warnings, "\n")+"\n"), false)
	}
	names := make([]string, 0, len(sums))
	for n := range sums {
		names = append(names, n)
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, n := range names {
		fmt.Fprintf(&sb, "%s  %s\n", sums[n], n)
	}
	addBytes("SHA256SUMS", []byte(sb.String()), false)
	zw.Close()
}

func (s *Server) copyArtifact(ctx context.Context, zw *zip.Writer, name string, a exportArtifact, mod time.Time) (string, error) {
	method := zip.Store // most evidence (images, video, archives) is already compressed
	if strings.HasPrefix(a.MimeType, "text/") || strings.Contains(a.MimeType, "json") || a.Role == "mhtml" {
		method = zip.Deflate
	}
	f, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: mod})
	if err != nil {
		return "", err
	}
	rc, err := s.Vault.Get(ctx, a.SHA256)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), rc); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
