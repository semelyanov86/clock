package render

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/fogleman/gg"
	"github.com/semelyanov86/clock/internal/model"
)

// fillPanel draws a rounded panel with a subtle border.
func fillPanel(dc *gg.Context, x, y, w, h, radius float64) {
	dc.SetHexColor(theme.panel)
	dc.DrawRoundedRectangle(x, y, w, h, radius)
	dc.Fill()
	dc.SetHexColor(theme.stroke)
	dc.SetLineWidth(1.5)
	dc.DrawRoundedRectangle(x, y, w, h, radius)
	dc.Stroke()
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

// formatPct renders the day percentage with an explicit sign.
func formatPct(d model.Delta) string {
	sign := "+"
	v := d.Pct
	if v < 0 {
		sign = "−"
		v = -v
	}
	return sign + strconv.FormatFloat(v, 'f', 2, 64) + "%"
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
