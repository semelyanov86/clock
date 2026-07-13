package render

import (
	"bytes"
	"image/jpeg"
	"testing"
	"time"

	"github.com/semelyanov86/clock/internal/model"
)

func TestRenderProducesValidJPEG(t *testing.T) {
	t.Parallel()

	r, err := New("Europe/Berlin")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	now := time.Date(2026, 6, 7, 14, 32, 0, 0, time.UTC)
	claudeOnly := model.SampleSnapshot(now)
	claudeOnly.Codex = model.ProviderUsage{}
	codexOnly := model.SampleSnapshot(now)
	codexOnly.Claude = model.ProviderUsage{}
	codexOnly.Codex.Primary = model.UsageWindow{}
	snaps := map[string]model.Snapshot{
		"both providers": model.SampleSnapshot(now),
		"Claude only":    claudeOnly,
		"Codex only":     codexOnly,
		"empty":          {Generated: now}, // no data must not panic
	}
	for name, snap := range snaps {
		pageCount := numPages
		if snap.Claude.Available() || snap.Codex.Available() {
			pageCount++
		}
		for frame := 0; frame < pageCount; frame++ {
			data, err := r.Render(snap, frame)
			if err != nil {
				t.Fatalf("%s frame %d: %v", name, frame, err)
			}
			if len(data) == 0 || len(data) > maxJPEGBytes {
				t.Errorf("%s frame %d: size %d out of bounds (max %d)", name, frame, len(data), maxJPEGBytes)
			}
			if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
				t.Errorf("%s frame %d: not a JPEG", name, frame)
			}
			cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("%s frame %d: decode config: %v", name, frame, err)
			}
			if cfg.Width != CanvasW || cfg.Height != CanvasH {
				t.Errorf("%s frame %d: dims %dx%d, want %dx%d", name, frame, cfg.Width, cfg.Height, CanvasW, CanvasH)
			}
		}
	}
}

func TestCycleIndex(t *testing.T) {
	t.Parallel()
	tests := []struct {
		frame, n, want int
	}{
		{0, 3, 0}, {1, 3, 1}, {3, 3, 0}, {4, 3, 1},
		{-1, 3, 2}, {0, 0, 0}, {5, 1, 0},
	}
	for _, tt := range tests {
		if got := cycleIndex(tt.frame, tt.n); got != tt.want {
			t.Errorf("cycleIndex(%d,%d) = %d, want %d", tt.frame, tt.n, got, tt.want)
		}
	}
}

func TestRoundTemp(t *testing.T) {
	t.Parallel()
	tests := map[float64]string{
		18.1: "18°", 18.6: "19°", -2.4: "-2°", -2.6: "-3°", 0: "0°",
	}
	for in, want := range tests {
		if got := roundTemp(in); got != want {
			t.Errorf("roundTemp(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatPressure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   float64
		want string
	}{
		{name: "standard atmosphere", in: 1013.25, want: "760 мм рт. ст."},
		{name: "round conversion", in: 1000, want: "750 мм рт. ст."},
		{name: "missing", in: 0, want: "—"},
		{name: "invalid", in: -1, want: "—"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatPressure(tt.in); got != tt.want {
				t.Errorf("formatPressure(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatWind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   float64
		want string
	}{
		{name: "round up", in: 13.9, want: "14 км/ч"},
		{name: "round down", in: 4.3, want: "4 км/ч"},
		{name: "calm", in: 0, want: "0 км/ч"},
		{name: "invalid", in: -1, want: "—"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatWind(tt.in); got != tt.want {
				t.Errorf("formatWind(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestWeatherCardValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		weather      model.WeatherNow
		wantPressure string
		wantWind     string
		wantValid    bool
	}{
		{
			name:         "available",
			weather:      model.WeatherNow{PressureHPa: 1013.25, WindKmh: 13.9},
			wantPressure: "760 мм рт. ст.",
			wantWind:     "14 км/ч",
			wantValid:    true,
		},
		{
			name:         "calm is valid",
			weather:      model.WeatherNow{PressureHPa: 1013.25},
			wantPressure: "760 мм рт. ст.",
			wantWind:     "0 км/ч",
			wantValid:    true,
		},
		{
			name:         "missing weather",
			weather:      model.WeatherNow{},
			wantPressure: "—",
			wantWind:     "—",
			wantValid:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pressure, wind, valid := weatherCardValues(tt.weather)
			if pressure != tt.wantPressure || wind != tt.wantWind || valid != tt.wantValid {
				t.Errorf(
					"weatherCardValues() = %q, %q, %v; want %q, %q, %v",
					pressure,
					wind,
					valid,
					tt.wantPressure,
					tt.wantWind,
					tt.wantValid,
				)
			}
		})
	}
}
