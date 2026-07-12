package claudeusage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func staticToken(tok string) Option {
	return WithTokenFunc(func(context.Context) (string, error) { return tok, nil })
}

func TestFetchParsesHeaders(t *testing.T) {
	t.Parallel()

	var gotAuth, gotBeta, gotVersion string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		gotAuth = r.Header.Get("authorization")
		gotBeta = r.Header.Get("anthropic-beta")
		gotVersion = r.Header.Get("anthropic-version")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)

		h := w.Header()
		h.Set("anthropic-ratelimit-unified-5h-utilization", "0.33")
		h.Set("anthropic-ratelimit-unified-5h-reset", "1780850400")
		h.Set("anthropic-ratelimit-unified-7d-utilization", "0.03")
		h.Set("anthropic-ratelimit-unified-7d-reset", "1781438400")
		h.Set("anthropic-ratelimit-unified-status", "allowed")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer srv.Close()

	c := New("", 5*time.Second, WithBaseURL(srv.URL), staticToken("sk-ant-oat0-test"))
	u, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if !u.Available() {
		t.Fatal("usage not valid")
	}
	if u.Primary.Utilization != 0.33 || u.Secondary.Utilization != 0.03 {
		t.Errorf("utilization = %v / %v, want 0.33 / 0.03", u.Primary.Utilization, u.Secondary.Utilization)
	}
	if got := u.Primary.ResetAt.Unix(); got != 1780850400 {
		t.Errorf("5h reset = %d, want 1780850400", got)
	}
	if got := u.Secondary.ResetAt.Unix(); got != 1781438400 {
		t.Errorf("7d reset = %d, want 1781438400", got)
	}
	if u.Updated.IsZero() {
		t.Error("Updated not set")
	}

	if gotAuth != "Bearer sk-ant-oat0-test" {
		t.Errorf("authorization = %q", gotAuth)
	}
	if gotBeta != oauthBeta || gotVersion != anthropicVersion {
		t.Errorf("beta=%q version=%q", gotBeta, gotVersion)
	}
	if s, _ := gotBody["system"].(string); s != systemPrompt {
		t.Errorf("system prompt = %q, want Claude Code prompt", s)
	}
}

func TestFetchMissingHeaders(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"content":[]}`)) // 200 but no rate-limit headers
	}))
	defer srv.Close()

	c := New("", 5*time.Second, WithBaseURL(srv.URL), staticToken("tok"))
	if _, err := c.Fetch(context.Background()); err == nil {
		t.Fatal("expected error when unified headers are absent")
	}
}

func TestFetchTokenError(t *testing.T) {
	t.Parallel()

	c := New("/nonexistent/path/credentials.json", 5*time.Second)
	if _, err := c.Fetch(context.Background()); err == nil {
		t.Fatal("expected error when token cannot be read")
	}
}

func TestAccessTokenFromFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()

	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, []byte(`{"claudeAiOauth":{"accessToken":"sk-ant-oat0-abc","refreshToken":"r"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	tok, err := (&credStore{path: good}).accessToken(ctx)
	if err != nil {
		t.Fatalf("accessToken: %v", err)
	}
	if tok != "sk-ant-oat0-abc" {
		t.Errorf("token = %q", tok)
	}

	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, []byte(`{"claudeAiOauth":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (&credStore{path: empty}).accessToken(ctx); err == nil {
		t.Error("expected error for missing access token")
	}

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (&credStore{path: bad}).accessToken(ctx); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Errorf("expected parse error, got %v", err)
	}
}
