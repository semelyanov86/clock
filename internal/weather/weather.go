// Package weather fetches current conditions plus short hourly and daily
// forecasts from the Open-Meteo API. Open-Meteo requires no API key for
// non-commercial use.
package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/semelyanov86/clock/internal/model"
)

const defaultBaseURL = "https://api.open-meteo.com/v1/forecast"

// Client fetches weather for a fixed location.
type Client struct {
	lat, lon float64
	city     string
	loc      *time.Location
	tz       string
	baseURL  string
	httpc    *http.Client
}

// Option customises a Client.
type Option func(*Client)

// WithHTTPClient overrides the HTTP client (used in tests).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpc = h } }

// WithBaseURL overrides the API base URL (used in tests).
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

// New returns a weather client. tz is an IANA timezone (e.g. Europe/Berlin)
// used both for the API request and to interpret returned local times.
func New(lat, lon float64, tz, city string, timeout time.Duration, opts ...Option) (*Client, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("load timezone %q: %w", tz, err)
	}
	c := &Client{
		lat: lat, lon: lon, city: city, loc: loc, tz: tz,
		baseURL: defaultBaseURL,
		httpc:   &http.Client{Timeout: timeout},
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

type response struct {
	Current struct {
		Time     string  `json:"time"`
		Temp     float64 `json:"temperature_2m"`
		Feels    float64 `json:"apparent_temperature"`
		Humidity int     `json:"relative_humidity_2m"`
		Wind     float64 `json:"wind_speed_10m"`
		Code     int     `json:"weather_code"`
	} `json:"current"`
	Hourly struct {
		Time []string  `json:"time"`
		Temp []float64 `json:"temperature_2m"`
		Code []int     `json:"weather_code"`
	} `json:"hourly"`
	Daily struct {
		Time []string  `json:"time"`
		Code []int     `json:"weather_code"`
		Max  []float64 `json:"temperature_2m_max"`
		Min  []float64 `json:"temperature_2m_min"`
	} `json:"daily"`
}

// Fetch retrieves the current weather plus the next 3 hours and next 3 days.
func (c *Client) Fetch(ctx context.Context) (model.Weather, error) {
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(c.lat, 'f', 4, 64))
	q.Set("longitude", strconv.FormatFloat(c.lon, 'f', 4, 64))
	q.Set("current", "temperature_2m,relative_humidity_2m,apparent_temperature,weather_code,wind_speed_10m")
	q.Set("hourly", "temperature_2m,weather_code")
	q.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min")
	q.Set("timezone", c.tz)
	q.Set("forecast_days", "4") // today + next 3 days

	reqURL := c.baseURL + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return model.Weather{}, fmt.Errorf("new request: %w", err)
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return model.Weather{}, fmt.Errorf("fetch weather: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return model.Weather{}, fmt.Errorf("read weather response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return model.Weather{}, fmt.Errorf("weather: http %d", resp.StatusCode)
	}

	var r response
	if err := json.Unmarshal(body, &r); err != nil {
		return model.Weather{}, fmt.Errorf("decode weather response: %w", err)
	}
	return c.toModel(r), nil
}

func (c *Client) toModel(r response) model.Weather {
	now := parseLocal(r.Current.Time, c.loc)
	if now.IsZero() {
		now = time.Now().In(c.loc) // fall back to wall clock if the API time is unparseable
	}

	w := model.Weather{
		City: c.city,
		Now: model.WeatherNow{
			TempC:    r.Current.Temp,
			FeelsC:   r.Current.Feels,
			Code:     r.Current.Code,
			Humidity: r.Current.Humidity,
			WindKmh:  r.Current.Wind,
		},
	}

	// Next 3 hourly points strictly after the current observation time.
	for i := range r.Hourly.Time {
		if len(w.Hours) == 3 {
			break
		}
		t := parseLocal(r.Hourly.Time[i], c.loc)
		if !t.After(now) {
			continue
		}
		w.Hours = append(w.Hours, model.WeatherHour{
			Time:  t,
			TempC: safeAt(r.Hourly.Temp, i),
			Code:  safeAt(r.Hourly.Code, i),
		})
	}

	// Daily: skip today (index 0), take the next 3.
	for i := 1; i < len(r.Daily.Time) && len(w.Days) < 3; i++ {
		w.Days = append(w.Days, model.WeatherDay{
			Date: parseDay(r.Daily.Time[i], c.loc),
			MinC: safeAt(r.Daily.Min, i),
			MaxC: safeAt(r.Daily.Max, i),
			Code: safeAt(r.Daily.Code, i),
		})
	}
	return w
}

func parseLocal(s string, loc *time.Location) time.Time {
	t, err := time.ParseInLocation("2006-01-02T15:04", s, loc)
	if err != nil {
		return time.Time{}
	}
	return t
}

func parseDay(s string, loc *time.Location) time.Time {
	t, err := time.ParseInLocation("2006-01-02", s, loc)
	if err != nil {
		return time.Time{}
	}
	return t
}

// safeAt returns s[i] or the zero value when i is out of range.
func safeAt[T any](s []T, i int) T {
	if i >= 0 && i < len(s) {
		return s[i]
	}
	var zero T
	return zero
}
