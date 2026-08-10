// Package config loads the service configuration from environment variables
// (12-factor). Values have sensible defaults; a variable that is *present but
// invalid* is a hard error (no silent fallback), and ranges are validated so a
// typo surfaces at startup rather than as confusing runtime behaviour.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config is the fully resolved configuration.
type Config struct {
	Device    Device
	Freedom   Freedom
	Favqs     Favqs
	Weather   Weather
	Claude    Claude
	Codex     Codex
	Intervals Intervals
	// BrightnessSchedule sets the display brightness at fixed times of day (in
	// CLOCK_TZ). Empty disables the scheduler. The user drops brightness to 0 at
	// night manually; these points ramp it back up in the morning.
	BrightnessSchedule []BrightnessPoint
	Ambient            Ambient
	ClockTZ            string
	LogLevel           string
}

// BrightnessPoint is one scheduled brightness change: at Hour:Min (in CLOCK_TZ)
// the display is set to Level on the 0–100 scale.
type BrightnessPoint struct {
	Hour  int
	Min   int
	Level int
}

// Ambient configures the side RGB light strip: when it is lit (Schedule, in
// CLOCK_TZ) and the pool each day's look is drawn from. Every switch-on picks a
// fresh random effect and colour, so no two days look the same.
type Ambient struct {
	// Schedule turns the strip on and off at fixed times of day. Empty disables
	// the scheduler and leaves the strip untouched.
	Schedule []AmbientPoint
	// Brightness is the level the strip is lit at (1–100).
	Brightness int
	// Effects are the glow modes to draw from (device values 0–7). Effect 0 lights
	// only the bottom of the strip, so the default pool leaves it out.
	Effects []int
	// Colors are the "#rrggbb" colours to draw from.
	Colors []string
	// CycleChance is the percent chance (0–100) that a day glows through the whole
	// spectrum instead of a single colour.
	CycleChance int
}

// AmbientPoint is one scheduled switch of the side light: at Hour:Min (in
// CLOCK_TZ) the strip is lit with a fresh random look (On) or blanked.
type AmbientPoint struct {
	Hour int
	Min  int
	On   bool
}

// Device holds Divoom LAN connection settings.
type Device struct {
	Host      string
	Port      int
	Timeout   time.Duration
	ClockID   int // 0 = create a new local clock on startup
	ClockFont int // device font id for the native disp 4 clock layer
}

// Freedom holds Tradernet session credentials and the instrument symbols.
type Freedom struct {
	APIURL      string
	Login       string
	Password    string
	UserID      string
	ETFSymbols  []string
	BrentSymbol string
	FXSymbols   []string
	LogBodies   bool // when true, debug-log raw response bodies (SID redacted) for E2E
	// ViewOnly authenticates in read-only mode (cannot trade). Safe default, but
	// Tradernet then withholds the per-position portfolio breakdown — only a full
	// session returns positions (the service still never sends trade commands).
	ViewOnly bool
}

// Favqs holds the favqs.com API token.
type Favqs struct {
	Token string
}

// Weather holds the Open-Meteo location (no API key required).
type Weather struct {
	Lat  float64
	Lon  float64
	City string
	TZ   string
}

// Claude holds settings for the optional Claude-usage widget. When enabled, the
// service reads the Claude Code OAuth token from CredentialsPath and probes the
// Anthropic API to read the unified rate-limit headers — the same 5-hour and
// weekly windows shown by Claude Code's /usage. This relies on the subscription
// OAuth token and undocumented response headers, so it is opt-in (off by
// default) and each poll consumes a negligible amount of the reported quota.
type Claude struct {
	Enabled         bool
	CredentialsPath string
	Model           string
	APIURL          string

	// OAuth token refresh. When OAuthRefresh is on, the service exchanges the
	// stored refresh token for a fresh access token as it nears expiry, so the
	// widget keeps working on an idle server without Claude Code running. The
	// endpoint and client id are configurable because they are undocumented and
	// have moved before.
	OAuthRefresh  bool
	OAuthTokenURL string
	OAuthClientID string
}

// Codex holds settings for the optional Codex usage widget. The service invokes
// the local CLI's app-server protocol and reads the main account rate-limit
// bucket without starting a model turn.
type Codex struct {
	Enabled bool
	Bin     string
}

