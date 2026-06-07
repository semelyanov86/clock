package divoom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBuildMultipart(t *testing.T) {
	t.Parallel()

	meta := map[string]any{"Command": "Device/CreateLocalClock", "ReturnCode": 0}
	file := []byte{0xFF, 0xD8, 0x01, 0x02, 0x03}
	const boundary = "BND"

	body, err := buildMultipart(meta, file, "clock_bg.jpg", boundary)
	if err != nil {
		t.Fatalf("buildMultipart: %v", err)
	}
	s := string(body)

	metaJSON, _ := json.Marshal(meta)
	checks := []string{
		"--" + boundary + "\r\n",
		`Content-Disposition: form-data; name="json"; filename="cmd.json"` + "\r\n",
		"Content-Type: application/json\r\n",
		"Content-Length: " + strconv.Itoa(len(metaJSON)) + "\r\n\r\n",
		`filename="clock_bg.jpg"`,
		"Content-Type: application/octet-stream\r\n",
		"Content-Length: " + strconv.Itoa(len(file)) + "\r\n\r\n",
	}
	for _, c := range checks {
		if !strings.Contains(s, c) {
			t.Errorf("multipart body missing %q", c)
		}
	}
	if !strings.HasSuffix(s, "\r\n--"+boundary+"--\r\n") {
		t.Errorf("multipart body has wrong ending: %q", s[len(s)-20:])
	}
	// JSON part appears before the file part.
	if strings.Index(s, "cmd.json") > strings.Index(s, "clock_bg.jpg") {
		t.Error("JSON part must precede file part")
	}
}

func TestValidateBackground(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []byte
		ok   bool
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0x00}, true},
		{"webp", append([]byte("RIFF\x00\x00\x00\x00WEBP"), 0x00), true},
		{"png rejected", []byte{0x89, 0x50, 0x4E, 0x47}, false},
		{"empty", nil, false},
		{"too big", make([]byte, maxBackgroundBytes+1), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBackground(tt.in)
			if (err == nil) != tt.ok {
				t.Fatalf("validateBackground(%s) err=%v, want ok=%v", tt.name, err, tt.ok)
			}
		})
	}
}

func TestReplaceDialBg(t *testing.T) {
	t.Parallel()

	var gotCT, gotPath string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"ReturnCode":0}`)
	}))
	defer srv.Close()

	c := New("ignored", 0, 5*time.Second, WithBaseURL(srv.URL))
	jpeg := []byte{0xFF, 0xD8, 0xAA, 0xBB}
	if err := c.ReplaceDialBg(context.Background(), 60001, jpeg); err != nil {
		t.Fatalf("ReplaceDialBg: %v", err)
	}
	if gotPath != endpointReplaceBg {
		t.Errorf("path = %q, want %q", gotPath, endpointReplaceBg)
	}
	if !strings.HasPrefix(gotCT, "multipart/form-data; boundary=") || strings.Contains(gotCT, `"`) {
		t.Errorf("unexpected Content-Type %q (boundary must be present and unquoted)", gotCT)
	}
	if !strings.Contains(string(gotBody), `"ClockId":60001`) {
		t.Errorf("request JSON missing ClockId; body=%q", truncate(gotBody))
	}
}

func TestCommandReturnCodeError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ReturnCode":1,"ReturnMessage":"boom"}`)
	}))
	defer srv.Close()

	c := New("ignored", 0, 5*time.Second, WithBaseURL(srv.URL))
	err := c.SetClockSelect(context.Background(), 42)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want error mentioning device message, got %v", err)
	}
}

func TestCreateLocalClockReturnsID(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ReturnCode":0,"ClockId":60123}`)
	}))
	defer srv.Close()

	c := New("ignored", 0, 5*time.Second, WithBaseURL(srv.URL))
	id, err := c.CreateLocalClock(context.Background(), "Test", []map[string]any{{"disp": 4}}, []string{"time_main"}, []byte{0xFF, 0xD8, 0x01})
	if err != nil {
		t.Fatalf("CreateLocalClock: %v", err)
	}
	if id != 60123 {
		t.Errorf("ClockId = %d, want 60123", id)
	}
}
