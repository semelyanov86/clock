package app

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/semelyanov86/clock/internal/config"
	"github.com/semelyanov86/clock/internal/model"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeQuotes struct{ m map[string]model.Instrument }

func (f fakeQuotes) Quotes(_ context.Context, _ []string) (map[string]model.Instrument, error) {
	return f.m, nil
}

type fakeRenderer struct{}

func (fakeRenderer) Render(_ model.Snapshot, _ int) ([]byte, error) { return []byte{0xFF, 0xD8}, nil }

type fakeDevice struct {
	creates, patches, selects int
	lastClockID               int
}

func (f *fakeDevice) Ping(context.Context) error { return nil }
func (f *fakeDevice) CreateLocalClock(context.Context, string, []map[string]any, []string, []byte) (int, error) {
	f.creates++
	return 555, nil
}
func (f *fakeDevice) PatchDialBg(_ context.Context, clockID int, _ []byte) error {
	f.patches++
	f.lastClockID = clockID
	return nil
}
func (f *fakeDevice) SetClockSelect(_ context.Context, clockID int) error {
	f.selects++
	f.lastClockID = clockID
	return nil
}

func TestStoreSnapshotIsolation(t *testing.T) {
	t.Parallel()
	s := newStore()
	now := time.Now()

	s.setMarkets([]model.Instrument{{Symbol: "A"}}, model.Instrument{}, nil)
	snap1 := s.snapshot(now)

	s.setMarkets([]model.Instrument{{Symbol: "B"}}, model.Instrument{}, nil)
	snap2 := s.snapshot(now)

	if snap1.ETFs[0].Symbol != "A" {
		t.Errorf("snap1 mutated: got %q", snap1.ETFs[0].Symbol)
	}
	if snap2.ETFs[0].Symbol != "B" {
		t.Errorf("snap2 = %q, want B", snap2.ETFs[0].Symbol)
	}
}

func TestDoMarketsMapping(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		Device: config.Device{Timeout: time.Second},
		Freedom: config.Freedom{
			ETFSymbols:  []string{"A", "B"},
			BrentSymbol: "BR",
			FXSymbols:   []string{"EUR/RUB", "USD/RUB"},
		},
	}
	deps := Deps{Quotes: fakeQuotes{m: map[string]model.Instrument{
		"A":       {Symbol: "A", Last: 1},
		"B":       {Symbol: "B", Last: 2},
		"BR":      {Symbol: "BR", Last: 80},
		"EUR/RUB": {Symbol: "EUR/RUB", Last: 98},
		"USD/RUB": {Symbol: "USD/RUB", Last: 91},
	}}}
	a := New(cfg, testLogger(), deps)
	a.doMarkets(context.Background())

	snap := a.Snapshot(time.Now())
	if len(snap.ETFs) != 2 || snap.ETFs[0].Symbol != "A" || snap.ETFs[1].Symbol != "B" {
		t.Fatalf("ETFs = %+v", snap.ETFs)
	}
	if snap.Brent.Name != "Brent Crude" || snap.Brent.Last != 80 {
		t.Errorf("Brent = %+v", snap.Brent)
	}
	if len(snap.FX) != 2 || snap.FX[0].Symbol != "EUR/RUB" || snap.FX[0].Name != "Евро" || snap.FX[0].Currency != "₽" {
		t.Fatalf("FX = %+v", snap.FX)
	}
}

func TestPushFrameCreatesThenPatches(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Device: config.Device{Timeout: time.Second, ClockFont: 24}}
	dev := &fakeDevice{}
	a := New(cfg, testLogger(), Deps{Renderer: fakeRenderer{}, Device: dev})

	// First push with no pinned clock: create + select.
	a.pushFrame(context.Background(), 0)
	if dev.creates != 1 || a.clockID != 555 || dev.selects != 1 {
		t.Fatalf("after first push: creates=%d clockID=%d selects=%d", dev.creates, a.clockID, dev.selects)
	}
	// Subsequent push: patch the backdrop, then re-select to force a redraw.
	a.pushFrame(context.Background(), 1)
	if dev.patches != 1 || dev.lastClockID != 555 || dev.selects != 2 {
		t.Fatalf("after second push: patches=%d lastClockID=%d selects=%d", dev.patches, dev.lastClockID, dev.selects)
	}
}

func TestFxName(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"EUR/RUB": "Евро", "USD/RUB": "Доллар", "CNY/RUB": "Юань",
		"GBP/RUB": "Фунт", "JPY_RUB": "JPY",
	}
	for in, want := range tests {
		if got := fxName(in); got != want {
			t.Errorf("fxName(%q) = %q, want %q", in, got, want)
		}
	}
}
