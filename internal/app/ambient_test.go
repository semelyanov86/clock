package app

import (
	"context"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/semelyanov86/clock/internal/config"
	"github.com/semelyanov86/clock/internal/divoom"
)

// testAmbient is the shipped default schedule and pools.
func testAmbient() config.Ambient {
	return config.Ambient{
		Schedule: []config.AmbientPoint{
			{Hour: 7, Min: 0, On: true},
			{Hour: 22, Min: 30, On: false},
		},
		Brightness:  100,
		Effects:     []int{1, 2, 3, 4, 5, 6},
		Colors:      []string{"#FFFFFF", "#FFD166", "#4C6EF5"},
		CycleChance: 30,
	}
}

func testApp(t *testing.T, dev Device, amb config.Ambient) *App {
	t.Helper()
	cfg := config.Config{
		Device:  config.Device{Timeout: time.Second},
		Ambient: amb,
		ClockTZ: "Europe/Berlin",
	}
	a := New(cfg, testLogger(), Deps{Device: dev})
	a.retryInitial, a.retryMax, a.retryDeadline = time.Millisecond, time.Millisecond, 100*time.Millisecond
	a.rnd = rand.New(rand.NewPCG(1, 2)) // deterministic looks
	return a
}

func TestNextAmbientEvent(t *testing.T) {
	t.Parallel()
	schedule := testAmbient().Schedule
	at := func(h, m int) time.Time { return time.Date(2026, 8, 10, h, m, 0, 0, time.UTC) }

	tests := []struct {
		name     string
		now      time.Time
		wantWait time.Duration
		wantOn   bool
	}{
		{"morning ahead", at(3, 0), 4 * time.Hour, true},
		{"during the day the next switch is off", at(12, 0), 10*time.Hour + 30*time.Minute, false},
		{"exactly on a point rolls to the next", at(7, 0), 15*time.Hour + 30*time.Minute, false},
		{"after the last point wraps to tomorrow", at(23, 0), 8 * time.Hour, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotWait, gotPoint := nextAmbientEvent(tt.now, schedule)
			if gotWait != tt.wantWait || gotPoint.On != tt.wantOn {
				t.Errorf("nextAmbientEvent(%s) = (%s, on=%v), want (%s, on=%v)",
					tt.now.Format("15:04"), gotWait, gotPoint.On, tt.wantWait, tt.wantOn)
			}
		})
	}
}

func TestAmbientStateAt(t *testing.T) {
	t.Parallel()
	schedule := testAmbient().Schedule
	at := func(h, m int) time.Time { return time.Date(2026, 8, 10, h, m, 0, 0, time.UTC) }

	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"before the morning switch belongs to last night", at(3, 0), false},
		{"exactly at the morning switch", at(7, 0), true},
		{"midday is lit", at(15, 0), true},
		{"just before the night switch is still lit", at(22, 29), true},
		{"at the night switch", at(22, 30), false},
		{"late evening is dark", at(23, 59), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ambientStateAt(tt.now, schedule); got != tt.want {
				t.Errorf("ambientStateAt(%s) = %v, want %v", tt.now.Format("15:04"), got, tt.want)
			}
		})
	}
}