// Intervals controls refresh cadence and frame rotation.
type Intervals struct {
	Frame     time.Duration
	Weather   time.Duration
	Markets   time.Duration
	Portfolio time.Duration
	News      time.Duration
	Quote     time.Duration
	Claude    time.Duration
	Codex     time.Duration
}

// HasFreedom reports whether Freedom24 credentials are present.
func (c Config) HasFreedom() bool { return c.Freedom.Login != "" && c.Freedom.Password != "" }

// HasFavqs reports whether a favqs token is present.
func (c Config) HasFavqs() bool { return c.Favqs.Token != "" }

// HasClaude reports whether the Claude-usage widget is enabled and has a
// credentials path to read the OAuth token from.
func (c Config) HasClaude() bool { return c.Claude.Enabled && c.Claude.CredentialsPath != "" }

// HasCodex reports whether the Codex-usage widget is enabled and has a CLI
// executable configured.
func (c Config) HasCodex() bool { return c.Codex.Enabled && c.Codex.Bin != "" }

// Load reads the configuration from the environment, applying defaults and
// validating every value. It returns the joined set of all problems found.
func Load() (Config, error) {
	var errs []error
	track := func(_ any, err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	geti := func(key string, def int) int { v, err := envInt(key, def); track(v, err); return v }
	getf := func(key string, def float64) float64 { v, err := envFloat(key, def); track(v, err); return v }
	getd := func(key string, def time.Duration) time.Duration { v, err := envDur(key, def); track(v, err); return v }
	getb := func(key string, def bool) bool { v, err := envBool(key, def); track(v, err); return v }

	brightness, brightnessErr := parseBrightnessSchedule(env("BRIGHTNESS_SCHEDULE", defaultBrightnessSchedule))
	if brightnessErr != nil {
		errs = append(errs, brightnessErr)
	}
	ambientSchedule, ambientErr := parseAmbientSchedule(env("AMBIENT_SCHEDULE", defaultAmbientSchedule))
	if ambientErr != nil {
		errs = append(errs, ambientErr)
	}
	ambientEffects, effectsErr := parseIntList("AMBIENT_EFFECTS", env("AMBIENT_EFFECTS", defaultAmbientEffects))
	if effectsErr != nil {
		errs = append(errs, effectsErr)
	}

	c := Config{
		Device: Device{
			Host:      env("DIVOOM_DEVICE_HOST", "192.168.178.40"),
			Port:      geti("DIVOOM_DEVICE_PORT", 9000),
			Timeout:   time.Duration(geti("DIVOOM_TIMEOUT_MS", 45000)) * time.Millisecond,
			ClockID:   geti("DIVOOM_CLOCK_ID", 0),
			ClockFont: geti("DIVOOM_CLOCK_FONT", 24),
		},
		Freedom: Freedom{
			APIURL:      env("FREEDOM_API_URL", "https://tradernet.com/api"),
			Login:       env("FREEDOM_LOGIN", ""),
			Password:    env("FREEDOM_PASSWORD", ""),
			UserID:      env("FREEDOM_USER_ID", ""),
			ETFSymbols:  envList("FREEDOM_ETF_SYMBOLS", "XEON.EU,IQQ0.EU,IBCI.EU,4GLD.EU"),
			BrentSymbol: env("FREEDOM_BRENT_SYMBOL", "BRNT.EU"),
			FXSymbols:   envList("FREEDOM_FX_SYMBOLS", "EUR/RUR,USD/RUR,CNY/RUR"),
			LogBodies:   getb("FREEDOM_LOG_BODIES", false),
			ViewOnly:    getb("FREEDOM_VIEW_ONLY", false),
		},
		Favqs: Favqs{Token: env("FAVQS_API_TOKEN", "")},
		Weather: Weather{
			Lat:  getf("WEATHER_LAT", 53.55),
			Lon:  getf("WEATHER_LON", 9.99),
			City: env("WEATHER_CITY", "Гамбург"),
			TZ:   env("WEATHER_TZ", "Europe/Berlin"),
		},
		Claude: Claude{
			Enabled:         getb("CLAUDE_USAGE_ENABLED", false),
			CredentialsPath: env("CLAUDE_CREDENTIALS_PATH", defaultClaudeCredentialsPath()),
			Model:           env("CLAUDE_USAGE_MODEL", "claude-haiku-4-5-20251001"),
			APIURL:          env("CLAUDE_API_URL", "https://api.anthropic.com"),
			// Off by default: the in-process refresh is rejected (HTTP 429) by the
			// real token endpoint, which appears to distinguish the genuine Claude
			// Code client. Token freshness is handled out-of-process by the
			// claude-token-refresh systemd timer (see deploy/). Kept opt-in in case
			// the endpoint's behaviour changes.
			OAuthRefresh:  getb("CLAUDE_OAUTH_REFRESH", false),
			OAuthTokenURL: env("CLAUDE_OAUTH_TOKEN_URL", "https://platform.claude.com/v1/oauth/token"),
			OAuthClientID: env("CLAUDE_OAUTH_CLIENT_ID", "9d1c250a-e61b-44d9-88ed-5944d1962f5e"),
		},
		Codex: Codex{
			Enabled: getb("CODEX_USAGE_ENABLED", false),
			Bin:     env("CODEX_BIN", "codex"),
		},
		Intervals: Intervals{
			Frame:     getd("FRAME_INTERVAL", 13*time.Second),
			Weather:   getd("WEATHER_INTERVAL", 10*time.Minute),
			Markets:   getd("MARKETS_INTERVAL", 60*time.Second),
			Portfolio: getd("PORTFOLIO_INTERVAL", 90*time.Second),
			News:      getd("NEWS_INTERVAL", 5*time.Minute),
			Quote:     getd("QUOTE_INTERVAL", 30*time.Minute),
			Claude:    getd("CLAUDE_USAGE_INTERVAL", 5*time.Minute),
			Codex:     getd("CODEX_USAGE_INTERVAL", 5*time.Minute),
		},
		BrightnessSchedule: brightness,
		Ambient: Ambient{
			Schedule:    ambientSchedule,
			Brightness:  geti("AMBIENT_BRIGHTNESS", 70),
			Effects:     ambientEffects,
			Colors:      envList("AMBIENT_COLORS", defaultAmbientColors),
			CycleChance: geti("AMBIENT_COLOR_CYCLE_CHANCE", 30),
		},
		ClockTZ:  env("CLOCK_TZ", "Europe/Berlin"),
		LogLevel: env("LOG_LEVEL", "info"),
	}

	errs = append(errs, c.validate()...)
	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return c, nil
}

func (c Config) validate() []error {
	var errs []error
	add := func(ok bool, msg string) {
		if !ok {
			errs = append(errs, errors.New(msg))
		}
	}

	add(c.Device.Host != "", "DIVOOM_DEVICE_HOST must not be empty")
	add(c.Device.Port >= 1 && c.Device.Port <= 65535, fmt.Sprintf("DIVOOM_DEVICE_PORT out of range: %d", c.Device.Port))
	add(c.Device.Timeout > 0, "DIVOOM_TIMEOUT_MS must be > 0")

	for name, d := range map[string]time.Duration{
		"FRAME_INTERVAL":        c.Intervals.Frame,
		"WEATHER_INTERVAL":      c.Intervals.Weather,
		"MARKETS_INTERVAL":      c.Intervals.Markets,
		"PORTFOLIO_INTERVAL":    c.Intervals.Portfolio,
		"NEWS_INTERVAL":         c.Intervals.News,
		"QUOTE_INTERVAL":        c.Intervals.Quote,
		"CLAUDE_USAGE_INTERVAL": c.Intervals.Claude,
		"CODEX_USAGE_INTERVAL":  c.Intervals.Codex,
	} {
		add(d > 0, name+" must be > 0")
	}

	add(c.Weather.Lat >= -90 && c.Weather.Lat <= 90, fmt.Sprintf("WEATHER_LAT out of range: %v", c.Weather.Lat))
	add(c.Weather.Lon >= -180 && c.Weather.Lon <= 180, fmt.Sprintf("WEATHER_LON out of range: %v", c.Weather.Lon))

	if _, err := time.LoadLocation(c.ClockTZ); err != nil {
		errs = append(errs, fmt.Errorf("invalid CLOCK_TZ %q: %w", c.ClockTZ, err))
	}
	if _, err := time.LoadLocation(c.Weather.TZ); err != nil {
		errs = append(errs, fmt.Errorf("invalid WEATHER_TZ %q: %w", c.Weather.TZ, err))
	}
	if u, err := url.Parse(c.Freedom.APIURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("FREEDOM_API_URL must be an http(s) URL, got %q", c.Freedom.APIURL))
	}
	if c.Claude.Enabled {
		add(c.Claude.CredentialsPath != "", "CLAUDE_CREDENTIALS_PATH must not be empty when CLAUDE_USAGE_ENABLED=true (could not resolve a default; set it explicitly)")
		add(c.Claude.Model != "", "CLAUDE_USAGE_MODEL must not be empty when CLAUDE_USAGE_ENABLED=true")
		if u, err := url.Parse(c.Claude.APIURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, fmt.Errorf("CLAUDE_API_URL must be an http(s) URL, got %q", c.Claude.APIURL))
		}
		if c.Claude.OAuthRefresh {
			add(c.Claude.OAuthClientID != "", "CLAUDE_OAUTH_CLIENT_ID must not be empty when CLAUDE_OAUTH_REFRESH=true")
			if u, err := url.Parse(c.Claude.OAuthTokenURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				errs = append(errs, fmt.Errorf("CLAUDE_OAUTH_TOKEN_URL must be an http(s) URL, got %q", c.Claude.OAuthTokenURL))
			}
		}
	}
	if c.Codex.Enabled {
		add(c.Codex.Bin != "", "CODEX_BIN must not be empty when CODEX_USAGE_ENABLED=true")
	}
	// The side-light pools are validated even with an empty schedule: they are also
	// drawn from by `clock --ambient on`, and an out-of-range effect is stored by
	// the firmware as 0 (bottom-only) instead of being rejected, so a typo would
	// otherwise surface as a wrong-looking strip rather than an error.
	add(c.Ambient.Brightness >= 1 && c.Ambient.Brightness <= 100,
		fmt.Sprintf("AMBIENT_BRIGHTNESS out of range: %d (1-100)", c.Ambient.Brightness))
	add(c.Ambient.CycleChance >= 0 && c.Ambient.CycleChance <= 100,
		fmt.Sprintf("AMBIENT_COLOR_CYCLE_CHANCE out of range: %d (0-100)", c.Ambient.CycleChance))
	add(len(c.Ambient.Effects) > 0, "AMBIENT_EFFECTS must list at least one effect")
	for _, e := range c.Ambient.Effects {
		add(e >= 0 && e <= maxAmbientEffect, fmt.Sprintf("AMBIENT_EFFECTS value out of range: %d (0-%d)", e, maxAmbientEffect))
	}
	add(len(c.Ambient.Colors) > 0, "AMBIENT_COLORS must list at least one colour")
	for _, col := range c.Ambient.Colors {
		add(isHexColor(col), fmt.Sprintf("AMBIENT_COLORS value %q must be #rrggbb", col))
	}
	return errs
}

