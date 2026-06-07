package render

// Canvas dimensions required by the device (exactly 800×1280 portrait).
const (
	CanvasW = 800
	CanvasH = 1280
)

// palette is the dark "finance HUD" colour scheme. It is tuned for a physical
// device read across a room: deep backgrounds, layered panels, and high-chroma
// status colours so up/down deltas register at a glance.
type palette struct {
	// Background layers (top → mid → bottom of the vertical gradient) plus the
	// faint accent glow painted under the header.
	bgTop, bgMid, bgBottom string
	glow                   string

	// Panels: a subtle vertical gradient (top lighter than bottom) reads as a
	// raised surface; highlight is the 1px inner top edge.
	panelTop, panelBottom string
	highlight             string
	stroke, strokeSoft    string

	// Text tiers, from primary to the dimmest captions.
	text, muted, faint string

	// Accents: cyan is primary, amber secondary.
	accent, accent2 string

	// Status colours for day-over-day deltas and gauges.
	up, down, flat string
}

var theme = palette{
	bgTop:    "#0B0F17",
	bgMid:    "#0A0D14",
	bgBottom: "#06080C",
	glow:     "#103A4E",

	panelTop:    "#1A212F",
	panelBottom: "#121724",
	highlight:   "#334055",
	stroke:      "#2B3445",
	strokeSoft:  "#1B2230",

	text:  "#EBF2F9",
	muted: "#9DAABE",
	faint: "#697789",

	accent:  "#3FC9F4",
	accent2: "#FBBF24",

	up:   "#36D399",
	down: "#FB6E76",
	flat: "#9AA6B6",
}

// ClockSlot is the geometry of the native clock layer (disp 4) the device draws
// on top of the rendered background, in the 800×1280 canvas. The renderer keeps
// this region clear so the native digits stay legible.
type ClockSlot struct {
	X, Y, W, H, Size int
}

// NativeClockSlot returns the geometry the application uses to build the native
// clock ItemList entry. It matches the header layout in header.go.
func NativeClockSlot() ClockSlot {
	return ClockSlot{X: 40, Y: 34, W: 430, H: 150, Size: 128}
}
