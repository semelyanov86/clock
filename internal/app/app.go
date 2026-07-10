// Package app wires the data sources, renderer, and device together: it keeps a
// snapshot store fed by periodic fetchers and a frame loop that renders and
// pushes the background to the device on a fixed cadence. The live clock is the
// device's native layer, created once and kept on top across background pushes.
package app

import (
	"context"
	"strings"
	"sync"
	"time"

	"log/slog"

	"github.com/semelyanov86/clock/internal/config"
	"github.com/semelyanov86/clock/internal/model"
	"github.com/semelyanov86/clock/internal/render"
)

// fetchTimeout bounds every external fetch.
const fetchTimeout = 30 * time.Second

// WeatherSource fetches the weather.
type WeatherSource interface {
	Fetch(ctx context.Context) (model.Weather, error)
}

// PortfolioSource fetches the portfolio.
type PortfolioSource interface {
	Portfolio(ctx context.Context) (model.Portfolio, error)
}

// NewsSource fetches market-review headlines.
type NewsSource interface {
	News(ctx context.Context, n int) ([]model.NewsItem, error)
}

// QuoteSource fetches instrument quotes by symbol.
type QuoteSource interface {
	Quotes(ctx context.Context, symbols []string) (map[string]model.Instrument, error)
}

// QuoteTextSource fetches text quotes (favqs).
type QuoteTextSource interface {
	Fetch(ctx context.Context) ([]model.Quote, error)
}

// ClaudeUsageSource fetches Claude's unified rate-limit usage (5h + weekly).
type ClaudeUsageSource interface {
	Fetch(ctx context.Context) (model.ClaudeUsage, error)
}

// Renderer renders a frame to JPEG bytes.
type Renderer interface {
	Render(snap model.Snapshot, frame int) ([]byte, error)
}

// Device is the subset of the divoom client the app needs.
type Device interface {
	Ping(ctx context.Context) error
	CreateLocalClock(ctx context.Context, name string, itemList []map[string]any, itemIDList []string, background []byte) (int, error)
	PatchDialBg(ctx context.Context, clockID int, background []byte) error
	SetClockSelect(ctx context.Context, clockID int) error
	SetBrightness(ctx context.Context, level int) error
	GetBrightness(ctx context.Context) (int, error)
}

// Deps are the wired dependencies. Source fields may be nil when their
// credentials are absent; the app skips them and logs a warning.
type Deps struct {
	Weather   WeatherSource
	Portfolio PortfolioSource
	News      NewsSource
	Quotes    QuoteSource
	QuoteText QuoteTextSource
	Claude    ClaudeUsageSource
	Renderer  Renderer
	Device    Device
}

// App is the running service.
type App struct {
	cfg     config.Config
	log     *slog.Logger
	deps    Deps
	store   *store
	clockID int

	// Brightness set-retry policy. Overridable in tests; the night-time
	// WireGuard tunnel can briefly drop, and a scheduled set is a single shot
	// (unlike the frame loop, which retries every tick), so retry to ride it out.
	retryInitial  time.Duration
	retryMax      time.Duration
	retryDeadline time.Duration
}

// New constructs the application.
func New(cfg config.Config, log *slog.Logger, deps Deps) *App {
	return &App{
		cfg:           cfg,
		log:           log,
		deps:          deps,
		store:         newStore(),
		clockID:       cfg.Device.ClockID,
		retryInitial:  30 * time.Second,
		retryMax:      2 * time.Minute,
		retryDeadline: 10 * time.Minute,
	}
}

// Run populates the store, ensures the dial exists, then renders and pushes on
// the frame interval until ctx is cancelled.
func (a *App) Run(ctx context.Context) error {
	if err := a.deps.Device.Ping(ctx); err != nil {
		a.log.Warn("device not reachable at startup; will keep retrying", "err", err)
	}

	a.log.Info("priming data sources")
	a.refreshAll(ctx)

	a.startFetchers(ctx)
	go a.runBrightnessSchedule(ctx)

	// Bring the dashboard to the foreground. The create path selects a freshly
	// created dial; a pre-pinned DIVOOM_CLOCK_ID is otherwise only background-
	// replaced (which does not change what is displayed), so select it once here
	// in case the device drifted to another clock.
	if a.clockID != 0 {
		sctx, cancel := context.WithTimeout(ctx, a.cfg.Device.Timeout)
		if err := a.deps.Device.SetClockSelect(sctx, a.clockID); err != nil {
			a.log.Warn("select configured clock", "clockId", a.clockID, "err", err)
		}
		cancel()
	}

	ticker := time.NewTicker(a.cfg.Intervals.Frame)
	defer ticker.Stop()

	frame := 0
	a.pushFrame(ctx, frame)
	for {
		select {
		case <-ctx.Done():
			a.log.Info("shutting down")
			return nil
		case <-ticker.C:
			frame++
			a.pushFrame(ctx, frame)
		}
	}
}

