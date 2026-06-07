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
	dc.SetHexColor(theme.stroke)
	dc.SetLineWidth(1.5)
	dc.DrawLine(400, 318, 400, 410)
	dc.Stroke()

	r.drawForecast(dc, snap.Weather)
	return 426
}

func (r *Renderer) drawDate(dc *gg.Context, generated time.Time) {
	t := generated.In(r.loc)
	weekday := ruWeekdayLong(t)
	dayMonth := strconv.Itoa(t.Day()) + " " + ruMonthGen(t.Month())
	year := strconv.Itoa(t.Year())

	weekday = r.fit(dc, weekday, fontBold, 30, 290)
	r.text(dc, weekday, 758, 70, 1, fontBold, 30, theme.accent)
	r.text(dc, dayMonth, 758, 120, 1, fontBold, 42, theme.text)
	r.text(dc, year, 758, 160, 1, fontRegular, 22, theme.muted)
}

func (r *Renderer) drawWeatherNow(dc *gg.Context, w model.Weather) {
	drawWeatherIcon(dc, 74, 234, 74, w.Now.Code)
	r.text(dc, roundTemp(w.Now.TempC), 122, 230, 0, fontBold, 62, theme.text)

	_, label := model.DescribeWMO(w.Now.Code)
	r.text(dc, label, 256, 212, 0, fontBold, 26, theme.accent2)
	city := w.City
	if city == "" {
		city = "—"
	}
	r.text(dc, city, 256, 250, 0, fontRegular, 24, theme.muted)

	details := strings.Join([]string{
		"ощущ. " + roundTemp(w.Now.FeelsC),
		"вл " + strconv.Itoa(w.Now.Humidity) + "%",
		strconv.Itoa(int(w.Now.WindKmh)) + " км/ч",
	}, " · ")
	details = r.fit(dc, details, fontRegular, 18, 360)
	r.text(dc, details, 758, 234, 1, fontRegular, 18, theme.muted)
}

func (r *Renderer) drawForecast(dc *gg.Context, w model.Weather) {
	hourCenters := []float64{90, 212, 334}
	for i := 0; i < 3; i++ {
		if i >= len(w.Hours) {
			break
		}
		h := w.Hours[i]
		r.miniCard(dc, hourCenters[i], 312, h.Time.In(r.loc).Format("15:04"), h.Code, roundTemp(h.TempC), theme.text)
	}

	dayCenters := []float64{466, 588, 710}
	for i := 0; i < 3; i++ {
		if i >= len(w.Days) {
			break
		}
		d := w.Days[i]
		val := roundTemp(d.MaxC) + " / " + roundTemp(d.MinC)
		r.miniCard(dc, dayCenters[i], 312, ruWeekdayShort(d.Date), d.Code, val, theme.text)
	}
}

// miniCard draws a small forecast cell: a label, an icon, and a value line.
func (r *Renderer) miniCard(dc *gg.Context, cx, top float64, label string, code int, value, valColor string) {
	r.text(dc, label, cx, top+18, 0.5, fontRegular, 18, theme.muted)
	drawWeatherIcon(dc, cx, top+58, 40, code)
	r.text(dc, value, cx, top+96, 0.5, fontBold, 22, valColor)
}
