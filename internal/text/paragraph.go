package text

import (
	"math"
	"strings"
	"unicode"

	"github.com/Elagoht/collage-ogimage/internal/dom"
	"github.com/Elagoht/collage-ogimage/internal/layout"
	"golang.org/x/image/font/sfnt"
)

// Glyph is one positioned glyph of a line.
type Glyph struct {
	face  *face
	Index sfnt.GlyphIndex
	// X is the glyph's pen position from the line's start, after alignment.
	X float64
	// Size is the font size it is drawn at, in pixels.
	Size float64
	// Style is the style of the run it belongs to: its colour.
	Style *layout.Style
}

// Line is one laid-out line.
type Line struct {
	// Top is the line box's top from the paragraph's top, and Height its height;
	// Baseline is where its glyphs sit, from the paragraph's top.
	Top, Height, Baseline float64
	// Width is the advance of the line's text, without the spaces at its end.
	Width  float64
	Glyphs []Glyph
	// Text is the line's characters, as laid out: transformed, white space
	// processed, an ellipsis where a clamp put one.
	Text string
}

// Paragraph is a text block laid out at a width.
type Paragraph struct {
	Lines []Line
	// Width is the widest line's; Height the lines' together.
	Width, Height float64
	// Scale is what data-fit multiplied the font sizes by; 1 without it.
	Scale float64
	// overflowed is set when a clamp cut lines off.
	overflowed bool
}

// Measurer measures and lays out text with Fonts, for layout and for the
// painter. It implements layout.Measurer.
type Measurer struct {
	Fonts *Fonts
	// Locale is the page's, for text-transform: "tr" uppercases i as İ.
	Locale string
	// Images reports an image's natural size, when it is known.
	Images func(n *dom.Node) (width, height float64, ok bool)
}

var _ layout.Measurer = (*Measurer)(nil)

// MeasureText implements layout.Measurer.
func (m *Measurer) MeasureText(n *dom.Node, s *layout.Style, maxWidth float64) (float64, float64) {
	p := m.Layout(n, s, maxWidth, false)
	return p.Width, p.Height
}

// ImageSize implements layout.Measurer.
func (m *Measurer) ImageSize(n *dom.Node) (float64, float64, bool) {
	if m.Images == nil {
		return 0, 0, false
	}
	return m.Images(n)
}

// unit is one character of a paragraph, with the style it is drawn in.
type unit struct {
	r     rune
	style *layout.Style
	// forced is a line break: a <br>, or a newline under pre-wrap.
	forced bool
	// Shaped.
	face    *face
	glyph   sfnt.GlyphIndex
	size    float64
	advance float64
}

// Layout lays n's text out at most width wide (math.Inf(1): unbounded). With
// align it applies text-align, which needs the final width; measuring does not.
func (m *Measurer) Layout(n *dom.Node, s *layout.Style, width float64, align bool) *Paragraph {
	units := m.collect(n, s)
	if !n.Fit || math.IsInf(width, 1) || width <= 0 {
		return m.lay(units, s, width, 1, align)
	}
	// data-fit: 2px smaller at a time, down to half the declared size, until
	// the text fits its width and its clamp.
	declared := s.FontSize
	for size := declared; ; size -= 2 {
		scale := size / declared
		p := m.lay(units, s, width, scale, align)
		if size-2 < declared/2 || fits(p, s, width) {
			return p
		}
	}
}

func fits(p *Paragraph, s *layout.Style, width float64) bool {
	for _, ln := range p.Lines {
		if ln.Width > width+0.01 {
			return false
		}
	}
	if s.Clamp > 0 && p.overflowed {
		return false
	}
	return true
}

// collect flattens a text block — its text and the runs inside it — into units,
// each with its run's style, white space processed and text transformed.
func (m *Measurer) collect(n *dom.Node, s *layout.Style) []unit {
	var units []unit
	var walk func(n *dom.Node, s *layout.Style)
	walk = func(n *dom.Node, s *layout.Style) {
		switch n.Kind {
		case dom.Text:
			for _, r := range m.transform(n.Text, s.Transform) {
				units = append(units, unit{r: r, style: s})
			}
		case dom.Break:
			units = append(units, unit{r: '\n', style: s, forced: true})
		default:
			for _, c := range n.Children {
				cs := s
				if c.Kind != dom.Text {
					cs = layout.Compute(c, s, s.RootFont)
				}
				walk(c, cs)
			}
		}
	}
	walk(n, s)
	return collapse(units)
}

