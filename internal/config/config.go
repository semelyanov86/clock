// Package config loads the service configuration from environment variables
// (12-factor). All values have sensible defaults except secrets; missing
// secrets are surfaced via the Has* helpers so the loop can skip a source
// instead of crashing.
package config

import (
	"fmt"
	"os"
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

// Intervals controls refresh cadence and frame rotation.
type Intervals struct {
	Frame     time.Duration
	Weather   time.Duration
	Markets   time.Duration
	Portfolio time.Duration
	News      time.Duration
	Quote     time.Duration
}

// HasFreedom reports whether Freedom24 credentials are present.
func (c Config) HasFreedom() bool { return c.Freedom.Login != "" && c.Freedom.Password != "" }

// HasFavqs reports whether a favqs token is present.
func (c Config) HasFavqs() bool { return c.Favqs.Token != "" }

// Load reads the configuration from the environment, applying defaults.
func Load() (Config, error) {
	c := Config{
		Device: Device{
			Host:      env("DIVOOM_DEVICE_HOST", "192.168.178.40"),
			Port:      envInt("DIVOOM_DEVICE_PORT", 9000),
			Timeout:   time.Duration(envInt("DIVOOM_TIMEOUT_MS", 45000)) * time.Millisecond,
			ClockID:   envInt("DIVOOM_CLOCK_ID", 0),
			ClockFont: envInt("DIVOOM_CLOCK_FONT", 24),
		},
		Freedom: Freedom{
			APIURL:      env("FREEDOM_API_URL", "https://tradernet.com/api"),
			Login:       env("FREEDOM_LOGIN", ""),
			Password:    env("FREEDOM_PASSWORD", ""),
			UserID:      env("FREEDOM_USER_ID", ""),
			ETFSymbols:  envList("FREEDOM_ETF_SYMBOLS", "XEON.EU,IQQ0.EU,VUAA.EU,IGLN.EU"),
			BrentSymbol: env("FREEDOM_BRENT_SYMBOL", "BRN.NYMEX"),
			FXSymbols:   envList("FREEDOM_FX_SYMBOLS", "EUR/RUB,USD/RUB,CNY/RUB"),
		},
		Favqs: Favqs{Token: env("FAVQS_API_TOKEN", "")},
		Weather: Weather{
			Lat:  envFloat("WEATHER_LAT", 53.55),
			Lon:  envFloat("WEATHER_LON", 9.99),
			City: env("WEATHER_CITY", "Гамбург"),
			TZ:   env("WEATHER_TZ", "Europe/Berlin"),
		},
		Intervals: Intervals{
			Frame:     envDur("FRAME_INTERVAL", 13*time.Second),
			Weather:   envDur("WEATHER_INTERVAL", 10*time.Minute),
			Markets:   envDur("MARKETS_INTERVAL", 60*time.Second),
			Portfolio: envDur("PORTFOLIO_INTERVAL", 90*time.Second),
			News:      envDur("NEWS_INTERVAL", 5*time.Minute),
			Quote:     envDur("QUOTE_INTERVAL", 30*time.Minute),
		},
		ClockTZ:  env("CLOCK_TZ", "Europe/Berlin"),
		LogLevel: env("LOG_LEVEL", "info"),
	}

	if _, err := time.LoadLocation(c.ClockTZ); err != nil {
		return Config{}, fmt.Errorf("invalid CLOCK_TZ %q: %w", c.ClockTZ, err)
	}
	if c.Device.Host == "" {
		return Config{}, fmt.Errorf("DIVOOM_DEVICE_HOST must not be empty")
	}
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return def
}

func envDur(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil {
			return d
		}
	}
	return def
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