// pushFrame renders the current snapshot and pushes it to the device, creating
// the dial on the first successful push when no ClockId is configured.
func (a *App) pushFrame(ctx context.Context, frame int) {
	snap := a.store.snapshot(time.Now())
	jpeg, err := a.deps.Renderer.Render(snap, frame)
	if err != nil {
		a.log.Error("render frame", "frame", frame, "err", err)
		return
	}

	pctx, cancel := context.WithTimeout(ctx, a.cfg.Device.Timeout)
	defer cancel()

	if a.clockID == 0 {
		id, err := a.deps.Device.CreateLocalClock(pctx, "Clock Dashboard", a.clockItems(), []string{"time_main"}, jpeg)
		if err != nil {
			a.log.Error("create local clock", "err", err)
			return
		}
		a.clockID = id
		a.log.Info("created local clock", "clockId", id, "hint", "pin it via DIVOOM_CLOCK_ID to reuse across restarts")
		if err := a.deps.Device.SetClockSelect(pctx, id); err != nil {
			a.log.Warn("select clock", "clockId", id, "err", err)
		}
		return
	}

	// Patch the stored backdrop, then re-select the dial so the device redraws
	// it. On the Times Frame ReplaceDialBg only updates a cache that is not shown
	// live, so the displayed page would never change without this.
	if err := a.deps.Device.PatchDialBg(pctx, a.clockID, jpeg); err != nil {
		a.log.Warn("patch background", "clockId", a.clockID, "frame", frame, "err", err)
		return
	}
	if err := a.deps.Device.SetClockSelect(pctx, a.clockID); err != nil {
		a.log.Warn("refresh dial", "clockId", a.clockID, "frame", frame, "err", err)
	}
}

func (a *App) clockItems() []map[string]any { return ClockItems(a.cfg.Device.ClockFont) }

// ClockItems builds the native clock layer (disp 4 HOUR_MIN) overlaid on the
// background. The device must be set to Europe/Berlin + 24h in the Divoom app.
func ClockItems(font int) []map[string]any {
	s := render.NativeClockSlot()
	return []map[string]any{{
		"item_id":    "time_main",
		"disp":       4,
		"x":          s.X,
		"y":          s.Y,
		"w":          s.W,
		"h":          s.H,
		"size":       s.Size,
		"font":       font,
		"alig":       4, // left, hugging the clock box
		"color_1":    "#EAF2FF",
		"color_2":    "#0B0E14",
		"transp":     100,
		"hier":       2,
		"sep":        0,
		"angle":      0,
		"animation":  0,
		"image_id":   0,
		"image_addr": "",
	}}
}

// Prime fetches every source once (used by the one-shot preview mode).
func (a *App) Prime(ctx context.Context) { a.refreshAll(ctx) }

// Snapshot returns the current data snapshot.
func (a *App) Snapshot(now time.Time) model.Snapshot { return a.store.snapshot(now) }

// refreshAll fetches every source once, in parallel, before the loop starts.
func (a *App) refreshAll(ctx context.Context) {
	var wg sync.WaitGroup
	for _, fn := range []func(context.Context){a.doWeather, a.doMarkets, a.doPortfolio, a.doNews, a.doQuoteText, a.doClaude} {
		wg.Add(1)
		go func(f func(context.Context)) {
			defer wg.Done()
			f(ctx)
		}(fn)
	}
	wg.Wait()
}

