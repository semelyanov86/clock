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
	Intervals Intervals
	ClockTZ   string
	LogLevel  string
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

// Intervals controls refresh cadence and frame rotation.
type Intervals struct {
	Frame     time.Duration
	Weather   time.Duration
	Markets   time.Duration
	Portfolio time.Duration
	News      time.Duration
	Quote     time.Duration
	Claude    time.Duration
}

// HasFreedom reports whether Freedom24 credentials are present.
func (c Config) HasFreedom() bool { return c.Freedom.Login != "" && c.Freedom.Password != "" }

// HasFavqs reports whether a favqs token is present.
func (c Config) HasFavqs() bool { return c.Favqs.Token != "" }

// HasClaude reports whether the Claude-usage widget is enabled and has a
// credentials path to read the OAuth token from.
func (c Config) HasClaude() bool { return c.Claude.Enabled && c.Claude.CredentialsPath != "" }

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
			ETFSymbols:  envList("FREEDOM_ETF_SYMBOLS", "XEON.EU,IQQ0.EU,VUAA.EU,IGLN.EU"),
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
			OAuthRefresh:    getb("CLAUDE_OAUTH_REFRESH", true),
			OAuthTokenURL:   env("CLAUDE_OAUTH_TOKEN_URL", "https://platform.claude.com/v1/oauth/token"),
			OAuthClientID:   env("CLAUDE_OAUTH_CLIENT_ID", "9d1c250a-e61b-44d9-88ed-5944d1962f5e"),
		},
		Intervals: Intervals{
			Frame:     getd("FRAME_INTERVAL", 13*time.Second),
			Weather:   getd("WEATHER_INTERVAL", 10*time.Minute),
			Markets:   getd("MARKETS_INTERVAL", 60*time.Second),
			Portfolio: getd("PORTFOLIO_INTERVAL", 90*time.Second),
			News:      getd("NEWS_INTERVAL", 5*time.Minute),
			Quote:     getd("QUOTE_INTERVAL", 30*time.Minute),
			Claude:    getd("CLAUDE_USAGE_INTERVAL", 5*time.Minute),
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
