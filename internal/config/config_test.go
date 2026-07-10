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
		{"bad brightness entry", "BRIGHTNESS_SCHEDULE", "04:00"},
		{"bad brightness level", "BRIGHTNESS_SCHEDULE", "04:00=200"},
		{"bad brightness hour", "BRIGHTNESS_SCHEDULE", "25:00=1"},
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

func TestLoadDefaultBrightnessSchedule(t *testing.T) {
	t.Setenv("DIVOOM_DEVICE_HOST", "192.168.178.40")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []BrightnessPoint{{4, 0, 1}, {5, 0, 3}, {6, 0, 4}, {7, 0, 7}, {8, 0, 15}}
	if len(c.BrightnessSchedule) != len(want) {
		t.Fatalf("BrightnessSchedule = %+v, want %+v", c.BrightnessSchedule, want)
	}
	for i, p := range want {
		if c.BrightnessSchedule[i] != p {
			t.Errorf("point %d = %+v, want %+v", i, c.BrightnessSchedule[i], p)
		}
	}
}

func TestParseBrightnessSchedule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want []BrightnessPoint
	}{
		{"empty disables", "", nil},
		{"single", "07:30=7", []BrightnessPoint{{7, 30, 7}}},
		{"spaces tolerated", " 04:00 = 1 , 05:00=3 ", []BrightnessPoint{{4, 0, 1}, {5, 0, 3}}},
		{"zero level allowed", "22:00=0", []BrightnessPoint{{22, 0, 0}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseBrightnessSchedule(tt.raw)
			if err != nil {
				t.Fatalf("parseBrightnessSchedule(%q): %v", tt.raw, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("point %d = %+v, want %+v", i, got[i], tt.want[i])
				}
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
