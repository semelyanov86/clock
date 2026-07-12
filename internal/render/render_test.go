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
