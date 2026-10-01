// Package text lays out and measures a card's text: the fonts a card is drawn in,
// the runs of a text block shaped into glyphs, white space, line breaking, line
// clamping and data-fit (DESIGN.md §6.4, §12.2).
//
// It implements layout.Measurer, and hands the painter the lines it measured
// with: every glyph, its font, its size and its position.
package text

import (
	"fmt"
	"math"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Face is one font file, under the family a card's font-family names it by.
type Face struct {
	Family string
	Weight int
	Italic bool
	Data   []byte
}

// face is a parsed Face.
type face struct {
	family string
	weight int
	italic bool
	font   *sfnt.Font
	// upem is the font's units per em, and ascent and descent its metrics in
	// those units: what a line of it takes.
	upem            float64
	ascent, descent float64
}

// Fonts is the fonts a card can be drawn in: the application's, then the bundled
// Go fonts, which every card falls back to.
type Fonts struct {
	families map[string][]*face
	// fallback is the Go fonts, consulted for a character no named family has.
	fallback []*face

	// mu guards buf, which every lookup into a font needs: cards are drawn
	// concurrently, and share the fonts.
	mu  sync.Mutex
	buf sfnt.Buffer
}

// Bundled are the Go fonts, under the families "Go" and "Go Mono" — what
// sans-serif, serif and monospace are drawn in.
var Bundled = []Face{
	{Family: "Go", Weight: 400, Data: goregular.TTF},
	{Family: "Go", Weight: 700, Data: gobold.TTF},
	{Family: "Go", Weight: 400, Italic: true, Data: goitalic.TTF},
	{Family: "Go", Weight: 700, Italic: true, Data: gobolditalic.TTF},
	{Family: "Go Mono", Weight: 400, Data: gomono.TTF},
	{Family: "Go Mono", Weight: 700, Data: gomonobold.TTF},
}

// NewFonts parses faces and the bundled fonts.
func NewFonts(faces []Face) (*Fonts, error) {
	f := &Fonts{families: map[string][]*face{}}
	for _, src := range append(append([]Face(nil), faces...), Bundled...) {
		parsed, err := sfnt.Parse(src.Data)
		if err != nil {
			return nil, fmt.Errorf("text: font %q weight %d: %w", src.Family, src.Weight, err)
		}
		var buf sfnt.Buffer
		upem := float64(parsed.UnitsPerEm())
		m, err := parsed.Metrics(&buf, fixed.Int26_6(parsed.UnitsPerEm())<<6, font.HintingNone)
		if err != nil {
			return nil, fmt.Errorf("text: font %q metrics: %w", src.Family, err)
		}
		fc := &face{
			family: src.Family, weight: src.Weight, italic: src.Italic, font: parsed, upem: upem,
			ascent: float64(m.Ascent) / 64, descent: float64(m.Descent) / 64,
		}
		key := strings.ToLower(src.Family)
		f.families[key] = append(f.families[key], fc)
	}
	f.fallback = f.families["go"]
	return f, nil
}

// generic maps CSS's generic families to the bundled fonts.
var generic = map[string]string{
	"sans-serif": "go", "serif": "go", "system-ui": "go",
	"monospace": "go mono", "ui-monospace": "go mono",
}

// match returns the faces for a font-family list, weight and style, in the
// order a character is looked up in: each family's best face, then the Go
// fonts' best.
func (f *Fonts) match(families []string, weight float64, italic bool) []*face {
	var out []*face
	seen := map[*face]bool{}
	add := func(candidates []*face) {
		if fc := best(candidates, weight, italic); fc != nil && !seen[fc] {
			seen[fc] = true
			out = append(out, fc)
		}
	}
	for _, fam := range families {
		key := strings.ToLower(fam)
		if g, ok := generic[key]; ok {
			key = g
		}
		add(f.families[key])
	}
	add(f.fallback)
	return out
}

// best is CSS's font matching, for weight and style: the requested style first,
// then the nearest weight — heavier ones first for a request above 500, lighter
// ones first below 400, as CSS Fonts 4 §5.2 orders them.
func best(faces []*face, weight float64, italic bool) *face {
	var pick *face
	score := math.Inf(1)
	for _, fc := range faces {
		s := math.Abs(float64(fc.weight) - weight)
		if fc.italic != italic {
			s += 10000
		}
		switch {
		case weight > 500 && float64(fc.weight) < weight,
			weight < 400 && float64(fc.weight) > weight:
			s += 1000
		}
		if s < score {
			pick, score = fc, s
		}
	}
	return pick
}

// glyph returns the face among faces that has r, and r's glyph in it; when none
// has it, the first face's .notdef box.
func (f *Fonts) glyph(faces []*face, r rune) (*face, sfnt.GlyphIndex) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, fc := range faces {
		if i, err := fc.font.GlyphIndex(&f.buf, r); err == nil && i != 0 {
			return fc, i
		}
	}
	return faces[0], 0
}

// advance is a glyph's advance width at size pixels.
func (f *Fonts) advance(fc *face, g sfnt.GlyphIndex, size float64) float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, err := fc.font.GlyphAdvance(&f.buf, g, fixed.Int26_6(fc.upem*64), font.HintingNone)
	if err != nil {
		return 0
	}
	return float64(a) / 64 * size / fc.upem
}

// kern is the kerning between two glyphs of one face at size pixels.
func (f *Fonts) kern(fc *face, a, b sfnt.GlyphIndex, size float64) float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	k, err := fc.font.Kern(&f.buf, a, b, fixed.Int26_6(fc.upem*64), font.HintingNone)
	if err != nil {
		return 0
	}
	return float64(k) / 64 * size / fc.upem
}

// Has reports whether a family is registered: a font-family naming one that is
// not falls back, which the plugin warns about.
func (f *Fonts) Has(family string) bool {
	key := strings.ToLower(family)
	if g, ok := generic[key]; ok {
		key = g
	}
	return len(f.families[key]) > 0
}

// Outline returns a glyph's outline segments at size pixels, for the painter.
func (f *Fonts) Outline(g Glyph, size float64) (sfnt.Segments, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return g.face.font.LoadGlyph(&f.buf, g.Index, fixed.Int26_6(size*64), nil)
}
