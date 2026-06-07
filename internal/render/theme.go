package render

// Canvas dimensions required by the device (exactly 800×1280 portrait).
const (
	CanvasW = 800
	CanvasH = 1280
)

// palette is the dark colour scheme. Up/down drive the green/red deltas.
type palette struct {
	bgTop, bgBottom string
	panel           string
	stroke          string
	text, muted     string
	accent, accent2 string
	up, down, flat  string
}

var theme = palette{
	bgTop:    "#0B0E14",
	bgBottom: "#10141D",
	panel:    "#161B26",
	stroke:   "#28303F",
	text:     "#E6EDF3",
	muted:    "#8A97A8",
	accent:   "#38BDF8",
	accent2:  "#FBBF24",
	up:       "#27C281",
	down:     "#F2545B",
	flat:     "#9AA6B6",
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
