package render

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fogleman/gg"
	"github.com/semelyanov86/clock/internal/model"
)

// fillPanel draws a rounded panel: a soft top-to-bottom gradient (raised look),
// a 1px inner top highlight, and a hairline border.
func fillPanel(dc *gg.Context, x, y, w, h, radius float64) {
	grad := gg.NewLinearGradient(x, y, x, y+h)
	grad.AddColorStop(0, parseHex(theme.panelTop))
	grad.AddColorStop(1, parseHex(theme.panelBottom))
	dc.SetFillStyle(grad)
	dc.DrawRoundedRectangle(x, y, w, h, radius)
	dc.Fill()

	// Inner top highlight: a faint bright line just inside the top edge.
	dc.SetColor(withAlpha(theme.highlight, 0.5))
	dc.SetLineWidth(1)
	dc.DrawLine(x+radius, y+1, x+w-radius, y+1)
	dc.Stroke()

	dc.SetHexColor(theme.stroke)
	dc.SetLineWidth(1.5)
	dc.DrawRoundedRectangle(x, y, w, h, radius)
	dc.Stroke()
}

// fillPanelEdge draws a panel with a vertical accent bar down its left edge,
// used to make a card the focal point of its page (balance, Brent, …).
func fillPanelEdge(dc *gg.Context, x, y, w, h, radius float64, edgeHex string) {
	fillPanel(dc, x, y, w, h, radius)
	dc.SetHexColor(edgeHex)
	dc.DrawRoundedRectangle(x, y, 7, h, radius)
	dc.Fill()
	// Square off the bar's right side so only the outer corners stay rounded.
	dc.DrawRectangle(x+4, y, 3, h)
	dc.Fill()
}

// chip draws a rounded status pill containing s, anchored horizontally by ax
// (0=left edge at x, 1=right edge at x, 0.5=centred on x) and vertically centred
// on yc. The dark tinted fill plus bright text reads cleanly from across a room.
func (r *Renderer) chip(dc *gg.Context, s string, x, yc, ax, size float64, fgHex string) {
	padX := size * 0.52
	h := size*1.62 + 2
	w := r.measure(dc, s, fontBold, size) + padX*2

	x0 := x
	switch ax {
	case 1:
		x0 = x - w
	case 0.5:
		x0 = x - w/2
	}
	y0 := yc - h/2
	rad := h / 2

	dc.SetHexColor(tintBg(fgHex))
	dc.DrawRoundedRectangle(x0, y0, w, h, rad)
	dc.Fill()
	dc.SetColor(withAlpha(fgHex, 0.45))
	dc.SetLineWidth(1.2)
	dc.DrawRoundedRectangle(x0, y0, w, h, rad)
	dc.Stroke()

	r.text(dc, s, x0+w/2, yc, 0.5, fontBold, size, fgHex)
}

// drawPercentageBar draws a rounded progress bar: a full-width track plus a
// gradient fill whose width is frac (0..1) of the track. The fill brightens
// left-to-right so it reads as energy/level rather than a flat block.
func drawPercentageBar(dc *gg.Context, x, y, w, h, frac float64, fillHex string) {
	switch {
	case frac < 0:
		frac = 0
	case frac > 1:
		frac = 1
	}
	radius := h / 2
	dc.SetHexColor(theme.strokeSoft)
	dc.DrawRoundedRectangle(x, y, w, h, radius)
	dc.Fill()
	dc.SetHexColor(theme.stroke)
	dc.SetLineWidth(1)
	dc.DrawRoundedRectangle(x, y, w, h, radius)
	dc.Stroke()

	fw := w * frac
	if fw <= 0 {
		return
	}
	if fw < h { // keep the rounded cap drawable for tiny fractions
		fw = h
	}
	grad := gg.NewLinearGradient(x, y, x+fw, y)
	grad.AddColorStop(0, parseHex(mixHex(fillHex, "#000000", 0.28)))
	grad.AddColorStop(1, parseHex(fillHex))
	dc.SetFillStyle(grad)
	dc.DrawRoundedRectangle(x, y, fw, h, radius)
	dc.Fill()
}

// usageColor grades a utilization fraction: green when low, amber when
// elevated, red when high.
func usageColor(frac float64) string {
	switch {
	case frac >= 0.85:
		return theme.down
	case frac >= 0.6:
		return theme.accent2
	default:
		return theme.up
	}
}

func formatWind(kmh float64) string {
	if kmh < 0 {
		return "—"
	}
	return strconv.Itoa(int(math.Round(kmh))) + " км/ч"
}

