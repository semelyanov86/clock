package render

import (
	"math"
	"strconv"
	"time"

	"github.com/fogleman/gg"

	"github.com/semelyanov86/clock/internal/model"
)

// sectionTitle draws a page heading: a vertical accent tab, the title, and a
// hairline rule (accent-tipped) spanning the page width. Returns the y below it.
func (r *Renderer) sectionTitle(dc *gg.Context, area rect, title string) float64 {
	dc.SetHexColor(theme.accent)
	dc.DrawRoundedRectangle(area.x, area.y-4, 7, 32, 3)
	dc.Fill()

	r.text(dc, title, area.x+22, area.y+13, 0, fontBold, 30, theme.text)

	ry := area.y + 40
	dc.SetHexColor(theme.strokeSoft)
	dc.DrawRectangle(area.x, ry, area.w, 2)
	dc.Fill()
	dc.SetHexColor(theme.accent)
	dc.DrawRectangle(area.x, ry, 150, 2)
	dc.Fill()
	return area.y + 60
}

// drawParagraph word-wraps s within maxW and returns the baseline below the
// last drawn line.
func (r *Renderer) drawParagraph(dc *gg.Context, s, hex string, x, topY, maxW, lineH float64, kind fontKind, size float64, maxLines int) float64 {
	dc.SetFontFace(r.fonts.face(kind, size))
	dc.SetHexColor(hex)
	lines := dc.WordWrap(s, maxW)
	base := topY + size*0.9
	for i, ln := range lines {
		if i >= maxLines {
			break
		}
		dc.DrawString(ln, x, base)
		base += lineH
	}
	return base
}

// drawDeltaBlock draws a status chip (arrow + percentage) over the signed
// absolute change, right-aligned at xRight and centred on yc.
func (r *Renderer) drawDeltaBlock(dc *gg.Context, xRight, yc float64, d model.Delta, currency string) {
	col := deltaColor(d)
	r.chip(dc, deltaArrow(d)+" "+formatPct(d), xRight, yc-16, 1, 30, col)
	r.text(dc, formatSignedAbs(d, currency, 2), xRight, yc+30, 1, fontRegular, 22, col)
}

func (r *Renderer) pagePortfolio(dc *gg.Context, snap model.Snapshot, area rect) {
	y := r.sectionTitle(dc, area, "ПОРТФЕЛЬ")
	p := snap.Portfolio

	const totalH = 134
	fillPanelEdge(dc, area.x, y, area.w, totalH, 18, deltaColor(p.TotalDelta))
	r.text(dc, "ОБЩИЙ БАЛАНС", area.x+30, y+36, 0, fontBold, 20, theme.muted)

	// Shrink the balance if needed so it never runs into the delta block on the
	// right (which can be wide for large day changes).
	deltaChip := deltaArrow(p.TotalDelta) + " " + formatPct(p.TotalDelta)
	blockW := math.Max(
		r.measure(dc, deltaChip, fontBold, 30)+30*0.52*2,
		r.measure(dc, formatSignedAbs(p.TotalDelta, p.TotalCurrency, 2), fontRegular, 22),
	)
	balance := formatMoney(p.TotalValue, p.TotalCurrency, 2)
	balSize := r.fitSize(dc, balance, fontBold, 58, 34, area.w-30-blockW-24)
	r.text(dc, balance, area.x+30, y+94, 0, fontBold, balSize, theme.text)
	r.drawDeltaBlock(dc, area.x+area.w-26, y+76, p.TotalDelta, p.TotalCurrency)
	y += totalH + 16

	if len(p.Positions) == 0 {
		r.text(dc, "нет данных", area.x+area.w/2, y+40, 0.5, fontRegular, 24, theme.muted)
		return
	}

	const rowH = 108
	maxRows := int((area.y + area.h - y) / rowH)
	for i, pos := range p.Positions {
		if i >= maxRows {
			break
		}
		ry := y + float64(i)*rowH
		ih := float64(rowH - 12)
		fillPanel(dc, area.x, ry, area.w, ih, 14)

		// Right column: price (top) over a delta chip, both right-aligned and
		// kept apart so the chip never rides up into the price.
		price := formatMoney(pos.Value, pos.Currency, 2)
		priceSize := r.fitSize(dc, price, fontMono, 28, 20, 320)
		r.text(dc, price, area.x+area.w-24, ry+34, 1, fontMono, priceSize, theme.text)
		r.chip(dc, deltaArrow(pos.Delta)+" "+formatPct(pos.Delta), area.x+area.w-24, ry+68, 1, 20, deltaColor(pos.Delta))

		// Left column: symbol over the holding name. The name is capped to the
		// width of the allocation meter below it so the two form one tidy block
		// instead of the name sprawling across the bar.
		r.text(dc, pos.Symbol, area.x+24, ry+34, 0, fontBold, 30, theme.text)
		label := pos.Name
		if pos.Qty > 0 {
			label = strconv.FormatFloat(pos.Qty, 'f', -1, 64) + " × " + pos.Name
		}
		name := r.fit(dc, label, fontRegular, 20, 330)
		r.text(dc, name, area.x+24, ry+62, 0, fontRegular, 20, theme.muted)

		// Allocation meter: this holding's share of the total portfolio value —
		// a glanceable sense of weighting, drawn as an underline along the row.
		if p.TotalValue > 0 {
			frac := pos.Weight
			if frac <= 0 {
				frac = pos.Value / p.TotalValue
			}
			frac = math.Min(frac, 1)
			barY := ry + ih - 16
			barW := 330.0
			dc.SetColor(withAlpha(theme.stroke, 0.7))
			dc.DrawRoundedRectangle(area.x+24, barY, barW, 6, 3)
			dc.Fill()
			fw := barW * frac
			if fw < 6 {
				fw = 6
			}
			dc.SetColor(withAlpha(theme.accent, 0.7))
			dc.DrawRoundedRectangle(area.x+24, barY, fw, 6, 3)
			dc.Fill()
			r.text(dc, strconv.Itoa(int(math.Round(frac*100)))+"%", area.x+24+barW+16, barY+3, 0, fontBold, 18, theme.muted)
		}
	}
}

