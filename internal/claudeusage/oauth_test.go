package claudeusage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fixedNow returns a deterministic clock for expiry math in tests.
func fixedNow() time.Time { return time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC) }

func writeCreds(t *testing.T, dir, access, refresh string, expiresAt int64) string {
	t.Helper()
	path := filepath.Join(dir, ".credentials.json")
	body := `{
  "claudeAiOauth": {
    "accessToken": "` + access + `",
    "refreshToken": "` + refresh + `",
    "expiresAt": ` + strconv.FormatInt(expiresAt, 10) + `,
    "scopes": ["user:inference"],
    "subscriptionType": "team"
  }
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// tokenServer returns a mock OAuth token endpoint plus a pointer to the last
// decoded request body, so tests can assert on what was sent.
func tokenServer(t *testing.T, status int, resp map[string]any) (*httptest.Server, *map[string]any, *string) {
	t.Helper()
	var gotBody map[string]any
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("user-agent")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &gotBody, &gotUA
}

func TestProactiveRefreshRewritesFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := fixedNow()
	// Token expires in 10 minutes — inside the refresh margin.
	path := writeCreds(t, dir, "old-access", "refresh-1", now.Add(10*time.Minute).UnixMilli())

	srv, gotBody, gotUA := tokenServer(t, http.StatusOK, map[string]any{
		"access_token":  "new-access",
		"refresh_token": "refresh-2",
		"expires_in":    28800,
	})

	s := &credStore{
		path:     path,
		tokenURL: srv.URL,
		clientID: "test-client",
		httpc:    srv.Client(),
		refresh:  true,
		nowFn:    func() time.Time { return now },
	}

	tok, err := s.accessToken(context.Background())
	if err != nil {
		t.Fatalf("accessToken: %v", err)
	}
	if tok != "new-access" {
		t.Errorf("token = %q, want new-access", tok)
	}

	// The request carried the refresh grant with the stored refresh token.
	if (*gotBody)["grant_type"] != "refresh_token" || (*gotBody)["refresh_token"] != "refresh-1" || (*gotBody)["client_id"] != "test-client" {
		t.Errorf("refresh request body = %v", *gotBody)
	}
	if *gotUA == "" {
		t.Error("refresh request sent no User-Agent")
	}

	// The file now holds the rotated tokens and an integer expiresAt, and keeps
	// the unrelated fields intact.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantExp := strconv.FormatInt(now.Add(28800*time.Second).UnixMilli(), 10)
	got := string(raw)
	for _, want := range []string{`"new-access"`, `"refresh-2"`, wantExp, `"subscriptionType": "team"`, `"user:inference"`} {
		if !strings.Contains(got, want) {
			t.Errorf("rewritten file missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "e+") || strings.Contains(got, "E+") {
		t.Errorf("expiresAt was mangled into scientific notation:\n%s", got)
	}

	re, err := s.readCreds()
	if err != nil {
		t.Fatal(err)
	}
	if re.ExpiresAt != now.Add(28800*time.Second).UnixMilli() {
		t.Errorf("expiresAt = %d, want %s", re.ExpiresAt, wantExp)
	}
}

func TestNoRefreshWhenTokenFresh(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := fixedNow()
	path := writeCreds(t, dir, "fresh-access", "refresh-1", now.Add(3*time.Hour).UnixMilli())

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	t.Cleanup(srv.Close)

	s := &credStore{path: path, tokenURL: srv.URL, clientID: "c", httpc: srv.Client(), refresh: true, nowFn: func() time.Time { return now }}
	tok, err := s.accessToken(context.Background())
	if err != nil {
		t.Fatalf("accessToken: %v", err)
	}
	if tok != "fresh-access" {
		t.Errorf("token = %q, want fresh-access", tok)
	}
	if called {
		t.Error("token endpoint was called for a token that is not near expiry")
	}
}

func TestRefreshFailureKeepsValidToken(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := fixedNow()
	srv, _, _ := tokenServer(t, http.StatusTooManyRequests, map[string]any{"error": "rate_limited"})

	// Near expiry but not yet expired: a failed refresh must fall back silently.
	valid := writeCreds(t, dir, "still-valid", "refresh-1", now.Add(10*time.Minute).UnixMilli())
	s := &credStore{path: valid, tokenURL: srv.URL, clientID: "c", httpc: srv.Client(), refresh: true, nowFn: func() time.Time { return now }}
	tok, err := s.accessToken(context.Background())
	if err != nil {
		t.Fatalf("accessToken should fall back, got error: %v", err)
	}
	if tok != "still-valid" {
		t.Errorf("token = %q, want still-valid", tok)
	}

	// Already expired and refresh fails: now it is a hard error.
	expired := writeCreds(t, dir, "dead", "refresh-1", now.Add(-time.Minute).UnixMilli())
	s2 := &credStore{path: expired, tokenURL: srv.URL, clientID: "c", httpc: srv.Client(), refresh: true, nowFn: func() time.Time { return now }}
	if _, err := s2.accessToken(context.Background()); err == nil {
		t.Error("expected error when token is expired and refresh fails")
	}
}

func TestRefreshBackoffSuppressesRetries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := fixedNow()
	clock := now
	// Near expiry (inside the 30m margin) but still valid well past the backoff
	// window, so a failed refresh falls back silently and each call would
	// otherwise try again.
	path := writeCreds(t, dir, "still-valid", "refresh-1", now.Add(25*time.Minute).UnixMilli())

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate_limited"}`))
	}))
	t.Cleanup(srv.Close)

	s := &credStore{path: path, tokenURL: srv.URL, clientID: "c", httpc: srv.Client(), refresh: true, nowFn: func() time.Time { return clock }}

	// Three polls in quick succession: only the first should hit the endpoint;
	// the next two are inside the 15m backoff window.
	for i := 0; i < 3; i++ {
		if _, err := s.accessToken(context.Background()); err != nil {
			t.Fatalf("accessToken #%d: %v", i, err)
		}
	}
	if calls != 1 {
		t.Fatalf("endpoint hit %d times during backoff, want 1", calls)
	}

	// After the backoff window elapses, a new attempt is allowed.
	clock = now.Add(16 * time.Minute)
	if _, err := s.accessToken(context.Background()); err != nil {
		t.Fatalf("accessToken after backoff: %v", err)
	}
	if calls != 2 {
		t.Fatalf("endpoint hit %d times total, want 2 after backoff elapsed", calls)
	}
}

