// Package model holds the domain types shared between the data sources, the
// renderer, and the application loop. It has no external dependencies so every
// other package can import it freely.
package model

import "time"

// Direction is the sign of a day-over-day change, used to colour numbers
// (green for up, red for down, neutral for flat).
type Direction int

const (
	// Down means the value decreased.
	Down Direction = -1
	// Flat means the value did not change.
	Flat Direction = 0
	// Up means the value increased.
	Up Direction = 1
)

// Delta is a change over the trading day: absolute and percentage. Either field
// may be zero when a source only reports one of them.
type Delta struct {
	Abs float64
	Pct float64
}

// Direction reports whether the delta is up, down, or flat. Percentage is
// preferred; the absolute change is the fallback when the percentage is zero.
func (d Delta) Direction() Direction {
	v := d.Pct
	if v == 0 {
		v = d.Abs
	}
	switch {
	case v > 0:
		return Up
	case v < 0:
		return Down
	default:
		return Flat
	}
}

// WeatherCategory groups WMO weather codes into the handful of buckets the
// renderer draws icons for.
type WeatherCategory int

// Weather categories the renderer knows how to draw.
const (
	WeatherClear WeatherCategory = iota
	WeatherPartly
	WeatherCloudy
	WeatherFog
	WeatherRain
	WeatherSnow
	WeatherThunder
)

// DescribeWMO maps a WMO weather code to a drawing category and a short Russian
// label. Unknown codes fall back to cloudy / "—".
func DescribeWMO(code int) (WeatherCategory, string) {
	switch code {
	case 0:
		return WeatherClear, "Ясно"
	case 1:
		return WeatherPartly, "Малооблачно"
	case 2:
		return WeatherPartly, "Облачно"
	case 3:
		return WeatherCloudy, "Пасмурно"
	case 45, 48:
		return WeatherFog, "Туман"
	case 51, 53, 55:
		return WeatherRain, "Морось"
	case 56, 57:
		return WeatherRain, "Ледяная морось"
	case 61, 63, 65:
		return WeatherRain, "Дождь"
	case 66, 67:
		return WeatherRain, "Ледяной дождь"
	case 71, 73, 75:
		return WeatherSnow, "Снег"
	case 77:
		return WeatherSnow, "Снежные зёрна"
	case 80, 81, 82:
		return WeatherRain, "Ливень"
	case 85, 86:
		return WeatherSnow, "Снегопад"
	case 95:
		return WeatherThunder, "Гроза"
	case 96, 99:
		return WeatherThunder, "Гроза с градом"
	default:
		return WeatherCloudy, "—"
	}
}

// WeatherNow is the current observation.
type WeatherNow struct {
	TempC    float64
	FeelsC   float64
	Code     int
	Humidity int
	WindKmh  float64
}

// WeatherHour is a single hourly forecast point.
type WeatherHour struct {
	Time  time.Time
	TempC float64
	Code  int
}

// WeatherDay is a single daily forecast point.
type WeatherDay struct {
	Date time.Time
	MinC float64
	MaxC float64
	Code int
}

// Weather aggregates current conditions plus the short hourly and daily
// forecasts shown in the header.
type Weather struct {
	City  string
	Now   WeatherNow
	Hours []WeatherHour // next few hours
	Days  []WeatherDay  // next few days
}

// Instrument is a tradable quote: an ETF, a commodity, or an FX pair. For FX
// pairs Symbol is the pair (e.g. "EUR/RUB"), Last is the rate, and Currency is
// the quote currency symbol.
type Instrument struct {
	Symbol   string
	Name     string
	Last     float64
	Currency string
	Delta    Delta
}

// Position is a single holding in the portfolio.
type Position struct {
	Symbol   string
	Name     string
	Qty      float64
	Value    float64
	Currency string
	Delta    Delta
}

// Portfolio is the account summary plus its positions.
type Portfolio struct {
	TotalValue    float64
	TotalCurrency string
	TotalDelta    Delta
	Positions     []Position
}

// NewsItem is one market-review headline.
type NewsItem struct {
	Title  string
	Date   time.Time
	Source string
}

// Quote is a single quotation with its author.
type Quote struct {
	Text   string
	Author string
}

// Snapshot is the immutable view of all data the renderer needs for one frame.
// The application loop assembles it from the latest values held in the store.
type Snapshot struct {
	Generated time.Time
	Weather   Weather
	Portfolio Portfolio
	ETFs      []Instrument
	Brent     Instrument
	FX        []Instrument
	News      []NewsItem
	Quotes    []Quote
}