// startFetchers launches one goroutine per source on its own interval.
func (a *App) startFetchers(ctx context.Context) {
	go a.periodic(ctx, a.cfg.Intervals.Weather, a.doWeather)
	go a.periodic(ctx, a.cfg.Intervals.Markets, a.doMarkets)
	go a.periodic(ctx, a.cfg.Intervals.Portfolio, a.doPortfolio)
	go a.periodic(ctx, a.cfg.Intervals.News, a.doNews)
	go a.periodic(ctx, a.cfg.Intervals.Quote, a.doQuoteText)
	go a.periodic(ctx, a.cfg.Intervals.Claude, a.doClaude)
}

func (a *App) periodic(ctx context.Context, interval time.Duration, fn func(context.Context)) {
	if interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn(ctx)
		}
	}
}

// runBrightnessSchedule sleeps until each configured time-of-day and sets the
// display brightness then, in CLOCK_TZ (the same zone the native clock uses).
// It loops forever, re-arming for the next point (today or tomorrow), until ctx
// is cancelled. No-op when the schedule is empty.
func (a *App) runBrightnessSchedule(ctx context.Context) {
	if len(a.cfg.BrightnessSchedule) == 0 {
		return
	}
	loc, err := time.LoadLocation(a.cfg.ClockTZ)
	if err != nil { // CLOCK_TZ is validated at load, so this is defensive.
		a.log.Error("brightness schedule disabled: load timezone", "tz", a.cfg.ClockTZ, "err", err)
		return
	}
	a.log.Info("brightness schedule active", "tz", a.cfg.ClockTZ, "points", len(a.cfg.BrightnessSchedule))

	for {
		now := time.Now().In(loc)
		wait, level := nextBrightnessEvent(now, a.cfg.BrightnessSchedule)
		a.log.Info("next brightness change",
			"at", now.Add(wait).Format("2006-01-02T15:04 MST"),
			"level", level, "in", wait.Round(time.Second).String())

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			a.setBrightness(ctx, level)
		}
	}
}

// setBrightness applies one brightness level, retrying transient device/network
// failures with backoff until retryDeadline (the tunnel can drop for a minute or
// two overnight). On success it reads the value back to confirm it took effect.
func (a *App) setBrightness(ctx context.Context, level int) {
	deadline := time.Now().Add(a.retryDeadline)
	backoff := a.retryInitial

	for attempt := 1; ; attempt++ {
		bctx, cancel := context.WithTimeout(ctx, a.cfg.Device.Timeout)
		err := a.deps.Device.SetBrightness(bctx, level)
		cancel()
		if err == nil {
			a.log.Info("set scheduled brightness", "level", level, "attempt", attempt)
			a.verifyBrightness(ctx, level)
			return
		}

		if time.Now().Add(backoff).After(deadline) {
			a.log.Warn("set scheduled brightness: giving up", "level", level, "attempts", attempt, "err", err)
			return
		}
		a.log.Warn("set scheduled brightness: will retry", "level", level, "attempt", attempt, "backoff", backoff.String(), "err", err)

		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < a.retryMax {
			backoff = min(backoff*2, a.retryMax)
		}
	}
}

// verifyBrightness reads the brightness back and warns if it does not match what
// was just set (a "command accepted but no effect" case the set alone can't
// catch). A failed read-back is only a warning — the set itself succeeded.
func (a *App) verifyBrightness(ctx context.Context, want int) {
	vctx, cancel := context.WithTimeout(ctx, a.cfg.Device.Timeout)
	defer cancel()
	got, err := a.deps.Device.GetBrightness(vctx)
	if err != nil {
		a.log.Warn("verify brightness: read-back failed", "want", want, "err", err)
		return
	}
	if got != want {
		a.log.Warn("verify brightness: mismatch after set", "want", want, "got", got)
		return
	}
	a.log.Info("verify brightness: confirmed", "level", got)
}

// nextBrightnessEvent returns how long to wait until the next scheduled point
// and the level to set then. now must already be in the schedule's timezone;
// schedule must be non-empty. Points earlier than now roll over to tomorrow.
func nextBrightnessEvent(now time.Time, schedule []config.BrightnessPoint) (time.Duration, int) {
	var (
		best  time.Duration = -1
		level int
	)
	for _, p := range schedule {
		t := time.Date(now.Year(), now.Month(), now.Day(), p.Hour, p.Min, 0, 0, now.Location())
		if !t.After(now) {
			t = t.AddDate(0, 0, 1)
		}
		if d := t.Sub(now); best < 0 || d < best {
			best, level = d, p.Level
		}
	}
	return best, level
}

