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

func TestSetAmbientLightSendsEveryField(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"ReturnCode":0}`)
	}))
	defer srv.Close()

	c := New("ignored", 0, 5*time.Second, WithBaseURL(srv.URL))
	want := AmbientLight{Brightness: 100, Color: "#FFD166", ColorCycle: 1, EqOnOff: 0, SelectEffect: AmbientEffectWaveUp}
	if err := c.SetAmbientLight(context.Background(), want); err != nil {
		t.Fatalf("SetAmbientLight: %v", err)
	}
	if gotPath != endpointAPI {
		t.Errorf("path = %q, want %q", gotPath, endpointAPI)
	}

	// A partial write zeroes the whole structure on the device, so every field must
	// be on the wire — including the ones that happen to be zero.
	var sent map[string]any
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("decode request %q: %v", gotBody, err)
	}
	for _, key := range []string{"Command", "ReturnCode", "Brightness", "Color", "ColorCycle", "EqOnOff", "SelectEffect"} {
		if _, ok := sent[key]; !ok {
			t.Errorf("request missing %s; body=%s", key, gotBody)
		}
	}
	if sent["Command"] != "Channel/SetAmbientLight" || sent["Color"] != "#FFD166" {
		t.Errorf("request = %s", gotBody)
	}
}

func TestSetAmbientLightRejectsInvalidState(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ReturnCode":0}`)
	}))
	defer srv.Close()
	c := New("ignored", 0, 5*time.Second, WithBaseURL(srv.URL))

	valid := AmbientLight{Brightness: 50, Color: "#0a84ff", SelectEffect: AmbientEffectSolid}
	tests := map[string]AmbientLight{
		// The firmware stores an unknown effect as 0 rather than refusing it, so the
		// client has to guard the range itself.
		"effect above range": {Brightness: 50, Color: "#0a84ff", SelectEffect: AmbientEffectMax + 1},
		"negative effect":    {Brightness: 50, Color: "#0a84ff", SelectEffect: -1},
		"brightness above":   {Brightness: 101, Color: "#0a84ff"},
		"missing colour":     {Brightness: 50},
		"short colour":       {Brightness: 50, Color: "#fff"},
		"non-hex colour":     {Brightness: 50, Color: "#gggggg"},
		"colour without #":   {Brightness: 50, Color: "0a84ff"},
		"cycle out of range": {Brightness: 50, Color: "#0a84ff", ColorCycle: 2},
		"eq out of range":    {Brightness: 50, Color: "#0a84ff", EqOnOff: 5},
	}
	for name, l := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := c.SetAmbientLight(context.Background(), l); err == nil {
				t.Errorf("SetAmbientLight(%+v) accepted an invalid state", l)
			}
		})
	}
	if err := c.SetAmbientLight(context.Background(), valid); err != nil {
		t.Errorf("SetAmbientLight(%+v) = %v, want accepted", valid, err)
	}
}

func TestGetAmbientLight(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ReturnCode":0,"Brightness":100,"ColorCycle":0,"EqOnOff":0,"Color":"#ffffff","SelectEffect":6}`)
	}))
	defer srv.Close()

	c := New("ignored", 0, 5*time.Second, WithBaseURL(srv.URL))
	got, err := c.GetAmbientLight(context.Background())
	if err != nil {
		t.Fatalf("GetAmbientLight: %v", err)
	}
	want := AmbientLight{Brightness: 100, Color: "#ffffff", ColorCycle: 0, EqOnOff: 0, SelectEffect: AmbientEffectFadeInOut}
	if got != want {
		t.Errorf("state = %+v, want %+v", got, want)
	}
	if !got.On() {
		t.Error("On() = false for a strip at brightness 100")
	}
	// The device lowercases the colour it echoes back; a set/read-back comparison
	// must not trip over that.
	if !got.SameAs(AmbientLight{Brightness: 100, Color: "#FFFFFF", SelectEffect: AmbientEffectFadeInOut}) {
		t.Error("SameAs must ignore colour case")
	}
}

func TestGetAmbientLightDeviceError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ReturnCode":1,"ReturnMessage":"Only accept JSON parameters"}`)
	}))
	defer srv.Close()

	c := New("ignored", 0, 5*time.Second, WithBaseURL(srv.URL))
	if _, err := c.GetAmbientLight(context.Background()); err == nil {
		t.Fatal("want an error when the device rejects the command")
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
