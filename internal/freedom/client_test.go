package freedom

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// quoteGateway serves a Tradernet-shaped gateway: authByLogin returns a session,
// getSecurityInfo answers from bodies keyed by ticker. A ticker with an empty
// body is answered with HTTP 500 to stand in for a failing symbol.
func quoteGateway(t *testing.T, bodies map[string]string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		form, err := url.ParseQuery(string(raw))
		if err != nil {
			t.Errorf("parse request: %v", err)
			return
		}

		var req struct {
			Cmd    string `json:"cmd"`
			Params struct {
				Ticker string `json:"ticker"`
			} `json:"params"`
		}
		if err := json.Unmarshal([]byte(form.Get("q")), &req); err != nil {
			t.Errorf("decode q: %v", err)
			return
		}

		if req.Cmd == "authByLogin" {
			_, _ = io.WriteString(w, `{"success":true,"logged":true,"SID":"sid-1","userId":42}`)
			return
		}
		body, ok := bodies[req.Params.Ticker]
		if !ok || body == "" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":"boom"}`)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestClient(t *testing.T, url string) *Client {
	t.Helper()
	c, err := New("user@example.com", "pw", "", url, 5*time.Second,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestQuotesReturnsPartialBatch(t *testing.T) {
	t.Parallel()

	srv := quoteGateway(t, map[string]string{
		"XEON.EU": `{"c":"XEON.EU","short_name":"Xtrackers","ltp":149.92,"pcp":0.05,"x_curr":"EUR"}`,
		"BRNT.EU": "", // this one fails
		"EUR/RUR": `{"c":"EUR/RUR","name":"Euro","ltp":97.69,"pcp":0.59,"ClosePrice":80.1,"x_curr":"RUR"}`,
	})
	c := newTestClient(t, srv.URL)

	got, err := c.Quotes(t.Context(), []string{"XEON.EU", "BRNT.EU", "EUR/RUR"})
	if err != nil {
		t.Fatalf("Quotes: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("resolved %d quotes, want 2: %+v", len(got), got)
	}
	if _, ok := got["BRNT.EU"]; ok {
		t.Error("failing symbol should be absent, not fabricated")
	}
	if pct := got["EUR/RUR"].Delta.Pct; pct != 0.59 {
		t.Errorf("EUR/RUR pct = %v, want 0.59 (feed percent, not stale close)", pct)
	}
}

func TestQuotesAllFailingReportsCause(t *testing.T) {
	t.Parallel()

	srv := quoteGateway(t, map[string]string{})
	c := newTestClient(t, srv.URL)

	_, err := c.Quotes(t.Context(), []string{"XEON.EU", "BRNT.EU"})
	if err == nil {
		t.Fatal("want an error when nothing resolves")
	}
	if !strings.Contains(err.Error(), "http 500") {
		t.Errorf("error should carry the underlying cause, got: %v", err)
	}
}

// TestQuotesOneStallDoesNotStarveTheBatch pins the per-symbol deadline: a ticker
// that never answers must not consume the whole batch budget.
func TestQuotesOneStallDoesNotStarveTheBatch(t *testing.T) {
	t.Parallel()

	stall := make(chan struct{})
	t.Cleanup(func() { close(stall) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		body := string(raw)
		switch {
		case strings.Contains(body, "authByLogin"):
			_, _ = io.WriteString(w, `{"success":true,"logged":true,"SID":"sid-1","userId":42}`)
		case strings.Contains(body, "SLOW"):
			select {
			case <-stall:
			case <-r.Context().Done():
			}
		default:
			_, _ = io.WriteString(w, `{"c":"XEON.EU","short_name":"Xtrackers","ltp":149.92,"pcp":0.05,"x_curr":"EUR"}`)
		}
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv.URL)
	c.httpc.Timeout = time.Second // stand in for perQuoteTimeout, keeping the test quick

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	got, err := c.Quotes(ctx, []string{"SLOW.EU", "XEON.EU"})
	if err != nil {
		t.Fatalf("Quotes: %v", err)
	}
	if _, ok := got["XEON.EU"]; !ok {
		t.Errorf("symbol after the stalled one should still resolve, got %+v", got)
	}
}
