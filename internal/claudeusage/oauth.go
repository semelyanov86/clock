package claudeusage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	// defaultOAuthTokenURL is Claude Code's OAuth token endpoint. It migrated from
	// console.anthropic.com to platform.claude.com; it is overridable via ENV in
	// case it moves again.
	defaultOAuthTokenURL = "https://platform.claude.com/v1/oauth/token"
	// defaultOAuthClientID is the public Claude Code OAuth client id.
	defaultOAuthClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"

	// refreshMargin is how long before expiry a token is refreshed proactively.
	// With an ~8h token lifetime and a 5-minute poll, the refresh fires only in
	// the final window and stops on the first success — it does not hammer the
	// endpoint.
	refreshMargin = 30 * time.Minute

	// refreshUserAgent presents the refresh request as a browser. The token
	// endpoint sits behind Cloudflare, which has been seen to flag headless CLI
	// refreshes as bot traffic; this mirrors the Tradernet client's WAF fix.
	refreshUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

// oauthCreds is the subset of the Claude Code credentials file we read.
type oauthCreds struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64 // unix milliseconds, 0 when unknown
}

// credStore reads (and, when refresh is enabled, refreshes) the Claude Code
// OAuth token in the credentials JSON file. All access is serialised by mu so a
// proactive refresh and an on-401 refresh cannot race each other.
type credStore struct {
	path     string
	tokenURL string
	clientID string
	httpc    *http.Client
	refresh  bool
	log      *slog.Logger
	nowFn    func() time.Time

	mu sync.Mutex
}

func (s *credStore) now() time.Time {
	if s.nowFn != nil {
		return s.nowFn()
	}
	return time.Now()
}

// accessToken returns a usable access token, refreshing it first when it is
// enabled and the current token is within refreshMargin of expiry. A failed
// proactive refresh is non-fatal as long as the current token has not expired:
// the caller falls back to it and the next poll retries.
func (s *credStore) accessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.readCreds()
	if err != nil {
		return "", err
	}

	if s.refresh && creds.RefreshToken != "" && s.expiringSoon(creds.ExpiresAt) {
		if rerr := s.refreshLocked(ctx, creds.RefreshToken); rerr != nil {
			if s.log != nil {
				s.log.Warn("claude oauth proactive refresh failed", "err", rerr)
			}
			if s.expired(creds.ExpiresAt) {
				return "", fmt.Errorf("oauth token expired and refresh failed: %w", rerr)
			}
		} else if re, err := s.readCreds(); err == nil {
			creds = re
		}
	}

	if creds.AccessToken == "" {
		return "", errors.New("no claudeAiOauth.accessToken in credentials file")
	}
	return creds.AccessToken, nil
}

// forceRefresh refreshes the token regardless of its expiry. It is used by the
// on-401 retry and by the `--refresh-claude-token` command, and returns the old
// and new expiry instants for reporting.
func (s *credStore) forceRefresh(ctx context.Context) (oldExpiry, newExpiry time.Time, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	creds, err := s.readCreds()
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if creds.RefreshToken == "" {
		return time.Time{}, time.Time{}, errors.New("no claudeAiOauth.refreshToken in credentials file")
	}
	oldExpiry = msToTime(creds.ExpiresAt)
	if err := s.refreshLocked(ctx, creds.RefreshToken); err != nil {
		return oldExpiry, time.Time{}, err
	}
	re, err := s.readCreds()
	if err != nil {
		return oldExpiry, time.Time{}, err
	}
	return oldExpiry, msToTime(re.ExpiresAt), nil
}

// refreshLocked performs the refresh_token grant and writes the rotated token
// back to the file. The caller must hold s.mu.
func (s *credStore) refreshLocked(ctx context.Context, refreshToken string) error {
	body, err := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     s.clientID,
	})
	if err != nil {
		return fmt.Errorf("encode refresh request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new refresh request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	req.Header.Set("user-agent", refreshUserAgent)

	resp, err := s.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("post token endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token endpoint http %d: %s", resp.StatusCode, truncate(data, 200))
	}

	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &tr); err != nil {
		return fmt.Errorf("decode token response: %w", err)
	}
	if tr.AccessToken == "" {
		return errors.New("token response missing access_token")
	}

	newRefresh := tr.RefreshToken
	if newRefresh == "" {
		newRefresh = refreshToken // server did not rotate the refresh token
	}
	// Anthropic returns expires_in; if it is ever absent, re-check in an hour
	// rather than treating the token as immediately stale (which would loop).
	ttl := time.Duration(tr.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	newExpMs := s.now().Add(ttl).UnixMilli()

	if err := s.writeRefreshed(tr.AccessToken, newRefresh, newExpMs); err != nil {
		return err
	}
	if s.log != nil {
		s.log.Info("refreshed claude oauth token", "expiresAt", msToTime(newExpMs).Format(time.RFC3339))
	}
	return nil
}

// writeRefreshed rewrites only the three OAuth token fields, preserving every
// other key in the file (scopes, subscriptionType, …). It decodes with
// UseNumber so large integers (expiresAt) round-trip exactly instead of being
// mangled into float scientific notation, then writes atomically at 0600.
func (s *credStore) writeRefreshed(access, refresh string, expMs int64) error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("read credentials file: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil {
		return fmt.Errorf("parse credentials file: %w", err)
	}

	oauth, ok := root["claudeAiOauth"].(map[string]any)
	if !ok {
		oauth = map[string]any{}
		root["claudeAiOauth"] = oauth
	}
	oauth["accessToken"] = access
	oauth["refreshToken"] = refresh
	oauth["expiresAt"] = json.Number(strconv.FormatInt(expMs, 10))

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	return atomicWrite(s.path, out, 0o600)
}

func (s *credStore) readCreds() (oauthCreds, error) {
	if s.path == "" {
		return oauthCreds{}, errors.New("empty credentials path")
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return oauthCreds{}, fmt.Errorf("read credentials file: %w", err)
	}
	var c struct {
		ClaudeAiOauth struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
			ExpiresAt    int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return oauthCreds{}, fmt.Errorf("parse credentials file: %w", err)
	}
	return oauthCreds{
		AccessToken:  c.ClaudeAiOauth.AccessToken,
		RefreshToken: c.ClaudeAiOauth.RefreshToken,
		ExpiresAt:    c.ClaudeAiOauth.ExpiresAt,
	}, nil
}

func (s *credStore) expiringSoon(expMs int64) bool {
	if expMs <= 0 {
		return false // unknown expiry: leave it to the on-401 path
	}
	return msToTime(expMs).Sub(s.now()) < refreshMargin
}

func (s *credStore) expired(expMs int64) bool {
	if expMs <= 0 {
		return false
	}
	return !s.now().Before(msToTime(expMs))
}

func msToTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// atomicWrite writes data to a temp file in the target directory and renames it
// into place, so a concurrent reader never sees a half-written credentials file.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".credentials-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

func truncate(b []byte, n int) string {
	s := string(bytes.TrimSpace(b))
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
