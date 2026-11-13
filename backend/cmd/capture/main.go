// Lumen Capture: forensic web page acquisition in a sandboxed headless browser.
//
// Every capture runs in a fresh browser with a throwaway profile (no cookies,
// cache or history shared between captures) and returns the screenshot,
// rendered HTML, an MHTML archive, and the network context (HTTP response
// headers, redirect chain, remote IP, TLS certificate and DNS records).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const version = "0.1.0"

type captureRequest struct {
	URL string `json:"url"`
}

type redirect struct {
	URL      string `json:"url"`
	Status   int64  `json:"status"`
	Location string `json:"location"`
}

type httpInfo struct {
	Status     int64             `json:"status"`
	StatusText string            `json:"status_text"`
	Protocol   string            `json:"protocol"`
	RemoteIP   string            `json:"remote_ip"`
	RemotePort int64             `json:"remote_port"`
	MimeType   string            `json:"mime_type"`
	Headers    map[string]string `json:"headers"`
}

type tlsInfo struct {
	Protocol    string   `json:"protocol"`
	Cipher      string   `json:"cipher"`
	KeyExchange string   `json:"key_exchange"`
	SubjectName string   `json:"subject_name"`
	Issuer      string   `json:"issuer"`
	SANs        []string `json:"sans"`
	ValidFrom   string   `json:"valid_from"`
	ValidTo     string   `json:"valid_to"`
}

type captureResult struct {
	RequestedURL string     `json:"requested_url"`
	FinalURL     string     `json:"final_url"`
	Title        string     `json:"title"`
	StartedAt    time.Time  `json:"started_at"`
	CompletedAt  time.Time  `json:"completed_at"`
	Browser      string     `json:"browser"`
	UserAgent    string     `json:"user_agent"`
	Viewport     string     `json:"viewport"`
	Redirects    []redirect `json:"redirects"`
	HTTP         *httpInfo  `json:"http"`
	TLS          *tlsInfo   `json:"tls"`
	DNS          dnsInfo    `json:"dns"`
	Tool         string     `json:"tool"`
	// Artifacts are base64 encoded by encoding/json.
	Screenshot []byte `json:"screenshot_png"`
	HTML       []byte `json:"html"`
	MHTML      []byte `json:"mhtml"`
	DNSZone    []byte `json:"dns_zone"`
}

