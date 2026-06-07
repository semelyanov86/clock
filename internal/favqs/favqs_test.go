package favqs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchWithToken(t *testing.T) {
	t.Parallel()

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/quotes") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"quotes":[{"body":"A","author":"X"},{"body":"B","author":"Y"}]}`))
	}))
	defer srv.Close()

	c := New("tok123", 5*time.Second, WithBaseURL(srv.URL))
	qs, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(qs) != 2 || qs[0].Text != "A" || qs[0].Author != "X" {
		t.Fatalf("quotes = %+v", qs)
	}
	if gotAuth != `Token token="tok123"` {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestFetchQotdFallback(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/qotd" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"quote":{"body":"Q","author":"Z"}}`))
	}))
	defer srv.Close()

	c := New("", 5*time.Second, WithBaseURL(srv.URL))
	qs, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(qs) != 1 || qs[0].Text != "Q" || qs[0].Author != "Z" {
		t.Fatalf("quotes = %+v", qs)
	}
}
