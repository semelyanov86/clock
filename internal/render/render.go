// Package render draws the 800×1280 dashboard background as a JPEG. It draws
// everything except the live clock, which the device overlays as a native layer
// (see NativeClockSlot). A frame counter drives page rotation and the cycling
// of news and quotes.
package render

import (
	"bytes"
	"fmt"
	"image/color"
	"image/jpeg"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/fogleman/gg"

	"github.com/semelyanov86/clock/internal/model"
)

// numPages is the number of base rotating body pages. The optional
// Claude-usage page is appended at render time when its data is present.
const numPages = 3

// maxJPEGBytes is the device limit for a dial background.
const maxJPEGBytes = 500 * 1024

// rect is a drawing area in canvas coordinates.
type rect struct {
	x, y, w, h float64
}

// Renderer draws dashboard frames. It is safe for sequential use by the frame
// loop (faces are cached internally).
type Renderer struct {
	loc   *time.Location
	fonts *fontSet
}

// New creates a renderer. tz is the timezone used to render the date and
// footer timestamp (the clock itself is the device's native layer).
func New(tz string) (*Renderer, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("load timezone %q: %w", tz, err)
	}
	fonts, err := loadFonts()
	if err != nil {
		return nil, err
	}
	return &Renderer{loc: loc, fonts: fonts}, nil
}

// Render draws one frame and returns it encoded as JPEG. frame advances every
// push; it selects the body page and cycles news/quote items.
func (r *Renderer) Render(snap model.Snapshot, frame int) ([]byte, error) {
	dc := gg.NewContext(CanvasW, CanvasH)
	r.drawBackground(dc)

	headerBottom := r.drawHeader(dc, snap, frame)

	body := rect{x: 20, y: headerBottom + 14, w: CanvasW - 40, h: CanvasH - 20 - (headerBottom + 14) - 44}

	// The base pages always rotate; the Claude-usage page joins the rotation
	// only when its data is present (the widget is opt-in).
	pages := []func(rect){
		func(a rect) { r.pagePortfolio(dc, snap, a) },
		func(a rect) { r.pageMarkets(dc, snap, a) },
		func(a rect) { r.pageInfo(dc, snap, frame, a) },
	}
	if snap.Claude.Valid {
		pages = append(pages, func(a rect) { r.pageClaude(dc, snap, a) })
	}

	page := cycleIndex(frame, len(pages))
	pages[page](body)

	r.drawFooter(dc, snap, page, len(pages))
	return encodeJPEG(dc)
}

func (r *Renderer) drawBackground(dc *gg.Context) {
	grad := gg.NewLinearGradient(0, 0, 0, CanvasH)
	grad.AddColorStop(0, parseHex(theme.bgTop))
	grad.AddColorStop(1, parseHex(theme.bgBottom))
	dc.SetFillStyle(grad)
	dc.DrawRectangle(0, 0, CanvasW, CanvasH)
	dc.Fill()
}

func (r *Renderer) drawFooter(dc *gg.Context, snap model.Snapshot, page, pageCount int) {
	y := float64(CanvasH - 26)
	r.text(dc, "обновлено "+snap.Generated.In(r.loc).Format("15:04:05"), CanvasW-30, y, 1, fontRegular, 18, theme.muted)
	for i := 0; i < pageCount; i++ {
		cx := 32 + float64(i)*22
		if i == page {
			dc.SetHexColor(theme.accent)
		} else {
			dc.SetHexColor(theme.stroke)
		}
		dc.DrawCircle(cx, y, 5)
		dc.Fill()
	}
}

func encodeJPEG(dc *gg.Context) ([]byte, error) {
	img := dc.Image()
	var buf bytes.Buffer
	for _, q := range []int{92, 88, 82, 75, 68, 60} {
		buf.Reset()
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
			return nil, fmt.Errorf("encode jpeg: %w", err)
		}
		if buf.Len() <= maxJPEGBytes {
			return bytes.Clone(buf.Bytes()), nil
		}
	}
	return nil, fmt.Errorf("rendered frame exceeds %d bytes even at low quality (%d)", maxJPEGBytes, buf.Len())
}

func parseHex(s string) color.Color {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return color.Black
	}
	r, _ := strconv.ParseUint(s[0:2], 16, 8)
	g, _ := strconv.ParseUint(s[2:4], 16, 8)
	b, _ := strconv.ParseUint(s[4:6], 16, 8)
	return color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}
}

func roundTemp(c float64) string {
	return strconv.Itoa(int(math.Round(c))) + "°"
}

// cycleIndex maps a frame counter to a valid index in [0, n) for rotation.
func cycleIndex(frame, n int) int {
	if n <= 0 {
		return 0
	}
	return ((frame % n) + n) % n
}