func TestForceRefreshReportsExpiry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := fixedNow()
	oldExp := now.Add(2 * time.Hour)
	path := writeCreds(t, dir, "old", "refresh-1", oldExp.UnixMilli())
	srv, _, _ := tokenServer(t, http.StatusOK, map[string]any{"access_token": "new", "refresh_token": "r2", "expires_in": 3600})

	s := &credStore{path: path, tokenURL: srv.URL, clientID: "c", httpc: srv.Client(), refresh: true, nowFn: func() time.Time { return now }}
	gotOld, gotNew, err := s.forceRefresh(context.Background())
	if err != nil {
		t.Fatalf("forceRefresh: %v", err)
	}
	if !gotOld.Equal(oldExp) {
		t.Errorf("old expiry = %v, want %v", gotOld, oldExp)
	}
	if want := now.Add(time.Hour); !gotNew.Equal(want) {
		t.Errorf("new expiry = %v, want %v", gotNew, want)
	}
}

func TestFetchRefreshesOn401(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := fixedNow()
	// Token not near expiry, so the proactive path stays out of the way; the 401
	// retry path is what must kick in.
	path := writeCreds(t, dir, "stale", "refresh-1", now.Add(2*time.Hour).UnixMilli())

	tokenSrv, _, _ := tokenServer(t, http.StatusOK, map[string]any{"access_token": "fresh", "refresh_token": "r2", "expires_in": 28800})

	var calls int
	msgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("authorization") == "Bearer stale" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		h := w.Header()
		h.Set("anthropic-ratelimit-unified-5h-utilization", "0.20")
		h.Set("anthropic-ratelimit-unified-7d-utilization", "0.05")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(msgSrv.Close)

	c := New(path, 5*time.Second, WithBaseURL(msgSrv.URL))
	c.store.refresh = true
	c.store.tokenURL = tokenSrv.URL
	c.store.clientID = "c"
	c.store.nowFn = func() time.Time { return now }

	u, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !u.Available() || u.Primary.Utilization != 0.20 {
		t.Errorf("usage = %+v, want valid with 0.20 5h utilization", u)
	}
	if calls < 2 {
		t.Errorf("probe was called %d times, want a retry after refresh", calls)
	}
}
