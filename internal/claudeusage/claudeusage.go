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
	"fmt"
	"io"
	"log/slog"
	"net/http"
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
// so a token refreshed (by this service or by Claude Code) is picked up
// automatically.
type TokenFunc func(ctx context.Context) (string, error)

// Client probes the Anthropic API for unified rate-limit usage.
type Client struct {
	apiURL string
	model  string
	token  TokenFunc
	httpc  *http.Client
	store  *credStore
	log    *slog.Logger
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

// WithLogger attaches a logger so token-refresh attempts and outcomes are
// visible in the service journal.
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) {
		c.log = l
		c.store.log = l
	}
}

// WithOAuthRefresh enables in-process refresh of the OAuth token: when the token
// is near expiry (or rejected with 401) the client exchanges the stored refresh
// token for a fresh one and rewrites the credentials file. Empty url/clientID
// fall back to the Claude Code defaults.
func WithOAuthRefresh(tokenURL, clientID string) Option {
	return func(c *Client) {
		c.store.refresh = true
		c.store.tokenURL = orDefault(tokenURL, defaultOAuthTokenURL)
		c.store.clientID = orDefault(clientID, defaultOAuthClientID)
	}
}

// New returns a client that reads the OAuth token from the Claude Code
// credentials file at credentialsPath. The path is read on every fetch so a
// freshly refreshed token is used automatically.
func New(credentialsPath string, timeout time.Duration, opts ...Option) *Client {
	store := &credStore{
		path:  credentialsPath,
		httpc: &http.Client{Timeout: timeout},
	}
	c := &Client{
		apiURL: defaultAPIURL,
		model:  defaultModel,
		httpc:  &http.Client{Timeout: timeout},
		store:  store,
	}
	c.token = store.accessToken
	for _, o := range opts {
		o(c)
	}
	return c
}

// Fetch probes the API and returns the 5-hour and weekly usage windows. When
// the probe is rejected with 401 and refresh is enabled, it refreshes the token
// once and retries.
func (c *Client) Fetch(ctx context.Context) (model.ProviderUsage, error) {
	usage, status, err := c.probe(ctx)
	if err != nil && status == http.StatusUnauthorized && c.store != nil && c.store.refresh {
		if rerr := c.store.refreshOn401(ctx); rerr != nil {
			if c.log != nil {
				c.log.Warn("claude oauth refresh after 401 failed", "err", rerr)
			}
		} else {
			usage, _, err = c.probe(ctx)
		}
	}
	return usage, err
}

// ForceRefresh refreshes the OAuth token immediately and reports the old and new
// expiry. It powers the `--refresh-claude-token` command and requires refresh to
// be enabled (see WithOAuthRefresh).
func (c *Client) ForceRefresh(ctx context.Context) (oldExpiry, newExpiry time.Time, err error) {
	if c.store == nil || !c.store.refresh {
		return time.Time{}, time.Time{}, fmt.Errorf("oauth refresh is not enabled")
	}
	return c.store.forceRefresh(ctx)
}

// probe sends one minimal Claude Code message and reads the unified rate-limit
// headers, returning the HTTP status so callers can react to auth failures.
func (c *Client) probe(ctx context.Context) (model.ProviderUsage, int, error) {
	token, err := c.token(ctx)
	if err != nil {
		return model.ProviderUsage{}, 0, fmt.Errorf("read oauth token: %w", err)
	}

	body, err := json.Marshal(map[string]any{
		"model":      c.model,
		"max_tokens": 1,
		"system":     systemPrompt,
		"messages":   []map[string]any{{"role": "user", "content": "ping"}},
	})
	if err != nil {
		return model.ProviderUsage{}, 0, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return model.ProviderUsage{}, 0, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("authorization", "Bearer "+token)
	req.Header.Set("anthropic-version", anthropicVersion)
	req.Header.Set("anthropic-beta", oauthBeta)
	req.Header.Set("content-type", "application/json")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return model.ProviderUsage{}, 0, fmt.Errorf("probe anthropic api: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// The body is unused; drain it (bounded) so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	usage, ok := parseUsage(resp.Header)
	if !ok {
		return model.ProviderUsage{}, resp.StatusCode, fmt.Errorf("no unified rate-limit headers in response (http %d)", resp.StatusCode)
	}
	usage.Updated = time.Now()
	return usage, resp.StatusCode, nil
}

// parseUsage extracts the unified rate-limit windows from the response headers.
// It returns ok=false when the 5-hour utilization header is absent (e.g. an
// auth failure that carries no rate-limit data).
func parseUsage(h http.Header) (model.ProviderUsage, bool) {
	u5, ok := parseFloat(h.Get("anthropic-ratelimit-unified-5h-utilization"))
	if !ok {
		return model.ProviderUsage{}, false
	}
	u7, weeklyValid := parseFloat(h.Get("anthropic-ratelimit-unified-7d-utilization"))
	return model.ProviderUsage{
		Primary: model.UsageWindow{
			Utilization: u5,
			Duration:    5 * time.Hour,
			ResetAt:     parseUnix(h.Get("anthropic-ratelimit-unified-5h-reset")),
			Valid:       true,
		},
		Secondary: model.UsageWindow{
			Utilization: u7,
			Duration:    7 * 24 * time.Hour,
			ResetAt:     parseUnix(h.Get("anthropic-ratelimit-unified-7d-reset")),
			Valid:       weeklyValid,
		},
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
