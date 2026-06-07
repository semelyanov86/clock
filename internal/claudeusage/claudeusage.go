// Package claudeusage reads Claude's unified rate-limit usage — the same
// 5-hour and weekly windows shown by Claude Code's /usage — by probing the
// Anthropic API with the Claude Code OAuth token and reading the
// anthropic-ratelimit-unified-* response headers.
//
// This relies on the subscription OAuth token (from the Claude Code
// credentials file) and on rate-limit response headers; both are outside the
// documented API surface and may change without notice. The probe sends a
// 1-token message, so each poll consumes a negligible amount of the same
// quota it reports.
package claudeusage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/semelyanov86/clock/internal/model"
)

const (
	defaultAPIURL = "https://api.anthropic.com"
	defaultModel  = "claude-haiku-4-5-20251001"

	// systemPrompt is required: the OAuth token is only accepted on requests
	// that identify as Claude Code.
	systemPrompt = "You are Claude Code, Anthropic's official CLI for Claude."

	anthropicVersion = "2023-06-01"
	oauthBeta        = "oauth-2025-04-20"
)

// TokenFunc returns the current OAuth access token. It is called on every fetch
// so a token refreshed by Claude Code is picked up automatically.
type TokenFunc func() (string, error)

// Client probes the Anthropic API for unified rate-limit usage.
type Client struct {
	apiURL string
	model  string
	token  TokenFunc
	httpc  *http.Client
}

// Option customises a Client.
type Option func(*Client)

// WithHTTPClient overrides the HTTP client (used in tests).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpc = h } }

// WithBaseURL overrides the API base URL (used in tests).
func WithBaseURL(u string) Option {
	return func(c *Client) { c.apiURL = strings.TrimRight(u, "/") }
}

// WithModel overrides the probe model (empty keeps the default).
func WithModel(m string) Option {
	return func(c *Client) {
		if m != "" {
			c.model = m
		}
	}
}

// WithTokenFunc overrides how the OAuth token is obtained (used in tests).
func WithTokenFunc(f TokenFunc) Option { return func(c *Client) { c.token = f } }

// New returns a client that reads the OAuth token from the Claude Code
// credentials file at credentialsPath. The path is read on every fetch so a
// token refreshed by Claude Code is used automatically.
func New(credentialsPath string, timeout time.Duration, opts ...Option) *Client {
	c := &Client{
		apiURL: defaultAPIURL,
		model:  defaultModel,
		token:  func() (string, error) { return tokenFromFile(credentialsPath) },
		httpc:  &http.Client{Timeout: timeout},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Fetch probes the API and returns the 5-hour and weekly usage windows.
func (c *Client) Fetch(ctx context.Context) (model.ClaudeUsage, error) {
	token, err := c.token()
	if err != nil {
		return model.ClaudeUsage{}, fmt.Errorf("read oauth token: %w", err)
	}

	body, err := json.Marshal(map[string]any{
		"model":      c.model,
		"max_tokens": 1,
		"system":     systemPrompt,
		"messages":   []map[string]any{{"role": "user", "content": "ping"}},
	})
	if err != nil {
		return model.ClaudeUsage{}, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return model.ClaudeUsage{}, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("authorization", "Bearer "+token)
	req.Header.Set("anthropic-version", anthropicVersion)
	req.Header.Set("anthropic-beta", oauthBeta)
	req.Header.Set("content-type", "application/json")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return model.ClaudeUsage{}, fmt.Errorf("probe anthropic api: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// The body is unused; drain it (bounded) so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	usage, ok := parseUsage(resp.Header)
	if !ok {
		return model.ClaudeUsage{}, fmt.Errorf("no unified rate-limit headers in response (http %d)", resp.StatusCode)
	}
	usage.Updated = time.Now()
	return usage, nil
}

// parseUsage extracts the unified rate-limit windows from the response headers.
// It returns ok=false when the 5-hour utilization header is absent (e.g. an
// auth failure that carries no rate-limit data).
func parseUsage(h http.Header) (model.ClaudeUsage, bool) {
	u5, ok := parseFloat(h.Get("anthropic-ratelimit-unified-5h-utilization"))
	if !ok {
		return model.ClaudeUsage{}, false
	}
	u7, _ := parseFloat(h.Get("anthropic-ratelimit-unified-7d-utilization"))
	return model.ClaudeUsage{
		Block5h: model.ClaudeWindow{Utilization: u5, ResetAt: parseUnix(h.Get("anthropic-ratelimit-unified-5h-reset"))},
		Weekly:  model.ClaudeWindow{Utilization: u7, ResetAt: parseUnix(h.Get("anthropic-ratelimit-unified-7d-reset"))},
		Valid:   true,
	}, true
}

func parseFloat(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func parseUnix(s string) time.Time {
	sec, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

// tokenFromFile reads the Claude Code OAuth access token from the credentials
// JSON file. The file is small and trusted; it is read fresh on each call.
func tokenFromFile(path string) (string, error) {
	if path == "" {
		return "", errors.New("empty credentials path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read credentials file: %w", err)
	}
	var creds struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", fmt.Errorf("parse credentials file: %w", err)
	}
	if creds.ClaudeAiOauth.AccessToken == "" {
		return "", errors.New("no claudeAiOauth.accessToken in credentials file")
	}
	return creds.ClaudeAiOauth.AccessToken, nil
}
