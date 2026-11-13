// Package vaultclient is the API's client for the Lumen Vault service.
package vaultclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{BaseURL: baseURL, Token: token, HTTP: &http.Client{}}
}

type PutResult struct {
	SHA256       string `json:"sha256"`
	SHA1         string `json:"sha1"`
	MD5          string `json:"md5"`
	Size         int64  `json:"size"`
	DetectedType string `json:"detected_type"`
	Deduplicated bool   `json:"deduplicated"`
}

type VerifyResult struct {
	SHA256         string    `json:"sha256"`
	ComputedSHA256 string    `json:"computed_sha256"`
	OK             bool      `json:"ok"`
	Error          string    `json:"error,omitempty"`
	Size           int64     `json:"size"`
	CheckedAt      time.Time `json:"checked_at"`
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vault unreachable: %w", err)
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		return nil, fmt.Errorf("vault %s %s: %d %s", method, path, resp.StatusCode, msg)
	}
	return resp, nil
}

// Put streams r into the vault and returns the hashes computed there.
func (c *Client) Put(ctx context.Context, r io.Reader) (*PutResult, error) {
	resp, err := c.do(ctx, http.MethodPost, "/v1/blobs", r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out PutResult
	return &out, json.NewDecoder(resp.Body).Decode(&out)
}

// Get returns a stream of the decrypted blob. The caller must close it.
func (c *Client) Get(ctx context.Context, sha256 string) (io.ReadCloser, error) {
	resp, err := c.do(ctx, http.MethodGet, "/v1/blobs/"+sha256, nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (c *Client) Verify(ctx context.Context, sha256 string) (*VerifyResult, error) {
	resp, err := c.do(ctx, http.MethodPost, "/v1/blobs/"+sha256+"/verify", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out VerifyResult
	return &out, json.NewDecoder(resp.Body).Decode(&out)
}
