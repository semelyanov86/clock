package app

import (
	"context"
	"errors"
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

type fakeUsage struct {
	usage model.ProviderUsage
	err   error
}

func (f fakeUsage) Fetch(context.Context) (model.ProviderUsage, error) {
	return f.usage, f.err
}

type fakeRenderer struct{}

func (fakeRenderer) Render(_ model.Snapshot, _ int) ([]byte, error) { return []byte{0xFF, 0xD8}, nil }

type fakeDevice struct {
	creates, patches, selects int
	lastClockID               int
	brightness                []int
	setCalls                  int
	failSetTimes              int // return an error on the first N SetBrightness calls
}

func (f *fakeDevice) Ping(context.Context) error { return nil }
func (f *fakeDevice) SetBrightness(_ context.Context, level int) error {
	f.setCalls++
	if f.setCalls <= f.failSetTimes {
		return errors.New("simulated device error")
	}
	f.brightness = append(f.brightness, level)
	return nil
}
func (f *fakeDevice) GetBrightness(context.Context) (int, error) {
	if len(f.brightness) == 0 {
		return 0, nil
	}
	return f.brightness[len(f.brightness)-1], nil
}
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

func TestDoUsageSourcesRemainIndependent(t *testing.T) {
	t.Parallel()

	claude := model.ProviderUsage{
		Primary: model.UsageWindow{Utilization: 0.31, Valid: true},
	}
	codex := model.ProviderUsage{
		Primary: model.UsageWindow{Utilization: 0.12, Valid: true},
	}
	a := New(config.Config{}, testLogger(), Deps{
		Claude: fakeUsage{usage: claude},
		Codex:  fakeUsage{usage: codex},
	})

	a.doClaude(context.Background())
	a.doCodex(context.Background())

	snap := a.Snapshot(time.Now())
	if snap.Claude.Primary.Utilization != 0.31 || snap.Codex.Primary.Utilization != 0.12 {
		t.Fatalf("usage snapshot = Claude %+v, Codex %+v", snap.Claude, snap.Codex)
	}
	if snap.Claude.Stale || snap.Codex.Stale {
		t.Fatal("successful usage fetch must not be stale")
	}
}

func TestDoCodexFailureMarksPreviousValueStale(t *testing.T) {
	t.Parallel()

	codex := model.ProviderUsage{
		Primary: model.UsageWindow{Utilization: 0.42, Valid: true},
	}
	a := New(config.Config{}, testLogger(), Deps{Codex: fakeUsage{usage: codex}})
	a.doCodex(context.Background())
	a.deps.Codex = fakeUsage{err: errors.New("temporarily unavailable")}
	a.doCodex(context.Background())

	got := a.Snapshot(time.Now()).Codex
	if !got.Available() || !got.Stale {
		t.Fatalf("Codex usage after error = %+v, want valid stale data", got)
	}
	if got.Primary.Utilization != 0.42 {
		t.Errorf("stale utilization = %v, want 0.42", got.Primary.Utilization)
	}

	freshApp := New(config.Config{}, testLogger(), Deps{
		Codex: fakeUsage{err: errors.New("initial failure")},
	})
	freshApp.doCodex(context.Background())
	if initial := freshApp.Snapshot(time.Now()).Codex; initial.Available() || initial.Stale {
		t.Errorf("initial failure produced usage: %+v", initial)
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

func TestNextBrightnessEvent(t *testing.T) {
	t.Parallel()
	loc := time.UTC
	schedule := []config.BrightnessPoint{
		{Hour: 4, Min: 0, Level: 1},
		{Hour: 5, Min: 0, Level: 3},
		{Hour: 6, Min: 0, Level: 4},
		{Hour: 7, Min: 0, Level: 7},
	}
	at := func(h, m int) time.Time { return time.Date(2026, 7, 10, h, m, 0, 0, loc) }

	tests := []struct {
		name      string
		now       time.Time
		wantWait  time.Duration
		wantLevel int
	}{
		{"before first point", at(3, 30), 30 * time.Minute, 1},
		{"between points", at(4, 15), 45 * time.Minute, 3},
		{"exactly on a point rolls to next", at(5, 0), time.Hour, 4},
		{"after last point wraps to tomorrow", at(8, 0), 20 * time.Hour, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotWait, gotLevel := nextBrightnessEvent(tt.now, schedule)
			if gotWait != tt.wantWait || gotLevel != tt.wantLevel {
				t.Errorf("nextBrightnessEvent(%s) = (%s, %d), want (%s, %d)",
					tt.now.Format("15:04"), gotWait, gotLevel, tt.wantWait, tt.wantLevel)
			}
		})
	}
}

func TestSetBrightnessRetriesThenSucceeds(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Device: config.Device{Timeout: time.Second}}
	dev := &fakeDevice{failSetTimes: 2} // fail twice, then succeed
	a := New(cfg, testLogger(), Deps{Device: dev})
	a.retryInitial, a.retryMax, a.retryDeadline = time.Millisecond, time.Millisecond, time.Second

	a.setBrightness(context.Background(), 7)

	if dev.setCalls != 3 {
		t.Errorf("setCalls = %d, want 3 (2 failures + 1 success)", dev.setCalls)
	}
	if len(dev.brightness) != 1 || dev.brightness[0] != 7 {
		t.Errorf("brightness = %v, want [7]", dev.brightness)
	}
}

func TestSetBrightnessGivesUpAtDeadline(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Device: config.Device{Timeout: time.Second}}
	dev := &fakeDevice{failSetTimes: 1_000_000} // always fail
	a := New(cfg, testLogger(), Deps{Device: dev})
	a.retryInitial, a.retryMax, a.retryDeadline = time.Millisecond, time.Millisecond, 20*time.Millisecond

	a.setBrightness(context.Background(), 7)

	if len(dev.brightness) != 0 {
		t.Errorf("brightness = %v, want none applied", dev.brightness)
	}
	if dev.setCalls < 2 {
		t.Errorf("setCalls = %d, want >= 2 (retried before giving up)", dev.setCalls)
	}
}

func TestSetBrightnessCancelStopsRetry(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Device: config.Device{Timeout: time.Second}}
	dev := &fakeDevice{failSetTimes: 1_000_000} // always fail
	a := New(cfg, testLogger(), Deps{Device: dev})
	a.retryInitial, a.retryMax, a.retryDeadline = 10*time.Millisecond, 10*time.Millisecond, time.Hour

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	a.setBrightness(ctx, 7) // must return on ctx cancel, not run to the 1h deadline

	if dev.setCalls == 0 {
		t.Error("expected at least one attempt")
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
