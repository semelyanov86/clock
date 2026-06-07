// Package freedom is a read-only client for the Freedom24 / Tradernet session
// API. It authenticates with login+password in view-only mode (authByLogin with
// viewOnlyMode=true), keeps the returned SID, and reads the portfolio (getOPQ),
// market reviews (getMarketReviews), and instrument quotes (getSecurityInfo).
//
// Transport (confirmed against the official docs): every request is an HTTPS
// POST to the API gateway with a single form field "q" holding a JSON string
// {"cmd":..,"SID":..,"params":..}. authByLogin returns {"success":true,
// "logged":true,"SID":".."}; the SID is echoed back on every subsequent call.
// getOPQ returns {"OPQ":{"ps":{"pos":[..],"acc":[..]},"homeCurrency":".."}}.
//
// The news (getMarketReviews) and quote (getSecurityInfo) response shapes are
// not fully documented; their parsing is defensive and raw responses are logged
// at debug level so the mappings can be verified during E2E testing.
package freedom

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/semelyanov86/clock/internal/model"
)

// DefaultAPIURL is the Tradernet API gateway used by the official docs.
const DefaultAPIURL = "https://tradernet.com/api"

// Client is a session-authenticated Freedom24 client.
type Client struct {
	login    string
	password string
	userID   string
	baseURL  string
	httpc    *http.Client
	log      *slog.Logger

	authMu sync.Mutex // serialises authByLogin so concurrent fetchers issue one login
	mu     sync.Mutex
	sid    string
}

// maxResponseBytes caps a single API response to guard against runaway reads.
const maxResponseBytes = 4 << 20

// Option customises a Client.
type Option func(*Client)

// WithHTTPClient overrides the HTTP client (used in tests).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpc = h } }

// New returns a Freedom24 client. apiURL defaults to DefaultAPIURL when empty.
func New(login, password, userID, apiURL string, timeout time.Duration, log *slog.Logger, opts ...Option) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("cookie jar: %w", err)
	}
	if log == nil {
		log = slog.Default()
	}
	if apiURL == "" {
		apiURL = DefaultAPIURL
	}
	c := &Client{
		login:    login,
		password: password,
		userID:   userID,
		baseURL:  strings.TrimRight(apiURL, "/"),
		httpc:    &http.Client{Timeout: timeout, Jar: jar},
		log:      log,
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// call sends a cmd with params, embedding the current SID, and returns the raw
// response body.
func (c *Client) call(ctx context.Context, cmd string, params map[string]any) (json.RawMessage, error) {
	c.mu.Lock()
	sid := c.sid
	c.mu.Unlock()

	payload := map[string]any{"cmd": cmd}
	if sid != "" {
		payload["SID"] = sid
	}
	if params != nil {
		payload["params"] = params
	}
	jb, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal %s payload: %w", cmd, err)
	}
	form := url.Values{"q": {string(jb)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("new %s request: %w", cmd, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", cmd, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("%s: read response: %w", cmd, err)
	}
	c.log.Debug("freedom response", "cmd", cmd, "http", resp.StatusCode, "body", truncate(body))
	if resp.StatusCode != http.StatusOK {
		return body, fmt.Errorf("%s: http %d", cmd, resp.StatusCode)
	}
	return body, nil
}

// authenticate performs authByLogin in view-only mode and stores the SID.
func (c *Client) authenticate(ctx context.Context) error {
	params := map[string]any{
		"login":        c.login,
		"password":     c.password,
		"viewOnlyMode": true,
		"rememberMe":   1,
		"getAccounts":  false,
	}
	if c.userID != "" {
		if id, err := strconv.Atoi(c.userID); err == nil {
			params["userId"] = id
		} else {
			params["userId"] = c.userID
		}
	}
	raw, err := c.call(ctx, "authByLogin", params)
	if err != nil {
		return err
	}
	sid, err := parseAuth(raw)
	if err != nil {
		return fmt.Errorf("authByLogin: %w", err)
	}
	c.mu.Lock()
	c.sid = sid
	c.mu.Unlock()
	return nil
}

// withSession ensures an authenticated session, runs fn, and retries once after
// re-authenticating when the response indicates an expired/invalid session.
func (c *Client) withSession(ctx context.Context, fn func(context.Context) (json.RawMessage, error)) (json.RawMessage, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return nil, err
	}

	raw, err := fn(ctx)
	if err != nil {
		return nil, err // transport/HTTP errors propagate unchanged
	}
	if !looksUnauthorized(raw) {
		return raw, nil
	}

	// The session expired: re-authenticate once and retry.
	if reauthErr := c.reauth(ctx); reauthErr != nil {
		return nil, fmt.Errorf("session expired and re-auth failed: %w", reauthErr)
	}
	return fn(ctx)
}

// ensureAuth logs in once if there is no session yet. authMu serialises
// concurrent callers so the parallel startup fetchers issue a single login.
func (c *Client) ensureAuth(ctx context.Context) error {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	c.mu.Lock()
	sid := c.sid
	c.mu.Unlock()
	if sid != "" {
		return nil
	}
	return c.authenticate(ctx)
}

// reauth forces a fresh login, serialised with ensureAuth.
func (c *Client) reauth(ctx context.Context) error {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	return c.authenticate(ctx)
}

// Portfolio reads the account positions and balances via getOPQ.
func (c *Client) Portfolio(ctx context.Context) (model.Portfolio, error) {
	raw, err := c.withSession(ctx, func(ctx context.Context) (json.RawMessage, error) {
		return c.call(ctx, "getOPQ", map[string]any{})
	})
	if err != nil {
		return model.Portfolio{}, err
	}
	return parsePortfolio(raw)
}

// News reads up to n market-review headlines via getMarketReviews.
//
// E2E: confirm the response container and item field names; adjust extractNews.
func (c *Client) News(ctx context.Context, n int) ([]model.NewsItem, error) {
	if n <= 0 {
		n = 9
	}
	raw, err := c.withSession(ctx, func(ctx context.Context) (json.RawMessage, error) {
		return c.call(ctx, "getMarketReviews", map[string]any{
			"category": []string{"any"},
			"author":   []string{"any"},
			"page":     map[string]any{"skip": 0, "take": n},
		})
	})
	if err != nil {
		return nil, err
	}
	return extractNews(raw, n), nil
}

// Quotes reads a snapshot for each symbol via getSecurityInfo.
//
// E2E: confirm the command and the last-price / day-change field names
// (ltp/chg/pcp/x_curr) against real symbols; adjust parseQuote.
func (c *Client) Quotes(ctx context.Context, symbols []string) (map[string]model.Instrument, error) {
	out := make(map[string]model.Instrument, len(symbols))
	for _, sym := range symbols {
		sym = strings.TrimSpace(sym)
		if sym == "" {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		raw, err := c.withSession(ctx, func(ctx context.Context) (json.RawMessage, error) {
			return c.call(ctx, "getSecurityInfo", map[string]any{"ticker": sym, "sup": false})
		})
		if err != nil {
			c.log.Debug("quote fetch failed", "symbol", sym, "err", err)
			continue
		}
		if inst, ok := parseQuote(sym, raw); ok {
			out[sym] = inst
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no quotes resolved for %v", symbols)
	}
	return out, nil
}

func truncate(b []byte) string {
	const max = 500
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}
