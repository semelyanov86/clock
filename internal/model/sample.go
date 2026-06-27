package model

import "time"

// SampleSnapshot returns a fully populated snapshot with plausible fake data.
// It is used by the `--fake` preview mode so the layout can be iterated without
// network access or credentials.
func SampleSnapshot(now time.Time) Snapshot {
	day := now.Truncate(24 * time.Hour)
	return Snapshot{
		Generated: now,
		Weather: Weather{
			City: "Гамбург",
			Now:  WeatherNow{TempC: 18.1, FeelsC: 16.4, Code: 80, Humidity: 64, WindKmh: 13.9},
			Hours: []WeatherHour{
				{Time: now.Add(1 * time.Hour), TempC: 18.4, Code: 3},
				{Time: now.Add(2 * time.Hour), TempC: 19.0, Code: 2},
				{Time: now.Add(3 * time.Hour), TempC: 17.6, Code: 61},
			},
			Days: []WeatherDay{
				{Date: day.AddDate(0, 0, 1), MinC: 11, MaxC: 20, Code: 1},
				{Date: day.AddDate(0, 0, 2), MinC: 12, MaxC: 22, Code: 95},
				{Date: day.AddDate(0, 0, 3), MinC: 10, MaxC: 17, Code: 71},
			},
		},
		Portfolio: Portfolio{
			TotalValue: 12340.55, TotalCurrency: "€",
			TotalDelta: Delta{Abs: 146.20, Pct: 1.20},
			Positions: []Position{
				{Symbol: "IBCI.EU", Name: "iShares € Infl Bond", Qty: 42, Value: 4210.10, Currency: "€", Delta: Delta{Abs: 38.4, Pct: 0.92}},
				{Symbol: "4GLD.EU", Name: "Xetra-Gold", Qty: 60, Value: 3180.00, Currency: "€", Delta: Delta{Abs: -22.1, Pct: -0.69}},
				{Symbol: "XEON.EU", Name: "Xtrackers EUR O/N", Qty: 18, Value: 2890.40, Currency: "€", Delta: Delta{Abs: 1.2, Pct: 0.04}},
				{Symbol: "IQQ0.EU", Name: "iShares MSCI", Qty: 25, Value: 2060.05, Currency: "€", Delta: Delta{Abs: 49.7, Pct: 2.47}},
			},
		},
		ETFs: []Instrument{
			{Symbol: "XEON.EU", Name: "Xtrackers EUR O/N", Last: 160.58, Currency: "€", Delta: Delta{Abs: 0.06, Pct: 0.04}},
			{Symbol: "IQQ0.EU", Name: "iShares MSCI", Last: 82.40, Currency: "€", Delta: Delta{Abs: 1.99, Pct: 2.47}},
			{Symbol: "IBCI.EU", Name: "iShares € Infl Bond", Last: 100.24, Currency: "€", Delta: Delta{Abs: 0.91, Pct: 0.92}},
			{Symbol: "4GLD.EU", Name: "Xetra-Gold", Last: 53.00, Currency: "€", Delta: Delta{Abs: -0.37, Pct: -0.69}},
		},
		Brent: Instrument{Symbol: "BRENT", Name: "Brent Crude", Last: 82.43, Currency: "$", Delta: Delta{Abs: -0.61, Pct: -0.73}},
		FX: []Instrument{
			{Symbol: "EUR/RUB", Name: "Евро", Last: 98.72, Currency: "₽", Delta: Delta{Abs: 0.34, Pct: 0.35}},
			{Symbol: "USD/RUB", Name: "Доллар", Last: 91.05, Currency: "₽", Delta: Delta{Abs: -0.18, Pct: -0.20}},
			{Symbol: "CNY/RUB", Name: "Юань", Last: 12.58, Currency: "₽", Delta: Delta{Abs: 0.02, Pct: 0.16}},
		},
		News: []NewsItem{
			{Title: "ЕЦБ сохранил ставку на уровне 4%, рынок ждёт снижения осенью", Date: now.Add(-40 * time.Minute), Source: "Freedom24"},
			{Title: "Нефть Brent скорректировалась на фоне роста запасов в США", Date: now.Add(-90 * time.Minute), Source: "Freedom24"},
			{Title: "Индекс S&P 500 обновил исторический максимум", Date: now.Add(-3 * time.Hour), Source: "Freedom24"},
		},
		Quotes: []Quote{
			{Text: "The only way to do great work is to love what you do.", Author: "Steve Jobs"},
			{Text: "Risk comes from not knowing what you are doing.", Author: "Warren Buffett"},
		},
		Claude: ClaudeUsage{
			Updated: now,
			Block5h: ClaudeWindow{Utilization: 0.33, ResetAt: now.Add(2*time.Hour + 13*time.Minute)},
			Weekly:  ClaudeWindow{Utilization: 0.71, ResetAt: now.Add(5 * 24 * time.Hour)},
			Valid:   true,
		},
	}
}