func (m *Measurer) transform(t, how string) string {
	turkic := m.Locale == "tr" || m.Locale == "az" || strings.HasPrefix(m.Locale, "tr-") || strings.HasPrefix(m.Locale, "az-")
	switch how {
	case "uppercase":
		if turkic {
			return strings.ToUpperSpecial(unicode.TurkishCase, t)
		}
		return strings.ToUpper(t)
	case "lowercase":
		if turkic {
			return strings.ToLowerSpecial(unicode.TurkishCase, t)
		}
		return strings.ToLower(t)
	}
	return t
}

// collapse applies white-space: under normal and nowrap, tabs and newlines are
// spaces and a run of spaces is one, none at the paragraph's start or end; under
// pre-wrap, spaces are kept and a newline breaks the line.
func collapse(units []unit) []unit {
	out := units[:0:0]
	for _, u := range units {
		pre := u.style.WhiteSpace == "pre-wrap"
		switch {
		case u.forced:
			// A break swallows the collapsible space before it.
			if len(out) > 0 && out[len(out)-1].r == ' ' && out[len(out)-1].style.WhiteSpace != "pre-wrap" {
				out = out[:len(out)-1]
			}
			out = append(out, u)
		case pre && u.r == '\n':
			u.forced = true
			out = append(out, u)
		case pre && u.r == '\t':
			for range 8 {
				out = append(out, unit{r: ' ', style: u.style})
			}
		case pre:
			out = append(out, u)
		case u.r == ' ' || u.r == '\t' || u.r == '\n' || u.r == '\r' || u.r == '\f':
			if len(out) == 0 || out[len(out)-1].r == ' ' || out[len(out)-1].forced {
				continue
			}
			u.r = ' '
			out = append(out, u)
		default:
			out = append(out, u)
		}
	}
	for len(out) > 0 && out[len(out)-1].r == ' ' && out[len(out)-1].style.WhiteSpace != "pre-wrap" {
		out = out[:len(out)-1]
	}
	return out
}