func TestAmbientLookStaysInsideTheConfiguredPools(t *testing.T) {
	t.Parallel()
	cfg := testAmbient()
	rnd := rand.New(rand.NewPCG(7, 9))

	effects := map[int]int{}
	colors := map[string]int{}
	cycles := 0
	for range 300 {
		look := AmbientLook(cfg, rnd, -1)

		if look.Brightness != cfg.Brightness {
			t.Fatalf("brightness = %d, want %d", look.Brightness, cfg.Brightness)
		}
		if look.EqOnOff != 0 {
			t.Fatal("sound reactivity must stay off")
		}
		// An effect the firmware does not know is stored as 0 (bottom-only) instead
		// of being rejected, so the randomiser must never produce one.
		if look.SelectEffect < 0 || look.SelectEffect > divoom.AmbientEffectMax {
			t.Fatalf("effect %d outside the range the firmware accepts", look.SelectEffect)
		}
		effects[look.SelectEffect]++
		colors[look.Color]++
		cycles += look.ColorCycle
	}

	// Every configured option should show up over 300 draws, and none other.
	if len(effects) != len(cfg.Effects) {
		t.Errorf("drew %d distinct effects %v, want all %v", len(effects), effects, cfg.Effects)
	}
	if len(colors) != len(cfg.Colors) {
		t.Errorf("drew %d distinct colours %v, want all %v", len(colors), colors, cfg.Colors)
	}
	// Colour cycling is a minority of days but must happen (CycleChance = 30%).
	if cycles == 0 || cycles == 300 {
		t.Errorf("colour cycle drawn %d/300 times, want a mix", cycles)
	}
}

func TestAmbientLookAvoidsRepeatingTheEffect(t *testing.T) {
	t.Parallel()
	cfg := testAmbient()
	rnd := rand.New(rand.NewPCG(3, 4))

	repeats := 0
	last := -1
	for range 200 {
		look := AmbientLook(cfg, rnd, last)
		if look.SelectEffect == last {
			repeats++
		}
		last = look.SelectEffect
	}
	// A repeat needs both draws to land on the same effect: ~1/36 with six
	// effects, so a fifth of the days repeating would mean the guard is not wired.
	if repeats > 40 {
		t.Errorf("%d/200 consecutive days reused the effect, want far fewer", repeats)
	}
}

func TestAmbientLookSingleEffectPoolDoesNotSpin(t *testing.T) {
	t.Parallel()
	cfg := testAmbient()
	cfg.Effects = []int{6}

	look := AmbientLook(cfg, rand.New(rand.NewPCG(1, 1)), 6)
	if look.SelectEffect != 6 {
		t.Errorf("effect = %d, want 6 (the only option, repeat or not)", look.SelectEffect)
	}
}

func TestApplyAmbientOffWritesEveryField(t *testing.T) {
	t.Parallel()
	dev := &fakeDevice{ambientState: divoom.AmbientLight{Brightness: 100, Color: "#ffffff", SelectEffect: 6}}
	a := testApp(t, dev, testAmbient())

	a.applyAmbient(context.Background(), false)

	if len(dev.ambient) != 1 {
		t.Fatalf("ambient writes = %d, want 1", len(dev.ambient))
	}
	got := dev.ambient[0]
	// A partial write zeroes the whole structure on the device, so "off" must be a
	// complete, explicit state.
	if got.On() || got.Color != "#000000" || got.ColorCycle != 0 || got.EqOnOff != 0 || got.SelectEffect != 0 {
		t.Errorf("off state = %+v, want a fully blanked strip", got)
	}
}

func TestApplyAmbientOnRetriesThenSucceeds(t *testing.T) {
	t.Parallel()
	dev := &fakeDevice{failAmbientSet: 2}
	a := testApp(t, dev, testAmbient())

	a.applyAmbient(context.Background(), true)

	if dev.ambientSets != 3 {
		t.Errorf("SetAmbientLight calls = %d, want 3 (2 failures + 1 success)", dev.ambientSets)
	}
	if len(dev.ambient) != 1 || !dev.ambient[0].On() {
		t.Fatalf("applied states = %+v, want one lit strip", dev.ambient)
	}
	if a.lastAmbientEffect != dev.ambient[0].SelectEffect {
		t.Errorf("lastAmbientEffect = %d, want %d", a.lastAmbientEffect, dev.ambient[0].SelectEffect)
	}
}

func TestApplyAmbientGivesUpAndKeepsLastEffect(t *testing.T) {
	t.Parallel()
	dev := &fakeDevice{failAmbientSet: 1_000_000}
	a := testApp(t, dev, testAmbient())

	a.applyAmbient(context.Background(), true)

	if len(dev.ambient) != 0 {
		t.Errorf("applied states = %+v, want none", dev.ambient)
	}
	if dev.ambientSets < 2 {
		t.Errorf("SetAmbientLight calls = %d, want >= 2 (retried before giving up)", dev.ambientSets)
	}
	if a.lastAmbientEffect != -1 {
		t.Errorf("lastAmbientEffect = %d, want -1 (nothing reached the device)", a.lastAmbientEffect)
	}
}