func (r *Renderer) pageMarkets(dc *gg.Context, snap model.Snapshot, area rect) {
	y := r.sectionTitle(dc, area, "РЫНКИ")

	// ETF 2×2 grid.
	const gap = 16
	tileW := (area.w - gap) / 2
	const etfH = 120
	for i, inst := range snap.ETFs {
		if i >= 4 {
			break
		}
		col := float64(i % 2)
		row := float64(i / 2)
		tx := area.x + col*(tileW+gap)
		ty := y + row*(etfH+gap)
		r.instrumentTile(dc, tx, ty, tileW, etfH, inst, "")
	}
	y += 2*etfH + gap + gap

	// Brent: full-width tile with an amber commodity edge.
	const brentH = 108
	r.instrumentTile(dc, area.x, y, area.w, brentH, snap.Brent, theme.accent2)
	y += brentH + gap

	// FX: three tiles (rates vs RUB).
	r.text(dc, "КУРСЫ К РУБЛЮ", area.x+2, y+14, 0, fontBold, 20, theme.muted)
	y += 30
	fxW := (area.w - 2*gap) / 3
	fxH := area.y + area.h - y
	if fxH > 150 {
		fxH = 150
	}
	for i, fx := range snap.FX {
		if i >= 3 {
			break
		}
		tx := area.x + float64(i)*(fxW+gap)
		r.fxTile(dc, tx, y, fxW, fxH, fx)
	}
}

// instrumentTile draws one quote card. A non-empty edgeHex adds a left accent
// bar (used to flag the commodity row).
func (r *Renderer) instrumentTile(dc *gg.Context, x, y, w, h float64, inst model.Instrument, edgeHex string) {
	pad := 18.0
	if edgeHex != "" {
		fillPanelEdge(dc, x, y, w, h, 16, edgeHex)
		pad = 24
	} else {
		fillPanel(dc, x, y, w, h, 16)
	}
	if inst.Symbol == "" {
		r.text(dc, "—", x+w/2, y+h/2, 0.5, fontRegular, 24, theme.muted)
		return
	}
	r.text(dc, inst.Symbol, x+pad, y+32, 0, fontBold, 26, theme.accent)
	if inst.Name != "" {
		r.text(dc, r.fit(dc, inst.Name, fontRegular, 17, w-pad-16), x+pad, y+58, 0, fontRegular, 17, theme.muted)
	}

	// Price and delta chip share one baseline so the row reads as a single value;
	// the price shrinks to fit whatever room the chip leaves on a narrow tile.
	delta := deltaArrow(inst.Delta) + " " + formatPct(inst.Delta)
	chipW := r.measure(dc, delta, fontBold, 21) + 21*0.52*2
	valY := y + h - 28
	price := formatMoney(inst.Last, inst.Currency, 2)
	priceSize := r.fitSize(dc, price, fontMono, 32, 22, w-pad-18-chipW-14)
	r.text(dc, price, x+pad, valY, 0, fontMono, priceSize, theme.text)
	r.chip(dc, delta, x+w-18, valY, 1, 21, deltaColor(inst.Delta))
}

func (r *Renderer) fxTile(dc *gg.Context, x, y, w, h float64, fx model.Instrument) {
	fillPanel(dc, x, y, w, h, 16)
	if fx.Symbol == "" {
		r.text(dc, "—", x+w/2, y+h/2, 0.5, fontRegular, 24, theme.muted)
		return
	}
	r.text(dc, fx.Symbol, x+16, y+34, 0, fontBold, 24, theme.accent)
	if fx.Name != "" {
		r.text(dc, r.fit(dc, fx.Name, fontRegular, 18, w-28), x+16, y+60, 0, fontRegular, 18, theme.muted)
	}
	// Price over the delta chip, both anchored to the bottom with even padding so
	// the chip can never spill past the rounded panel edge.
	r.text(dc, formatMoney(fx.Last, "₽", 2), x+16, y+h-60, 0, fontMono, 26, theme.text)
	r.chip(dc, deltaArrow(fx.Delta)+" "+formatPct(fx.Delta), x+16, y+h-28, 0, 19, deltaColor(fx.Delta))
}