// lay shapes units at scale and breaks them into lines at most width wide.
func (m *Measurer) lay(src []unit, s *layout.Style, width, scale float64, align bool) *Paragraph {
	units := append([]unit(nil), src...)
	for i := range units {
		u := &units[i]
		if u.forced {
			continue
		}
		u.size = u.style.FontSize * scale
		faces := m.Fonts.match(u.style.FontFamily, u.style.FontWeight, u.style.Italic)
		u.face, u.glyph = m.Fonts.glyph(faces, u.r)
		u.advance = m.Fonts.advance(u.face, u.glyph, u.size) + u.style.LetterSpacing
		if i > 0 && units[i-1].face == u.face && units[i-1].size == u.size && !units[i-1].forced {
			units[i-1].advance += m.Fonts.kern(u.face, units[i-1].glyph, u.glyph, u.size)
		}
	}

	wrap := s.WhiteSpace != "nowrap"
	var lines [][]unit
	var cur []unit
	curW := 0.0 // the line's width without its trailing spaces
	pending := 0.0
	flush := func() {
		lines = append(lines, cur)
		cur, curW, pending = nil, 0, 0
	}
	for i := 0; i < len(units); {
		if units[i].forced {
			flush()
			i++
			continue
		}
		// A word: up to and including the next break opportunity.
		j := i
		for j < len(units) && !units[j].forced {
			j++
			if units[j-1].r == ' ' {
				for j < len(units) && units[j].r == ' ' && !units[j].forced {
					j++
				}
				break
			}
			if isHyphen(units[j-1].r) && j < len(units) && units[j].r != ' ' && j-1 > i {
				break
			}
		}
		word := units[i:j]
		ink, spaces := 0.0, 0.0
		for k, u := range word {
			if u.r == ' ' && allSpaces(word[k:]) {
				spaces += u.advance
			} else {
				ink += u.advance
			}
		}
		if wrap && len(cur) > 0 && curW+pending+ink > width+0.01 {
			flush()
		}
		cur = append(cur, word...)
		if ink > 0 || len(cur) == len(word) {
			curW += pending + ink
			pending = spaces
		} else {
			pending += spaces
		}
		i = j
	}
	if len(cur) > 0 || len(lines) == 0 && len(units) > 0 {
		flush()
	}

	p := &Paragraph{Scale: scale}
	// text-overflow: ellipsis on a line that does not wrap: cut where the box
	// ends, which only the final width — the painter's — knows.
	if align && s.Ellipsis && !wrap && !math.IsInf(width, 1) {
		for i, lu := range lines {
			w := 0.0
			for _, u := range lu {
				w += u.advance
			}
			if w > width+0.01 {
				lines[i] = m.ellipsize(lu, width)
			}
		}
	}
	if s.Clamp > 0 && len(lines) > s.Clamp {
		lines = lines[:s.Clamp]
		lines[len(lines)-1] = m.ellipsize(lines[len(lines)-1], width)
		p.overflowed = true
	}

	y := 0.0
	strutAbove, strutBelow := m.metrics(s, scale)
	for _, lu := range lines {
		above, below := strutAbove, strutBelow
		ln := Line{}
		x := 0.0
		lastInk := 0.0
		var text strings.Builder
		for _, u := range lu {
			if u.forced {
				continue
			}
			text.WriteRune(u.r)
			a, b := m.metrics(u.style, scale)
			above, below = math.Max(above, a), math.Max(below, b)
			ln.Glyphs = append(ln.Glyphs, Glyph{face: u.face, Index: u.glyph, X: x, Size: u.size, Style: u.style})
			x += u.advance
			if u.r != ' ' || u.style.WhiteSpace == "pre-wrap" {
				lastInk = x
			}
		}
		ln.Width = lastInk
		ln.Text = text.String()
		ln.Top, ln.Height, ln.Baseline = y, above+below, y+above
		y += ln.Height
		p.Width = math.Max(p.Width, ln.Width)
		p.Lines = append(p.Lines, ln)
	}
	p.Height = y

	if align && !math.IsInf(width, 1) {
		for i := range p.Lines {
			ln := &p.Lines[i]
			shift := 0.0
			switch s.TextAlign {
			case "center":
				shift = (width - ln.Width) / 2
			case "right", "end":
				shift = width - ln.Width
			}
			for k := range ln.Glyphs {
				ln.Glyphs[k].X += shift
			}
		}
	}
	return p
}

func allSpaces(us []unit) bool {
	for _, u := range us {
		if u.r != ' ' {
			return false
		}
	}
	return true
}

func isHyphen(r rune) bool { return r == '-' || r == '‐' }

// metrics is how far a line of s's text reaches above and below its baseline:
// its primary font's ascent and descent at its size, with the line height's
// leading split between them, as CSS lays out an inline box.
func (m *Measurer) metrics(s *layout.Style, scale float64) (float64, float64) {
	size := s.FontSize * scale
	fc := m.Fonts.match(s.FontFamily, s.FontWeight, s.Italic)[0]
	a, d := fc.ascent*size/fc.upem, fc.descent*size/fc.upem
	lh := s.LinePx()
	if !s.LineHeightPx {
		lh *= scale
	}
	half := (lh - (a + d)) / 2
	return a + half, d + half
}

// ellipsize ends a clamped line with "…", removing characters until it fits.
func (m *Measurer) ellipsize(line []unit, width float64) []unit {
	if len(line) == 0 {
		return line
	}
	last := line[len(line)-1]
	faces := m.Fonts.match(last.style.FontFamily, last.style.FontWeight, last.style.Italic)
	fc, g := m.Fonts.glyph(faces, '…')
	ell := unit{r: '…', style: last.style, face: fc, glyph: g, size: last.size}
	ell.advance = m.Fonts.advance(fc, g, last.size) + last.style.LetterSpacing
	for {
		w := ell.advance
		for _, u := range line {
			w += u.advance
		}
		trailing := len(line) > 0 && line[len(line)-1].r == ' '
		if (w <= width+0.01 || len(line) == 0) && !trailing {
			break
		}
		line = line[:len(line)-1]
	}
	return append(line, ell)
}
