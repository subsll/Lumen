// Package vaultclient is the API's client for the Lumen Vault service.
package vaultclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrLocked means the vault is locked and needs a security key to unlock.
var ErrLocked = errors.New("vault is locked")

// StatusError is a non-2xx answer from the vault.
type StatusError struct {
	Code    int
	Message string
}

func (e *StatusError) Error() string { return e.Message }

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
	if resp.StatusCode == http.StatusLocked {
		resp.Body.Close()
		return nil, ErrLocked
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		var body struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(msg, &body) == nil && body.Error != "" {
			return nil, &StatusError{resp.StatusCode, body.Error}
		}
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

// ---- security keys ----

type KeySlot struct {
	ID           string     `json:"id"`
	Type         string     `json:"type"`
	Label        string     `json:"label"`
	CredentialID string     `json:"credential_id,omitempty"`
	RPID         string     `json:"rp_id,omitempty"`
	Salt         string     `json:"salt,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
}

type KeyStatus struct {
	Protected     bool `json:"protected"`
	Sealed        bool `json:"sealed"`
	EnvKeyPresent bool `json:"env_key_present"`
	// RotationPending: the master key was replaced but not every file is re-encrypted yet.
	RotationPending bool      `json:"rotation_pending"`
	Slots           []KeySlot `json:"slots"`
}

type SlotResult struct {
	Slot          KeySlot `json:"slot"`
	RecoveryCode  string  `json:"recovery_code,omitempty"`
	RotationError string  `json:"rotation_error,omitempty"`
}

func (c *Client) json(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	resp, err := c.do(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) KeyStatus(ctx context.Context) (*KeyStatus, error) {
	var out KeyStatus
	return &out, c.json(ctx, http.MethodGet, "/v1/keys", nil, &out)
}

// UnlockRequest carries either a WebAuthn PRF secret or a recovery code.
type UnlockRequest struct {
	CredentialID string `json:"credential_id,omitempty"`
	Secret       string `json:"secret,omitempty"`
	RecoveryCode string `json:"recovery_code,omitempty"`
}

func (c *Client) Unlock(ctx context.Context, req UnlockRequest) (*SlotResult, error) {
	var out SlotResult
	return &out, c.json(ctx, http.MethodPost, "/v1/keys/unlock", req, &out)
}

func (c *Client) Seal(ctx context.Context) error {
	return c.json(ctx, http.MethodPost, "/v1/keys/seal", nil, nil)
}

type EnrollRequest struct {
	Label        string `json:"label"`
	CredentialID string `json:"credential_id"`
	RPID         string `json:"rp_id"`
	Salt         string `json:"salt"`
	Secret       string `json:"secret"`
}

func (c *Client) Enroll(ctx context.Context, req EnrollRequest) (*SlotResult, error) {
	var out SlotResult
	return &out, c.json(ctx, http.MethodPost, "/v1/keys/slots", req, &out)
}

func (c *Client) RemoveSlot(ctx context.Context, id string) (*SlotResult, error) {
	var out SlotResult
	return &out, c.json(ctx, http.MethodDelete, "/v1/keys/slots/"+id, nil, &out)
}

func (c *Client) RotateRecovery(ctx context.Context) (*SlotResult, error) {
	var out SlotResult
	return &out, c.json(ctx, http.MethodPost, "/v1/keys/recovery", nil, &out)
}