// pageClaude shows Claude's unified rate-limit usage: the rolling 5-hour block
// and the weekly window, each as a labelled gauge. The page only appears when
// the data is present (see Render).
func (r *Renderer) pageClaude(dc *gg.Context, snap model.Snapshot, area rect) {
	y := r.sectionTitle(dc, area, "ЛИМИТЫ CLAUDE")
	u := snap.Claude
	if !u.Valid {
		r.text(dc, "нет данных", area.x+area.w/2, y+60, 0.5, fontRegular, 24, theme.muted)
		return
	}

	now := snap.Generated.In(r.loc)
	const gaugeH = 250
	y += 14
	r.usageGauge(dc, area.x, y, area.w, gaugeH, "5 ЧАСОВ", u.Block5h, now)
	y += gaugeH + 28
	r.usageGauge(dc, area.x, y, area.w, gaugeH, "НЕДЕЛЯ", u.Weekly, now)
	// The frame footer already carries the "обновлено HH:MM:SS" timestamp, so the
	// gauges do not repeat it here.
}

// usageGauge draws one labelled rate-limit window: a status dot + title, a large
// percentage, a gradient progress bar, and a reset countdown.
func (r *Renderer) usageGauge(dc *gg.Context, x, y, w, h float64, title string, win model.ClaudeWindow, now time.Time) {
	fillPanel(dc, x, y, w, h, 18)
	col := usageColor(win.Utilization)

	dc.SetHexColor(col)
	dc.DrawCircle(x+38, y+46, 9)
	dc.Fill()
	r.text(dc, title, x+60, y+46, 0, fontBold, 28, theme.text)

	pct := strconv.Itoa(int(math.Round(win.Utilization*100))) + "%"
	r.text(dc, pct, x+w-28, y+56, 1, fontBold, 74, col)

	drawPercentageBar(dc, x+30, y+h*0.55, w-60, 30, win.Utilization, col)

	if s := humanizeUntil(now, win.ResetAt); s != "" {
		r.text(dc, s, x+30, y+h-34, 0, fontRegular, 22, theme.muted)
	}
}

func (r *Renderer) pageInfo(dc *gg.Context, snap model.Snapshot, frame int, area rect) {
	t := snap.Generated.In(r.loc)
	y := r.sectionTitle(dc, area, ruMonthNom(t.Month())+" "+strconv.Itoa(t.Year()))

	calBottom := r.drawCalendar(dc, rect{x: area.x, y: y + 4, w: area.w, h: 360}, t)

	// News panel (rotates by frame).
	ny := calBottom + 16
	const newsH = 154
	fillPanelEdge(dc, area.x, ny, area.w, newsH, 16, theme.accent)
	r.text(dc, "НОВОСТИ", area.x+26, ny+30, 0, fontBold, 20, theme.accent)
	if len(snap.News) > 0 {
		item := snap.News[cycleIndex(frame, len(snap.News))]
		r.drawParagraph(dc, item.Title, theme.text, area.x+26, ny+46, area.w-52, 34, fontBold, 26, 3)
	} else {
		r.text(dc, "нет новостей", area.x+26, ny+90, 0, fontRegular, 22, theme.muted)
	}

	// Quote panel fills the remainder, with an oversized decorative mark.
	qy := ny + newsH + 16
	qh := area.y + area.h - qy
	if qh < 90 {
		qh = 90
	}
	fillPanel(dc, area.x, qy, area.w, qh, 16)
	if len(snap.Quotes) > 0 {
		q := snap.Quotes[cycleIndex(frame, len(snap.Quotes))]

		// Oversized opening mark watermark in the top-left corner.
		dc.SetColor(withAlpha(theme.accent, 0.13))
		dc.SetFontFace(r.fonts.face(fontBold, 150))
		dc.DrawStringAnchored("“", area.x+56, qy+70, 0.5, 0.5)

		// Vertically centre the text + author block within the remaining panel.
		const lineH, qSize = 36, 26
		dc.SetFontFace(r.fonts.face(fontRegular, qSize))
		n := len(dc.WordWrap(q.Text, area.w-72))
		if n > 4 {
			n = 4
		}
		blockH := float64(n)*lineH + 44
		startY := qy + (qh-blockH)/2
		if startY < qy+28 {
			startY = qy + 28
		}
		end := r.drawParagraph(dc, q.Text, theme.text, area.x+36, startY, area.w-72, lineH, fontRegular, qSize, 4)
		if q.Author != "" {
			authorY := end + 24
			if limit := qy + qh - 18; authorY > limit {
				authorY = limit
			}
			r.text(dc, "— "+q.Author, area.x+area.w-28, authorY, 1, fontBold, 22, theme.accent2)
		}
	}
}