// defaultClaudeCredentialsPath returns the standard Claude Code credentials
// file path (~/.claude/.credentials.json), or "" when the home directory
// cannot be resolved (in which case CLAUDE_CREDENTIALS_PATH must be set).
func defaultClaudeCredentialsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", ".credentials.json")
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func envInt(key string, def int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return def, fmt.Errorf("invalid %s=%q: must be an integer", key, v)
	}
	return n, nil
}

func envFloat(key string, def float64) (float64, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def, nil
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return def, fmt.Errorf("invalid %s=%q: must be a number", key, v)
	}
	return f, nil
}

func envDur(key string, def time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil {
		return def, fmt.Errorf("invalid %s=%q: must be a Go duration (e.g. 30s, 10m)", key, v)
	}
	return d, nil
}

func envBool(key string, def bool) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return def, fmt.Errorf("invalid %s=%q: must be a boolean", key, v)
	}
	return b, nil
}

// defaultBrightnessSchedule ramps the display up in the morning (Berlin time):
// the user blanks it at night, these points restore it. Override via
// BRIGHTNESS_SCHEDULE; set it empty to disable the scheduler.
const defaultBrightnessSchedule = "04:00=1,05:00=9,06:00=25,07:00=40,08:00=50"

// parseBrightnessSchedule parses "HH:MM=LEVEL,HH:MM=LEVEL,…" into schedule
// points. An empty string disables the scheduler; a present-but-malformed value
// is an error (no silent fallback).
func parseBrightnessSchedule(raw string) ([]BrightnessPoint, error) {
	var points []BrightnessPoint
	err := eachScheduleEntry("BRIGHTNESS_SCHEDULE", raw, "HH:MM=LEVEL", func(entry string, hour, minute int, value string) error {
		level, err := strconv.Atoi(value)
		if err != nil || level < 0 || level > 100 {
			return fmt.Errorf("invalid BRIGHTNESS_SCHEDULE level in %q: must be 0-100", entry)
		}
		points = append(points, BrightnessPoint{Hour: hour, Min: minute, Level: level})
		return nil
	})
	return points, err
}

