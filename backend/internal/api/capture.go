package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type captureResponse struct {
	RequestedURL string          `json:"requested_url"`
	FinalURL     string          `json:"final_url"`
	Title        string          `json:"title"`
	StartedAt    time.Time       `json:"started_at"`
	CompletedAt  time.Time       `json:"completed_at"`
	Browser      string          `json:"browser"`
	UserAgent    string          `json:"user_agent"`
	Viewport     string          `json:"viewport"`
	Redirects    json.RawMessage `json:"redirects"`
	HTTP         json.RawMessage `json:"http"`
	TLS          json.RawMessage `json:"tls"`
	DNS          json.RawMessage `json:"dns"`
	Tool         string          `json:"tool"`
	Screenshot   []byte          `json:"screenshot_png"`
	HTML         []byte          `json:"html"`
	MHTML        []byte          `json:"mhtml"`
	DNSZone      []byte          `json:"dns_zone"`
}

func slugHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "capture"
	}
	return strings.ReplaceAll(u.Hostname(), ".", "_")
}

func (s *Server) captureWeb(w http.ResponseWriter, r *http.Request) {
	caseID, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		URL         string   `json:"url"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(in.URL) == "" {
		writeErr(w, badRequest("url is required"))
		return
	}
	ctx := r.Context()

	body, _ := json.Marshal(map[string]string{"url": strings.TrimSpace(in.URL)})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.CaptureURL+"/v1/capture", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		writeErr(w, &apiError{http.StatusBadGateway, "capture service unreachable: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		writeErr(w, &apiError{http.StatusBadGateway, strings.TrimSpace(string(msg))})
		return
	}
	var cap captureResponse
	if err := json.NewDecoder(resp.Body).Decode(&cap); err != nil {
		writeErr(w, err)
		return
	}

	stamp := cap.CompletedAt.UTC().Format("20060102T150405Z")
	base := slugHost(cap.FinalURL) + "_" + stamp
	arts := []newArtifact{}
	for _, a := range []struct {
		role, name, mime string
		data             []byte
	}{
		{"screenshot", base + ".png", "image/png", cap.Screenshot},
		{"html", base + ".html", "text/html; charset=utf-8", cap.HTML},
		{"mhtml", base + ".mhtml", "multipart/related", cap.MHTML},
		{"dns", base + ".dns.txt", "text/plain; charset=utf-8", cap.DNSZone},
	} {
		if len(a.data) == 0 && a.role == "dns" {
			continue
		}
		put, err := s.Vault.Put(ctx, bytes.NewReader(a.data))
		if err != nil {
			writeErr(w, err)
			return
		}
		arts = append(arts, newArtifact{Role: a.role, Filename: a.name, MimeType: a.mime, Put: put})
	}

	// The metadata record binds the bundle: it lists the hashes of every other
	// artifact, and is itself hashed and stored.
	meta := map[string]any{
		"lumen_capture_format": 1,
		"requested_url":        cap.RequestedURL,
		"final_url":            cap.FinalURL,
		"title":                cap.Title,
		"started_at":           cap.StartedAt,
		"completed_at":         cap.CompletedAt,
		"browser":              cap.Browser,
		"user_agent":           cap.UserAgent,
		"viewport":             cap.Viewport,
		"tool":                 cap.Tool,
		"redirects":            cap.Redirects,
		"http":                 cap.HTTP,
		"tls":                  cap.TLS,
		"dns":                  cap.DNS,
		"artifacts":            artifactDetails(arts),
	}
	metaJSON, _ := json.MarshalIndent(meta, "", "  ")
	put, err := s.Vault.Put(ctx, bytes.NewReader(metaJSON))
	if err != nil {
		writeErr(w, err)
		return
	}
	arts = append(arts, newArtifact{Role: "metadata", Filename: base + ".metadata.json", MimeType: "application/json", Put: put})

	summary, _ := json.Marshal(map[string]any{
		"requested_url": cap.RequestedURL, "final_url": cap.FinalURL, "title": cap.Title,
		"started_at": cap.StartedAt, "completed_at": cap.CompletedAt, "browser": cap.Browser,
		"user_agent": cap.UserAgent, "viewport": cap.Viewport, "redirects": cap.Redirects,
		"http": cap.HTTP, "tls": cap.TLS, "dns": cap.DNS,
	})

	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = cap.Title
	}
	if title == "" {
		title = cap.FinalURL
	}
	ev := newEvidence{
		CaseID: caseID, Kind: "web_capture", Title: title,
		Description:       strings.TrimSpace(in.Description),
		Source:            cap.FinalURL,
		AcquisitionMethod: fmt.Sprintf("%s (%s)", cap.Tool, cap.Browser),
		AcquiredAt:        cap.CompletedAt,
		Location:          "Lumen Vault (encrypted)",
		Tags:              in.Tags,
		Capture:           summary,
	}
	var evID string
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		id, ref, err := s.insertEvidence(ctx, tx, ev, arts)
		if err != nil {
			return err
		}
		evID = id
		_, err = s.record(ctx, tx, eventInput{CaseID: caseID, EvidenceID: id, Action: "evidence.captured", Details: map[string]any{
			"exhibit": ref, "requested_url": cap.RequestedURL, "final_url": cap.FinalURL,
			"started_at": cap.StartedAt, "completed_at": cap.CompletedAt, "browser": cap.Browser,
			"artifacts": artifactDetails(arts),
		}})
		return err
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	list, err := s.queryEvidence(context.WithoutCancel(ctx), "WHERE e.id = $1", evID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, list[0])
}
