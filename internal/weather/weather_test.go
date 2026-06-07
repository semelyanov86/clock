package weather

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const sampleResponse = `{
  "current": {"time":"2026-06-07T13:00","temperature_2m":18.1,"apparent_temperature":16.4,"relative_humidity_2m":64,"wind_speed_10m":13.9,"weather_code":80},
  "hourly": {
    "time":["2026-06-07T11:00","2026-06-07T12:00","2026-06-07T13:00","2026-06-07T14:00","2026-06-07T15:00","2026-06-07T16:00"],
    "temperature_2m":[11,12,13,14,15,16],
    "weather_code":[1,1,1,2,3,61]
  },
  "daily": {
    "time":["2026-06-07","2026-06-08","2026-06-09","2026-06-10"],
    "weather_code":[1,2,3,95],
    "temperature_2m_max":[20,21,22,23],
    "temperature_2m_min":[10,11,12,13]
  }
}`

func TestFetch(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sampleResponse))
	}))
	defer srv.Close()

	c, err := New(53.55, 9.99, "Europe/Berlin", "Гамбург", 5*time.Second, WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if w.City != "Гамбург" {
		t.Errorf("city = %q", w.City)
	}
	if w.Now.TempC != 18.1 || w.Now.Humidity != 64 || w.Now.Code != 80 {
		t.Errorf("now = %+v", w.Now)
	}
	if len(w.Hours) != 3 {
		t.Fatalf("hours = %d, want 3", len(w.Hours))
	}
	if got := w.Hours[0].Time.Hour(); got != 14 {
		t.Errorf("first forecast hour = %d, want 14", got)
	}
	if w.Hours[2].TempC != 16 {
		t.Errorf("third hour temp = %v, want 16", w.Hours[2].TempC)
	}
	if len(w.Days) != 3 {
		t.Fatalf("days = %d, want 3 (today skipped)", len(w.Days))
	}
	if w.Days[0].Date.Day() != 8 {
		t.Errorf("first day = %d, want 8 (tomorrow)", w.Days[0].Date.Day())
	}
	if w.Days[0].MaxC != 21 || w.Days[0].MinC != 11 {
		t.Errorf("first day temps = %v/%v, want 21/11", w.Days[0].MaxC, w.Days[0].MinC)
	}
}