// humanizeUntil renders a short Russian "resets in …" string for a future
// instant relative to now. It returns "" when reset is unknown (zero).
func humanizeUntil(now, reset time.Time) string {
	if reset.IsZero() {
		return ""
	}
	d := reset.Sub(now)
	if d <= 0 {
		return "сброс скоро"
	}
	switch {
	case d >= 24*time.Hour:
		days := int(d / (24 * time.Hour))
		hrs := int((d % (24 * time.Hour)) / time.Hour)
		return fmt.Sprintf("сброс через %dд %dч", days, hrs)
	case d >= time.Hour:
		hrs := int(d / time.Hour)
		mins := int((d % time.Hour) / time.Minute)
		return fmt.Sprintf("сброс через %dч %02dм", hrs, mins)
	default:
		mins := int(d/time.Minute) + 1
		return fmt.Sprintf("сброс через %dм", mins)
	}
}

// text draws s anchored horizontally by ax (0=left, 0.5=center, 1=right) and
// vertically centred on y.
func (r *Renderer) text(dc *gg.Context, s string, x, y, ax float64, kind fontKind, size float64, hex string) {
	dc.SetFontFace(r.fonts.face(kind, size))
	dc.SetHexColor(hex)
	dc.DrawStringAnchored(s, x, y, ax, 0.5)
}

// measure returns the rendered width of s for the given font and size.
func (r *Renderer) measure(dc *gg.Context, s string, kind fontKind, size float64) float64 {
	dc.SetFontFace(r.fonts.face(kind, size))
	w, _ := dc.MeasureString(s)
	return w
}

// fitSize shrinks size (down to min) until s fits within maxW, so a value never
// gets clipped regardless of locale wording. It prefers shrinking over the
// ellipsis truncation of fit, keeping the whole value readable.
func (r *Renderer) fitSize(dc *gg.Context, s string, kind fontKind, size, min, maxW float64) float64 {
	for size > min && r.measure(dc, s, kind, size) > maxW {
		size--
	}
	return size
}

// fit truncates s with an ellipsis so it fits within maxW.
func (r *Renderer) fit(dc *gg.Context, s string, kind fontKind, size, maxW float64) string {
	if r.measure(dc, s, kind, size) <= maxW {
		return s
	}
	const ell = "…"
	for len(s) > 0 {
		_, sz := utf8.DecodeLastRuneInString(s)
		s = strings.TrimRight(s[:len(s)-sz], " ")
		if r.measure(dc, s+ell, kind, size) <= maxW {
			return s + ell
		}
	}
	return ell
}

// humanNumber formats v with space-grouped thousands and the given decimals.
func humanNumber(v float64, decimals int) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := strconv.FormatFloat(v, 'f', decimals, 64)
	intPart, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, frac = s[:i], s[i:]
	}
	var b strings.Builder
	n := len(intPart)
	for i, ch := range intPart {
		if i > 0 && (n-i)%3 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(ch)
	}
	out := b.String() + frac
	if neg {
		out = "−" + out
	}
	return out
}

// formatMoney prefixes the symbol for major currencies and suffixes others.
func formatMoney(v float64, currency string, decimals int) string {
	num := humanNumber(v, decimals)
	switch currency {
	case "€", "$", "¥", "£":
		return currency + num
	case "":
		return num
	default:
		return num + " " + currency
	}
}

// formatPct renders the day percentage with an explicit sign. A move too small
// to survive the rounding is printed unsigned, so a −0.0007% tick reads as
// "0.00%" instead of a fall the digits do not back up.
func formatPct(d model.Delta) string {
	v := d.Pct
	sign := "+"
	if v < 0 {
		sign = "−"
		v = -v
	}
	num := strconv.FormatFloat(v, 'f', 2, 64)
	if num == "0.00" {
		return num + "%"
	}
	return sign + num + "%"
}

// formatSignedAbs renders an absolute change with an explicit sign and symbol.
func formatSignedAbs(d model.Delta, currency string, decimals int) string {
	sign := "+"
	v := d.Abs
	if v < 0 {
		sign = "−"
		v = -v
	}
	return sign + formatMoney(v, currency, decimals)
}

func deltaColor(d model.Delta) string {
	switch d.Direction() {
	case model.Up:
		return theme.up
	case model.Down:
		return theme.down
	default:
		return theme.flat
	}
}

func deltaArrow(d model.Delta) string {
	switch d.Direction() {
	case model.Up:
		return "▲"
	case model.Down:
		return "▼"
	default:
		return "•"
	}
}
