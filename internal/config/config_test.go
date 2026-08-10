package config

import (
	"testing"
)

func TestLoadValidDefaults(t *testing.T) {
	// A minimal valid environment. The credential variables are cleared
	// explicitly: `task` loads .env into the test process, so a developer with real
	// secrets on disk would otherwise see this fail.
	t.Setenv("DIVOOM_DEVICE_HOST", "192.168.178.40")
	for _, key := range []string{"FREEDOM_LOGIN", "FREEDOM_PASSWORD", "FAVQS_API_TOKEN"} {
		t.Setenv(key, "")
	}

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
	if c.HasCodex() {
		t.Error("Codex usage should be opt-in")
	}
	if c.Intervals.Codex <= 0 {
		t.Errorf("Codex interval not defaulted: %v", c.Intervals.Codex)
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
		{"bad Codex enabled", "CODEX_USAGE_ENABLED", "maybe"},
		{"non-positive Codex interval", "CODEX_USAGE_INTERVAL", "0s"},
		{"bad ambient entry", "AMBIENT_SCHEDULE", "07:00"},
		{"bad ambient state", "AMBIENT_SCHEDULE", "07:00=maybe"},
		{"bad ambient minute", "AMBIENT_SCHEDULE", "07:99=on"},
		{"bad ambient brightness", "AMBIENT_BRIGHTNESS", "150"},
		{"zero ambient brightness", "AMBIENT_BRIGHTNESS", "0"},
		{"bad ambient effect", "AMBIENT_EFFECTS", "1,9"},
		{"non-numeric ambient effect", "AMBIENT_EFFECTS", "solid"},
		{"bad ambient colour", "AMBIENT_COLORS", "#FFFFFF,red"},
		{"bad ambient cycle chance", "AMBIENT_COLOR_CYCLE_CHANCE", "120"},
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

func TestLoadCodexUsageEnabled(t *testing.T) {
	t.Setenv("CODEX_USAGE_ENABLED", "true")
	t.Setenv("CODEX_BIN", "/home/sergey/.local/bin/codex")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.HasCodex() {
		t.Error("HasCodex should be true")
	}
	if c.Codex.Bin != "/home/sergey/.local/bin/codex" {
		t.Errorf("Codex.Bin = %q", c.Codex.Bin)
	}
}

func TestLoadDefaultBrightnessSchedule(t *testing.T) {
	t.Setenv("DIVOOM_DEVICE_HOST", "192.168.178.40")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []BrightnessPoint{{4, 0, 1}, {5, 0, 9}, {6, 0, 25}, {7, 0, 40}, {8, 0, 50}}
	if len(c.BrightnessSchedule) != len(want) {
		t.Fatalf("BrightnessSchedule = %+v, want %+v", c.BrightnessSchedule, want)
	}
	for i, p := range want {
		if c.BrightnessSchedule[i] != p {
			t.Errorf("point %d = %+v, want %+v", i, c.BrightnessSchedule[i], p)
		}
	}
}

func TestLoadDefaultAmbientSchedule(t *testing.T) {
	t.Setenv("DIVOOM_DEVICE_HOST", "192.168.178.40")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []AmbientPoint{{7, 0, true}, {22, 30, false}}
	if len(c.Ambient.Schedule) != len(want) {
		t.Fatalf("Ambient.Schedule = %+v, want %+v", c.Ambient.Schedule, want)
	}
	for i, p := range want {
		if c.Ambient.Schedule[i] != p {
			t.Errorf("point %d = %+v, want %+v", i, c.Ambient.Schedule[i], p)
		}
	}
	if c.Ambient.Brightness != 100 || c.Ambient.CycleChance != 30 {
		t.Errorf("brightness = %d, cycleChance = %d, want 100 / 30", c.Ambient.Brightness, c.Ambient.CycleChance)
	}
	// Effect 0 lights only the bottom of the strip and 7 duplicates 1: neither
	// belongs in the pool a random day is drawn from.
	for _, e := range c.Ambient.Effects {
		if e == 0 || e > 6 {
			t.Errorf("default effect pool contains %d: %v", e, c.Ambient.Effects)
		}
	}
	if len(c.Ambient.Effects) < 2 || len(c.Ambient.Colors) < 2 {
		t.Errorf("default pools too small: effects=%v colors=%v", c.Ambient.Effects, c.Ambient.Colors)
	}
}

func TestParseAmbientSchedule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want []AmbientPoint
	}{
		{"empty disables", "", nil},
		{"on and off", "07:00=on,22:30=off", []AmbientPoint{{7, 0, true}, {22, 30, false}}},
		{"spaces tolerated", " 07:00 = on , 22:30 = off ", []AmbientPoint{{7, 0, true}, {22, 30, false}}},
		{"booleans accepted", "06:15=true,23:45=0", []AmbientPoint{{6, 15, true}, {23, 45, false}}},
		{"case insensitive", "07:00=ON", []AmbientPoint{{7, 0, true}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseAmbientSchedule(tt.raw)
			if err != nil {
				t.Fatalf("parseAmbientSchedule(%q): %v", tt.raw, err)
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