var (
	allowPrivate = os.Getenv("CAPTURE_ALLOW_PRIVATE") == "true"
	sem          = make(chan struct{}, 2) // at most two browsers at once
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("POST /v1/capture", handleCapture)
	addr := env("LISTEN_ADDR", ":9100")
	slog.Info("lumen capture listening", "addr", addr)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func handleCapture(w http.ResponseWriter, r *http.Request) {
	var req captureRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	u, err := validateTarget(r.Context(), req.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sem <- struct{}{}
	defer func() { <-sem }()

	res, err := capture(r.Context(), u)
	if err != nil {
		slog.Error("capture failed", "url", u.String(), "err", err)
		http.Error(w, "capture failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	slog.Info("captured", "url", res.FinalURL, "status", res.HTTP.Status)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func validateTarget(ctx context.Context, raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, errors.New("URL must be an absolute http(s) URL")
	}
	if allowPrivate {
		return u, nil
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil {
		return nil, fmt.Errorf("cannot resolve %s: %w", u.Hostname(), err)
	}
	for _, ip := range ips {
		if ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() || ip.IP.IsUnspecified() {
			return nil, fmt.Errorf("%s resolves to a private address; set CAPTURE_ALLOW_PRIVATE=true to allow", u.Hostname())
		}
	}
	return u, nil
}

func capture(parent context.Context, u *url.URL) (*captureResult, error) {
	res := &captureResult{
		RequestedURL: u.String(),
		StartedAt:    time.Now().UTC(),
		Viewport:     "1440x900",
		Tool:         "Lumen Capture " + version,
		Redirects:    []redirect{},
	}

	profile, err := os.MkdirTemp("", "lumen-profile-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(profile)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.NoSandbox, // the container is the sandbox
		chromedp.UserDataDir(profile),
		chromedp.WindowSize(1440, 900),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("hide-scrollbars", true),
	)
	if p := os.Getenv("CHROME_PATH"); p != "" {
		opts = append(opts, chromedp.ExecPath(p))
	}
	if ua := os.Getenv("CAPTURE_USER_AGENT"); ua != "" {
		opts = append(opts, chromedp.UserAgent(ua))
	}

	// Detach from the request context so a client disconnect cannot leave a
	// half-written capture, but bound the total time.
	base, cancelBase := context.WithTimeout(context.WithoutCancel(parent), 2*time.Minute)
	defer cancelBase()
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(base, opts...)
	defer cancelAlloc()

	var mu sync.Mutex
	listener := func(ev any) {
		if e, ok := ev.(*network.EventRequestWillBeSent); ok && e.Type == network.ResourceTypeDocument && e.RedirectResponse != nil {
			mu.Lock()
			res.Redirects = append(res.Redirects, redirect{
				URL:      e.RedirectResponse.URL,
				Status:   e.RedirectResponse.Status,
				Location: e.Request.URL,
			})
			mu.Unlock()
		}
	}
	ctx, cancel := chromedp.NewContext(allocCtx, chromedp.WithListener(listener))
	defer cancel()

	// Start the browser on a context without its own timeout (see chromedp.Run docs).
	ver, err := chromedp.Run(ctx, func(ctx context.Context, t *chromedp.Target) (browser.GetVersionResult, error) {
		return cdp.Call(ctx, t, browser.GetVersion, cdp.Empty{})
	})
	if err != nil {
		return nil, fmt.Errorf("start browser: %w", err)
	}
	res.Browser = ver.Product
	res.UserAgent = ver.UserAgent
	if ua := os.Getenv("CAPTURE_USER_AGENT"); ua != "" {
		res.UserAgent = ua
	}

	navCtx, cancelNav := context.WithTimeout(ctx, 60*time.Second)
	defer cancelNav()
	resp, err := chromedp.Run(navCtx, chromedp.NavigateResponse(u.String()))
	if err != nil {
		return nil, fmt.Errorf("navigate: %w", err)
	}
	if resp != nil {
		res.HTTP = &httpInfo{
			Status: resp.Status, StatusText: resp.StatusText, Protocol: resp.Protocol,
			RemoteIP: resp.RemoteIPAddress, RemotePort: resp.RemotePort, MimeType: resp.MimeType,
			Headers: map[string]string{},
		}
		for k, v := range resp.Headers {
			res.HTTP.Headers[k] = fmt.Sprint(v)
		}
		if sd := resp.SecurityDetails; sd != nil {
			res.TLS = &tlsInfo{
				Protocol: sd.Protocol, Cipher: sd.Cipher, KeyExchange: sd.KeyExchange,
				SubjectName: sd.SubjectName, Issuer: sd.Issuer, SANs: sd.SanList,
				ValidFrom: epoch(sd.ValidFrom), ValidTo: epoch(sd.ValidTo),
			}
		}
	}

	// Let late scripts and lazy content settle before freezing the page.
	settle := 2 * time.Second
	if s, err := strconv.Atoi(os.Getenv("CAPTURE_SETTLE_SECONDS")); err == nil {
		settle = time.Duration(s) * time.Second
	}
	stepCtx, cancelStep := context.WithTimeout(ctx, 60*time.Second)
	defer cancelStep()
	if err := chromedp.Do(stepCtx, chromedp.Sleep(settle)); err != nil {
		return nil, err
	}
	if res.FinalURL, err = chromedp.Run(stepCtx, chromedp.Location()); err != nil {
		return nil, err
	}
	if res.Title, err = chromedp.Run(stepCtx, chromedp.Title()); err != nil {
		return nil, err
	}
	html, err := chromedp.Run(stepCtx, chromedp.Evaluate[string]("document.documentElement.outerHTML"))
	if err != nil {
		return nil, fmt.Errorf("html: %w", err)
	}
	res.HTML = []byte("<!DOCTYPE html>\n" + html)
	mhtml, err := chromedp.Run(stepCtx, func(ctx context.Context, t *chromedp.Target) (page.CaptureSnapshotResult, error) {
		return cdp.Call(ctx, t, page.CaptureSnapshot, page.CaptureSnapshotParams{Format: page.CaptureSnapshotFormatMhtml})
	})
	if err != nil {
		return nil, fmt.Errorf("mhtml: %w", err)
	}
	res.MHTML = []byte(mhtml.Data)
	if res.Screenshot, err = chromedp.Run(stepCtx, chromedp.FullScreenshot(100)); err != nil {
		return nil, fmt.Errorf("screenshot: %w", err)
	}

	// DNS for every host the navigation touched, in order.
	hosts := []string{u.Hostname()}
	for _, r := range res.Redirects {
		for _, raw := range []string{r.URL, r.Location} {
			if ru, err := url.Parse(raw); err == nil {
				hosts = append(hosts, ru.Hostname())
			}
		}
	}
	if fu, err := url.Parse(res.FinalURL); err == nil {
		hosts = append(hosts, fu.Hostname())
	}
	connected := ""
	if res.HTTP != nil {
		connected = res.HTTP.RemoteIP
	}
	res.DNS = collectDNS(parent, hosts, connected)
	res.DNSZone = zoneText(res.DNS, res.RequestedURL)
	res.CompletedAt = time.Now().UTC()
	return res, nil
}

func epoch(t cdp.TimeSinceEpoch) string {
	f := t.Float64()
	if f == 0 {
		return ""
	}
	return time.Unix(int64(f), 0).UTC().Format(time.RFC3339)
}