func (a *App) doWeather(ctx context.Context) {
	if a.deps.Weather == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	w, err := a.deps.Weather.Fetch(ctx)
	if err != nil {
		a.log.Warn("fetch weather", "err", err)
		return
	}
	a.store.setWeather(w)
}

func (a *App) doPortfolio(ctx context.Context) {
	if a.deps.Portfolio == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	p, err := a.deps.Portfolio.Portfolio(ctx)
	if err != nil {
		a.log.Warn("fetch portfolio", "err", err)
		return
	}
	a.store.setPortfolio(p)
}

func (a *App) doNews(ctx context.Context) {
	if a.deps.News == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	n, err := a.deps.News.News(ctx, 9)
	if err != nil {
		a.log.Warn("fetch news", "err", err)
		return
	}
	a.store.setNews(n)
}

func (a *App) doQuoteText(ctx context.Context) {
	if a.deps.QuoteText == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	q, err := a.deps.QuoteText.Fetch(ctx)
	if err != nil {
		a.log.Warn("fetch quote", "err", err)
		return
	}
	a.store.setQuotes(q)
}

func (a *App) doClaude(ctx context.Context) {
	if a.deps.Claude == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	u, err := a.deps.Claude.Fetch(ctx)
	if err != nil {
		a.log.Warn("fetch claude usage", "err", err)
		return
	}
	a.store.setClaude(u)
}

// doMarkets fetches all instrument quotes in one batch and splits them into
// ETFs, Brent, and FX in the configured order.
func (a *App) doMarkets(ctx context.Context) {
	if a.deps.Quotes == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	symbols := make([]string, 0, len(a.cfg.Freedom.ETFSymbols)+1+len(a.cfg.Freedom.FXSymbols))
	symbols = append(symbols, a.cfg.Freedom.ETFSymbols...)
	if a.cfg.Freedom.BrentSymbol != "" {
		symbols = append(symbols, a.cfg.Freedom.BrentSymbol)
	}
	symbols = append(symbols, a.cfg.Freedom.FXSymbols...)

	quotes, err := a.deps.Quotes.Quotes(ctx, symbols)
	if err != nil {
		a.log.Warn("fetch markets", "err", err)
		return
	}

	etfs := make([]model.Instrument, 0, len(a.cfg.Freedom.ETFSymbols))
	for _, sym := range a.cfg.Freedom.ETFSymbols {
		if inst, ok := quotes[sym]; ok {
			etfs = append(etfs, inst)
		}
	}
	brent := quotes[a.cfg.Freedom.BrentSymbol]
	if brent.Symbol == "" {
		brent = model.Instrument{Symbol: "BRENT", Name: "Brent Crude"}
	} else {
		brent.Name = "Brent Crude"
	}

	fx := make([]model.Instrument, 0, len(a.cfg.Freedom.FXSymbols))
	for _, sym := range a.cfg.Freedom.FXSymbols {
		if inst, ok := quotes[sym]; ok {
			inst.Symbol = fxDisplay(sym)
			inst.Name = fxName(sym)
			inst.Currency = "₽"
			fx = append(fx, inst)
		}
	}
	a.store.setMarkets(etfs, brent, fx)
}

// fxDisplay normalises a Tradernet FX pair for the card title. The API quotes
// the ruble under its legacy ISO code RUR (e.g. EUR/RUR); the familiar label is
// RUB, so the title shows RUB while the request keeps the API's RUR symbol.
func fxDisplay(pair string) string {
	return strings.ReplaceAll(strings.ToUpper(pair), "RUR", "RUB")
}

// fxName maps a currency pair to a Russian label.
func fxName(pair string) string {
	base := pair
	if i := strings.IndexAny(pair, "/_"); i > 0 {
		base = pair[:i]
	}
	switch strings.ToUpper(base) {
	case "EUR":
		return "Евро"
	case "USD":
		return "Доллар"
	case "CNY", "CNH":
		return "Юань"
	case "GBP":
		return "Фунт"
	default:
		return base
	}
}