func TestSyncAmbientToSchedule(t *testing.T) {
	t.Parallel()
	loc := time.UTC
	lit := divoom.AmbientLight{Brightness: 100, Color: "#ffffff", SelectEffect: 6}
	dark := AmbientOff()

	tests := []struct {
		name       string
		state      divoom.AmbientLight
		schedule   []config.AmbientPoint
		wantWrites int
		wantOn     bool
	}{
		{
			name:       "dark strip during the lit window is corrected",
			state:      dark,
			schedule:   []config.AmbientPoint{{Hour: 0, Min: 0, On: true}},
			wantWrites: 1,
			wantOn:     true,
		},
		{
			name:       "lit strip during the dark window is blanked",
			state:      lit,
			schedule:   []config.AmbientPoint{{Hour: 0, Min: 0, On: false}},
			wantWrites: 1,
			wantOn:     false,
		},
		{
			name:       "already lit as scheduled is left untouched",
			state:      lit,
			schedule:   []config.AmbientPoint{{Hour: 0, Min: 0, On: true}},
			wantWrites: 0,
		},
		{
			name:       "already dark as scheduled is left untouched",
			state:      dark,
			schedule:   []config.AmbientPoint{{Hour: 0, Min: 0, On: false}},
			wantWrites: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			amb := testAmbient()
			amb.Schedule = tt.schedule
			dev := &fakeDevice{ambientState: tt.state}
			a := testApp(t, dev, amb)

			a.syncAmbientToSchedule(context.Background(), loc)

			if len(dev.ambient) != tt.wantWrites {
				t.Fatalf("writes = %d (%+v), want %d", len(dev.ambient), dev.ambient, tt.wantWrites)
			}
			if tt.wantWrites > 0 && dev.ambient[0].On() != tt.wantOn {
				t.Errorf("written state on = %v, want %v", dev.ambient[0].On(), tt.wantOn)
			}
		})
	}
}

func TestSyncAmbientLeavesStripAloneWhenUnreadable(t *testing.T) {
	t.Parallel()
	dev := &fakeDevice{failAmbientGet: true}
	a := testApp(t, dev, testAmbient())

	a.syncAmbientToSchedule(context.Background(), time.UTC)

	// Guessing here could light the strip in the middle of the night; the next
	// scheduled point sets it anyway.
	if dev.ambientSets != 0 {
		t.Errorf("SetAmbientLight calls = %d, want 0 when the state cannot be read", dev.ambientSets)
	}
}

func TestRunAmbientScheduleNoopWithoutSchedule(t *testing.T) {
	t.Parallel()
	amb := testAmbient()
	amb.Schedule = nil
	dev := &fakeDevice{}
	a := testApp(t, dev, amb)

	a.runAmbientSchedule(context.Background()) // must return immediately

	if dev.ambientSets != 0 || dev.ambientGets != 0 {
		t.Errorf("device touched with an empty schedule: sets=%d gets=%d", dev.ambientSets, dev.ambientGets)
	}
}

func TestRunAmbientScheduleStopsOnContextCancel(t *testing.T) {
	t.Parallel()
	// A far-away schedule point: the loop must exit on cancel, not on the timer.
	amb := testAmbient()
	amb.Schedule = []config.AmbientPoint{{Hour: 12, Min: 0, On: true}}
	dev := &fakeDevice{ambientState: divoom.AmbientLight{Brightness: 100, Color: "#ffffff", SelectEffect: 6}}
	a := testApp(t, dev, amb)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.runAmbientSchedule(ctx)
		close(done)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runAmbientSchedule did not return after context cancel")
	}
}
