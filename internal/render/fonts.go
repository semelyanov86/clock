package render

import (
	"embed"
	"fmt"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

//go:embed fonts/*.ttf
var fontFS embed.FS

// fontKind selects one of the embedded typefaces.
type fontKind int

const (
	fontRegular fontKind = iota // body text, Cyrillic-capable
	fontBold                    // headings and large numbers
	fontMono                    // tabular figures (prices, balances)
)

// fontSet parses the embedded fonts once and caches sized faces. The embedded
// fonts travel inside the binary, so no system fonts are required at runtime.
type fontSet struct {
	parsed map[fontKind]*opentype.Font

	mu    sync.Mutex
	cache map[string]font.Face
}

func loadFonts() (*fontSet, error) {
	files := map[fontKind]string{
		fontRegular: "fonts/DejaVuSans.ttf",
		fontBold:    "fonts/DejaVuSans-Bold.ttf",
		fontMono:    "fonts/DejaVuSansMono-Bold.ttf",
	}
	parsed := make(map[fontKind]*opentype.Font, len(files))
	for kind, name := range files {
		b, err := fontFS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read embedded font %s: %w", name, err)
		}
		f, err := opentype.Parse(b)
		if err != nil {
			return nil, fmt.Errorf("parse font %s: %w", name, err)
		}
		parsed[kind] = f
	}
	return &fontSet{parsed: parsed, cache: make(map[string]font.Face)}, nil
}

// face returns a cached face for the given kind and pixel size (DPI 72 makes
// the point size equal to the pixel height).
func (fs *fontSet) face(kind fontKind, size float64) font.Face {
	key := fmt.Sprintf("%d:%.1f", kind, size)

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if f, ok := fs.cache[key]; ok {
		return f
	}
	f, err := opentype.NewFace(fs.parsed[kind], &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		// Fall back to the regular face at a safe size; should not happen.
		f, _ = opentype.NewFace(fs.parsed[fontRegular], &opentype.FaceOptions{Size: 24, DPI: 72, Hinting: font.HintingFull})
	}
	fs.cache[key] = f
	return f
}
