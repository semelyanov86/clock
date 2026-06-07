package render

import (
	"strconv"
	"strings"
	"time"

	"github.com/fogleman/gg"

	"github.com/semelyanov86/clock/internal/model"
)

// drawHeader draws the fixed top section (native-clock region kept clear, date,
// current weather, and the 3-hour / 3-day forecast strip) and returns the y of
// its bottom edge.
func (r *Renderer) drawHeader(dc *gg.Context, snap model.Snapshot, frame int) float64 {
	_ = frame // the header does not rotate

	// Panel A: clock region + date + current weather.
	fillPanel(dc, 20, 20, 760, 270, 22)

	r.drawDate(dc, snap.Generated)
	r.drawWeatherNow(dc, snap.Weather)

	// Panel B: forecast strip (hours | days).
	fillPanel(dc, 20, 302, 760, 124, 18)
	dc.SetHexColor(theme.strokeSoft)
	dc.SetLineWidth(1.5)
	dc.DrawLine(400, 322, 400, 406)
	dc.Stroke()

	r.drawForecast(dc, snap.Weather)
	return 426
}

func (r *Renderer) drawDate(dc *gg.Context, generated time.Time) {
	t := generated.In(r.loc)
	weekday := strings.ToUpper(ruWeekdayLong(t))
	dayMonth := strconv.Itoa(t.Day()) + " " + ruMonthGen(t.Month())
	year := strconv.Itoa(t.Year())

	weekday = r.fit(dc, weekday, fontBold, 24, 300)
	r.text(dc, weekday, 758, 62, 1, fontBold, 24, theme.accent)
	r.text(dc, dayMonth, 758, 112, 1, fontBold, 46, theme.text)
	r.text(dc, year, 758, 156, 1, fontRegular, 22, theme.muted)
}

func (r *Renderer) drawWeatherNow(dc *gg.Context, w model.Weather) {
	drawWeatherIcon(dc, 74, 232, 78, w.Now.Code)
	r.text(dc, roundTemp(w.Now.TempC), 126, 228, 0, fontBold, 64, theme.text)

	_, label := model.DescribeWMO(w.Now.Code)
	r.text(dc, r.fit(dc, label, fontBold, 26, 180), 262, 210, 0, fontBold, 26, theme.accent2)
	city := w.City
	if city == "" {
		city = "—"
	}
	r.text(dc, r.fit(dc, city, fontRegular, 24, 180), 262, 248, 0, fontRegular, 24, theme.muted)

	// HUD readout cluster: three labelled stats with hairline dividers, far
	// more legible at a distance than a single thin joined line of text.
	r.headerStat(dc, 456, "ОЩУЩ.", roundTemp(w.Now.FeelsC))
	r.headerStat(dc, 578, "ВЛАЖН.", strconv.Itoa(w.Now.Humidity)+"%")
	r.headerStat(dc, 700, "ВЕТЕР", strconv.Itoa(int(w.Now.WindKmh))+" км/ч")
	dc.SetHexColor(theme.strokeSoft)
	dc.SetLineWidth(1.5)
	for _, dx := range []float64{517, 639} {
		dc.DrawLine(dx, 214, dx, 256)
		dc.Stroke()
	}
}

// headerStat draws one small instrument-cluster reading: an uppercase caption
// over a bold value, centred on cx. The value shrinks to fit its column so
// longer readings (e.g. "13 км/ч") are never clipped.
func (r *Renderer) headerStat(dc *gg.Context, cx float64, label, value string) {
	r.text(dc, label, cx, 220, 0.5, fontBold, 14, theme.faint)
	size := r.fitSize(dc, value, fontBold, 24, 18, 112)
	r.text(dc, value, cx, 250, 0.5, fontBold, size, theme.text)
}

func (r *Renderer) drawForecast(dc *gg.Context, w model.Weather) {
	hourCenters := []float64{90, 212, 334}
	for i := 0; i < 3; i++ {
		if i >= len(w.Hours) {
			break
		}
		h := w.Hours[i]
		r.miniCard(dc, hourCenters[i], 312, h.Time.In(r.loc).Format("15:04"), h.Code, func(cx, y float64) {
			r.text(dc, roundTemp(h.TempC), cx, y, 0.5, fontBold, 24, theme.text)
		})
	}

	dayCenters := []float64{466, 588, 710}
	for i := 0; i < 3; i++ {
		if i >= len(w.Days) {
			break
		}
		d := w.Days[i]
		r.miniCard(dc, dayCenters[i], 312, ruWeekdayShort(d.Date), d.Code, func(cx, y float64) {
			r.drawHiLo(dc, cx, y, roundTemp(d.MaxC), roundTemp(d.MinC))
		})
	}
}

// miniCard draws a small forecast cell: a label, an icon, and a value drawn by
// the supplied callback (a single temperature for hours, a hi/lo for days).
func (r *Renderer) miniCard(dc *gg.Context, cx, top float64, label string, code int, value func(cx, y float64)) {
	r.text(dc, label, cx, top+18, 0.5, fontBold, 18, theme.muted)
	drawWeatherIcon(dc, cx, top+58, 42, code)
	value(cx, top+96)
}

// drawHiLo draws "max / min" centred on cx with the high temperature bright and
// the low dimmed, so the glance value (the high) dominates.
func (r *Renderer) drawHiLo(dc *gg.Context, cx, y float64, hi, lo string) {
	const sep = " / "
	wHi := r.measure(dc, hi, fontBold, 22)
	wSep := r.measure(dc, sep, fontBold, 22)
	wLo := r.measure(dc, lo, fontBold, 22)
	x := cx - (wHi+wSep+wLo)/2
	r.text(dc, hi, x, y, 0, fontBold, 22, theme.text)
	r.text(dc, sep, x+wHi, y, 0, fontBold, 22, theme.faint)
	r.text(dc, lo, x+wHi+wSep, y, 0, fontBold, 22, theme.muted)
}
