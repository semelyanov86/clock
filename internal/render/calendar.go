package render

import (
	"strconv"
	"time"

	"github.com/fogleman/gg"
)

// drawCalendar draws the month grid for the date t (Monday-first, Russian
// headers, current day highlighted) within area and returns its bottom y.
func (r *Renderer) drawCalendar(dc *gg.Context, area rect, t time.Time) float64 {
	const headerH = 38
	const rowH = 50

	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	firstCol := mondayIndex(first.Weekday())
	daysInMonth := first.AddDate(0, 1, 0).AddDate(0, 0, -1).Day()
	rows := (firstCol + daysInMonth + 6) / 7
	if rows < 1 {
		rows = 1
	}

	colW := area.w / 7

	// Weekday headers.
	for c, name := range ruWeekHeaders {
		cx := area.x + float64(c)*colW + colW/2
		hex := theme.muted
		if c >= 5 { // Sat, Sun
			hex = theme.down
		}
		r.text(dc, name, cx, area.y+18, 0.5, fontBold, 20, hex)
	}

	gridTop := area.y + headerH
	today := -1
	if t.Year() == first.Year() && t.Month() == first.Month() {
		today = t.Day()
	}

	for day := 1; day <= daysInMonth; day++ {
		idx := firstCol + day - 1
		col := idx % 7
		row := idx / 7
		cx := area.x + float64(col)*colW + colW/2
		cy := gridTop + float64(row)*rowH + rowH/2

		hex := theme.text
		if col >= 5 {
			hex = "#E78A8F" // softened weekend red
		}
		if day == today {
			dc.SetHexColor(theme.accent)
			dc.DrawCircle(cx, cy, 22)
			dc.Fill()
			hex = theme.bgTop // dark number on the bright circle
		}
		r.text(dc, strconv.Itoa(day), cx, cy, 0.5, fontBold, 24, hex)
	}

	return gridTop + float64(rows)*rowH + 6
}
