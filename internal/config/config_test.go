package config

import (
	"testing"
)

func TestLoadValidDefaults(t *testing.T) {
	// A minimal valid environment.
	t.Setenv("DIVOOM_DEVICE_HOST", "192.168.178.40")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Device.Port != 9000 {
		t.Errorf("default port = %d, want 9000", c.Device.Port)
	}
	if c.Intervals.Frame <= 0 {
		t.Errorf("frame interval not defaulted: %v", c.Intervals.Frame)
	}
	if c.HasFreedom() || c.HasFavqs() {
		t.Errorf("expected no credentials in a clean env")
	}
}

func TestLoadInvalidValuesAreErrors(t *testing.T) {
	tests := []struct {
		name, key, val string
	}{
		{"bad port", "DIVOOM_DEVICE_PORT", "abc"},
		{"port out of range", "DIVOOM_DEVICE_PORT", "99999"},
		{"bad duration", "FRAME_INTERVAL", "soon"},
		{"non-positive interval", "WEATHER_INTERVAL", "0s"},
		{"bad float", "WEATHER_LAT", "north"},
		{"lat out of range", "WEATHER_LAT", "200"},
		{"lon out of range", "WEATHER_LON", "-999"},
		{"bad bool", "FREEDOM_LOG_BODIES", "maybe"},
		{"bad api url", "FREEDOM_API_URL", "ftp://example.com"},
		{"bad timezone", "CLOCK_TZ", "Mars/Phobos"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DIVOOM_DEVICE_HOST", "192.168.178.40")
			t.Setenv(tt.key, tt.val)
			if _, err := Load(); err == nil {
				t.Fatalf("expected error for %s=%q", tt.key, tt.val)
			}
		})
	}
}

func TestLoadCredentialsDetected(t *testing.T) {
	t.Setenv("FREEDOM_LOGIN", "user@example.com")
	t.Setenv("FREEDOM_PASSWORD", "secret")
	t.Setenv("FAVQS_API_TOKEN", "tok")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.HasFreedom() {
		t.Error("HasFreedom should be true")
	}
	if !c.HasFavqs() {
		t.Error("HasFavqs should be true")
	}
}
