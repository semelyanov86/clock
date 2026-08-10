package app

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/semelyanov86/clock/internal/config"
	"github.com/semelyanov86/clock/internal/divoom"
)

// runAmbientSchedule switches the side RGB strip on and off at the configured
// times of day (in CLOCK_TZ, the zone the native clock uses). Each switch-on
// draws a fresh random look — effect, colour, and now and then a full-spectrum
// cycle — so the strip never glows the same two days running. It loops until ctx
// is cancelled. No-op when the schedule is empty.
func (a *App) runAmbientSchedule(ctx context.Context) {
	if len(a.cfg.Ambient.Schedule) == 0 {
		return
	}
	loc, err := time.LoadLocation(a.cfg.ClockTZ)
	if err != nil { // CLOCK_TZ is validated at load, so this is defensive.
		a.log.Error("ambient light schedule disabled: load timezone", "tz", a.cfg.ClockTZ, "err", err)
		return
	}
	a.log.Info("ambient light schedule active", "tz", a.cfg.ClockTZ,
		"points", len(a.cfg.Ambient.Schedule), "effects", a.cfg.Ambient.Effects,
		"colors", len(a.cfg.Ambient.Colors))

	a.syncAmbientToSchedule(ctx, loc)

	for {
		now := time.Now().In(loc)
		wait, point := nextAmbientEvent(now, a.cfg.Ambient.Schedule)
		a.log.Info("next ambient light change",
			"at", now.Add(wait).Format("2006-01-02T15:04 MST"),
			"on", point.On, "in", wait.Round(time.Second).String())

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			a.applyAmbient(ctx, point.On)
		}
	}
}

// syncAmbientToSchedule aligns the strip with what the schedule says it should
// be doing right now. The device keeps its strip state across reboots, so a
// service that was down over a switch-over point (an overnight restart, a fresh
// deploy) would otherwise leave the strip dark all day or lit all night. It only
// writes when the device disagrees, so a routine restart does not re-roll the
// day's look.
func (a *App) syncAmbientToSchedule(ctx context.Context, loc *time.Location) {
	wantOn := ambientStateAt(time.Now().In(loc), a.cfg.Ambient.Schedule)

	rctx, cancel := context.WithTimeout(ctx, a.cfg.Device.Timeout)
	got, err := a.deps.Device.GetAmbientLight(rctx)
	cancel()
	if err != nil {
		// Leave the strip alone: the next scheduled point will set it anyway, and
		// guessing here could light it up in the middle of the night.
		a.log.Warn("ambient light: read state at startup", "err", err)
		return
	}
	if got.On() == wantOn {
		a.log.Info("ambient light already matches schedule", "on", wantOn,
			"effect", got.SelectEffect, "color", got.Color)
		return
	}
	a.log.Info("ambient light out of sync with schedule; correcting", "deviceOn", got.On(), "wantOn", wantOn)
	a.applyAmbient(ctx, wantOn)
}

// applyAmbient lights the strip with a freshly drawn look or blanks it, retrying
// transient device/network failures, then reads the state back to confirm.
func (a *App) applyAmbient(ctx context.Context, on bool) {
	want := AmbientOff()
	if on {
		want = a.nextAmbientLook()
	}

	ok := a.applyWithRetry(ctx, "ambient light", func(c context.Context) error {
		return a.deps.Device.SetAmbientLight(c, want)
	}, "on", on, "effect", want.SelectEffect, "color", want.Color, "cycle", want.ColorCycle)
	if !ok {
		return
	}
	if on {
		a.lastAmbientEffect = want.SelectEffect
	}
	a.verifyAmbient(ctx, want)
}

// verifyAmbient reads the strip state back and warns when it does not match what
// was just written — the firmware accepts an out-of-range effect and silently
// stores 0 instead, which a successful set alone cannot catch. A failed read-back
// is only a warning: the write itself succeeded.
func (a *App) verifyAmbient(ctx context.Context, want divoom.AmbientLight) {
	vctx, cancel := context.WithTimeout(ctx, a.cfg.Device.Timeout)
	defer cancel()

	got, err := a.deps.Device.GetAmbientLight(vctx)
	if err != nil {
		a.log.Warn("verify ambient light: read-back failed", "err", err)
		return
	}
	if !got.SameAs(want) {
		a.log.Warn("verify ambient light: mismatch after set", "want", want, "got", got)
		return
	}
	a.log.Info("verify ambient light: confirmed", "on", got.On(),
		"brightness", got.Brightness, "effect", got.SelectEffect, "color", got.Color, "cycle", got.ColorCycle)
}

// nextAmbientLook draws the look for one switch-on: a random effect and colour
// from the configured pools, and, with CycleChance percent probability, a drift
// through the whole spectrum instead of a single colour. It avoids repeating the
// previous effect so consecutive days differ visibly.
func (a *App) nextAmbientLook() divoom.AmbientLight {
	return AmbientLook(a.cfg.Ambient, a.rnd, a.lastAmbientEffect)
}

// AmbientLook builds one random look for the side strip. avoidEffect is the
// previously used effect (-1 for none), which is skipped when the pool has an
// alternative.
func AmbientLook(cfg config.Ambient, rnd *rand.Rand, avoidEffect int) divoom.AmbientLight {
	effect := cfg.Effects[rnd.IntN(len(cfg.Effects))]
	if effect == avoidEffect && len(cfg.Effects) > 1 {
		effect = cfg.Effects[rnd.IntN(len(cfg.Effects))]
	}
	cycle := 0
	if rnd.IntN(100) < cfg.CycleChance {
		cycle = 1
	}
	return divoom.AmbientLight{
		Brightness:   cfg.Brightness,
		Color:        cfg.Colors[rnd.IntN(len(cfg.Colors))],
		ColorCycle:   cycle,
		EqOnOff:      0, // never sound-reactive: the strip would twitch at every noise
		SelectEffect: effect,
	}
}

// AmbientOff is the blanked strip: dark, static, no animation. Every field is
// written because a partial Channel/SetAmbientLight zeroes the whole structure
// on the device.
func AmbientOff() divoom.AmbientLight {
	return divoom.AmbientLight{
		Brightness:   0,
		Color:        "#000000",
		ColorCycle:   0,
		EqOnOff:      0,
		SelectEffect: divoom.AmbientEffectBottomOnly,
	}
}

// nextAmbientEvent returns how long to wait until the next scheduled switch and
// the point to apply then. now must already be in the schedule's timezone;
// schedule must be non-empty.
func nextAmbientEvent(now time.Time, schedule []config.AmbientPoint) (time.Duration, config.AmbientPoint) {
	return nextScheduledEvent(now, schedule, func(p config.AmbientPoint) (int, int) { return p.Hour, p.Min })
}

// ambientStateAt reports whether the strip should be lit at now according to the
// schedule: the state set by the most recent point at or before now, wrapping to
// the last point of the previous day when every point is still ahead. now must
// already be in the schedule's timezone; schedule must be non-empty.
func ambientStateAt(now time.Time, schedule []config.AmbientPoint) bool {
	var (
		bestPast, bestAny time.Duration = -1, -1
		pastOn, anyOn     bool
	)
	nowOfDay := time.Duration(now.Hour())*time.Hour + time.Duration(now.Minute())*time.Minute
	for _, p := range schedule {
		at := time.Duration(p.Hour)*time.Hour + time.Duration(p.Min)*time.Minute
		if at <= nowOfDay && at > bestPast {
			bestPast, pastOn = at, p.On
		}
		if at > bestAny {
			bestAny, anyOn = at, p.On
		}
	}
	if bestPast >= 0 {
		return pastOn
	}
	return anyOn
}