// defaultAmbientSchedule lights the side strip in the morning and blanks it for
// the night (Berlin time). Override via AMBIENT_SCHEDULE; set it empty to leave
// the strip alone.
const defaultAmbientSchedule = "07:00=on,22:30=off"

// defaultAmbientEffects is the pool of glow modes a day is drawn from. It omits
// effect 0 (lights only the bottom of the strip) and 7 (a visual duplicate of 1).
const defaultAmbientEffects = "1,2,3,4,5,6"

// defaultAmbientColors mixes white with saturated hues so some days glow plain
// white and others in colour.
const defaultAmbientColors = "#FFFFFF,#FFD166,#FF8C42,#FF5C5C,#FF3CAC,#845EF7,#4C6EF5,#22D3EE,#2FD968"

// maxAmbientEffect mirrors divoom.AmbientEffectMax: the highest glow mode the
// firmware accepts. Anything above it is silently stored as 0.
const maxAmbientEffect = 7

// parseAmbientSchedule parses "HH:MM=on,HH:MM=off,…" into switch points. An
// empty string disables the scheduler; a present-but-malformed value is an error.
func parseAmbientSchedule(raw string) ([]AmbientPoint, error) {
	var points []AmbientPoint
	err := eachScheduleEntry("AMBIENT_SCHEDULE", raw, "HH:MM=on|off", func(entry string, hour, minute int, value string) error {
		var on bool
		switch strings.ToLower(value) {
		case "on", "1", "true":
			on = true
		case "off", "0", "false":
			on = false
		default:
			return fmt.Errorf("invalid AMBIENT_SCHEDULE state in %q: want on or off", entry)
		}
		points = append(points, AmbientPoint{Hour: hour, Min: minute, On: on})
		return nil
	})
	return points, err
}

