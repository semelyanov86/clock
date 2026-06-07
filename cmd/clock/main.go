// Command clock renders an 800×1280 dashboard (clock, weather, Freedom24
// finance, calendar, news, quote) and pushes it to a Divoom Times Frame over the
// LAN. It runs the same on a local-network machine and on a remote host that can
// reach the device (e.g. via a WireGuard tunnel).
//
// Modes:
//
//	clock                       run the service loop (render + push on interval)
//	clock --once --out f.jpg    render one frame to a file and exit
//	clock --once --fake         use built-in sample data (no network/credentials)
//	clock --once --push         also push the rendered frame to the device
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/semelyanov86/clock/internal/app"
	"github.com/semelyanov86/clock/internal/claudeusage"
	"github.com/semelyanov86/clock/internal/config"
	"github.com/semelyanov86/clock/internal/divoom"
	"github.com/semelyanov86/clock/internal/favqs"
	"github.com/semelyanov86/clock/internal/freedom"
	"github.com/semelyanov86/clock/internal/model"
	"github.com/semelyanov86/clock/internal/render"
	"github.com/semelyanov86/clock/internal/weather"
)

const httpTimeout = 15 * time.Second

func main() {
	once := flag.Bool("once", false, "render a single frame to --out and exit")
	out := flag.String("out", "preview.jpg", "output file for --once")
	fake := flag.Bool("fake", false, "use built-in sample data (no network) with --once")
	frame := flag.Int("frame", 0, "frame index for --once (selects page / news / quote)")
	push := flag.Bool("push", false, "with --once, also push the frame to the device")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	log := newLogger(cfg.LogLevel)

	rnd, err := render.New(cfg.ClockTZ)
	if err != nil {
		log.Error("init renderer", "err", err)
		os.Exit(1)
	}

	deps := wire(cfg, log, rnd)

	if *once {
		if err := runOnce(context.Background(), cfg, log, rnd, deps, *out, *frame, *fake, *push); err != nil {
			log.Error("preview", "err", err)
			os.Exit(1)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.New(cfg, log, deps).Run(ctx); err != nil {
		log.Error("run", "err", err)
		os.Exit(1)
	}
}

func wire(cfg config.Config, log *slog.Logger, rnd *render.Renderer) app.Deps {
	deps := app.Deps{
		Renderer: rnd,
		Device:   divoom.New(cfg.Device.Host, cfg.Device.Port, cfg.Device.Timeout),
	}

	if w, err := weather.New(cfg.Weather.Lat, cfg.Weather.Lon, cfg.Weather.TZ, cfg.Weather.City, httpTimeout); err != nil {
		log.Error("init weather", "err", err)
	} else {
		deps.Weather = w
	}

	deps.QuoteText = favqs.New(cfg.Favqs.Token, httpTimeout)
	if !cfg.HasFavqs() {
		log.Warn("FAVQS_API_TOKEN not set; falling back to quote-of-the-day (English)")
	}

	if cfg.HasClaude() {
		deps.Claude = claudeusage.New(cfg.Claude.CredentialsPath, httpTimeout,
			claudeusage.WithBaseURL(cfg.Claude.APIURL), claudeusage.WithModel(cfg.Claude.Model))
		log.Info("Claude usage widget enabled", "credentials", cfg.Claude.CredentialsPath, "model", cfg.Claude.Model)
	} else {
		log.Info("Claude usage widget disabled (set CLAUDE_USAGE_ENABLED=true to enable)")
	}

	if cfg.HasFreedom() {
		fr, err := freedom.New(cfg.Freedom.Login, cfg.Freedom.Password, cfg.Freedom.UserID, cfg.Freedom.APIURL, 30*time.Second, log, freedom.WithBodyLogging(cfg.Freedom.LogBodies), freedom.WithViewOnly(cfg.Freedom.ViewOnly))
		if err != nil {
			log.Error("init freedom", "err", err)
		} else {
			deps.Portfolio = fr
			deps.News = fr
			deps.Quotes = fr
		}
	} else {
		log.Warn("Freedom24 credentials not set; portfolio, news and market quotes disabled")
	}
	return deps
}

func runOnce(ctx context.Context, cfg config.Config, log *slog.Logger, rnd *render.Renderer, deps app.Deps, out string, frame int, fake, push bool) error {
	loc, err := time.LoadLocation(cfg.ClockTZ)
	if err != nil {
		return fmt.Errorf("load timezone: %w", err)
	}

	var snap model.Snapshot
	if fake {
		snap = model.SampleSnapshot(time.Now().In(loc))
	} else {
		application := app.New(cfg, log, deps)
		log.Info("priming data sources for preview")
		pctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		application.Prime(pctx)
		cancel()
		snap = application.Snapshot(time.Now())
	}

	jpeg, err := rnd.Render(snap, frame)
	if err != nil {
		return fmt.Errorf("render: %w", err)
	}
	if err := os.WriteFile(out, jpeg, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	log.Info("wrote preview", "file", out, "bytes", len(jpeg))

	if push {
		return pushOnce(ctx, cfg, log, deps.Device, jpeg)
	}
	return nil
}

func pushOnce(ctx context.Context, cfg config.Config, log *slog.Logger, dev app.Device, jpeg []byte) error {
	ctx, cancel := context.WithTimeout(ctx, cfg.Device.Timeout)
	defer cancel()

	if cfg.Device.ClockID > 0 {
		if err := dev.PatchDialBg(ctx, cfg.Device.ClockID, jpeg); err != nil {
			return err
		}
		return dev.SetClockSelect(ctx, cfg.Device.ClockID)
	}
	id, err := dev.CreateLocalClock(ctx, "Clock Dashboard", app.ClockItems(cfg.Device.ClockFont), []string{"time_main"}, jpeg)
	if err != nil {
		return err
	}
	log.Info("created local clock", "clockId", id, "hint", "set DIVOOM_CLOCK_ID to reuse it")
	return dev.SetClockSelect(ctx, id)
}

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lv}))
}