// eachScheduleEntry splits a "HH:MM=VALUE,…" schedule, parses the time of day,
// and hands each entry's raw value to fn. want names the expected entry shape in
// error messages, key the environment variable being parsed.
func eachScheduleEntry(key, raw, want string, fn func(entry string, hour, minute int, value string) error) error {
	for entry := range strings.SplitSeq(strings.TrimSpace(raw), ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		hm, value, ok := strings.Cut(entry, "=")
		if !ok {
			return fmt.Errorf("invalid %s entry %q: want %s", key, entry, want)
		}
		hh, mm, ok := strings.Cut(strings.TrimSpace(hm), ":")
		if !ok {
			return fmt.Errorf("invalid %s time %q: want HH:MM", key, hm)
		}
		hour, err := strconv.Atoi(strings.TrimSpace(hh))
		if err != nil || hour < 0 || hour > 23 {
			return fmt.Errorf("invalid %s hour in %q: must be 0-23", key, entry)
		}
		minute, err := strconv.Atoi(strings.TrimSpace(mm))
		if err != nil || minute < 0 || minute > 59 {
			return fmt.Errorf("invalid %s minute in %q: must be 0-59", key, entry)
		}
		if err := fn(entry, hour, minute, strings.TrimSpace(value)); err != nil {
			return err
		}
	}
	return nil
}

// parseIntList parses a comma-separated list of integers, naming key in errors.
func parseIntList(key, raw string) ([]int, error) {
	var out []int
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid %s value %q: must be an integer", key, part)
		}
		out = append(out, n)
	}
	return out, nil
}

// isHexColor reports whether s is a "#rrggbb" colour literal.
func isHexColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, r := range s[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

func envList(key, def string) []string {
	raw := env(key, def)
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
