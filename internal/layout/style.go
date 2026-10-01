// Package layout places a card's boxes: the flexbox subset of DESIGN.md §6.2, over
// the default stylesheet of §6.6.
//
// It is given the dom tree and a Measurer for what it cannot size itself — text,
// and images without a declared size — and returns a tree of boxes with their
// border-box rectangles, in the card's pixels, the root at the origin.
package layout

import (
	"math"

	"github.com/Elagoht/collage-ogimage/internal/css"
	"github.com/Elagoht/collage-ogimage/internal/dom"
)

// Stylesheet is DESIGN.md §6.6 as CSS: what a browser must be given to lay a card
// out as this package does. The fixture recorder injects it into Chrome, and
// compute applies the same defaults; the two are kept in step by the fixtures.
const Stylesheet = `* { margin: 0; padding: 0; box-sizing: border-box; border: 0 solid; }
html, body { width: max-content; }
h1 { font-size: 2em; font-weight: 700; } h2 { font-size: 1.5em; font-weight: 700; }
h3 { font-size: 1.17em; font-weight: 700; } h4 { font-size: 1em; font-weight: 700; }
h5 { font-size: .83em; font-weight: 700; } h6 { font-size: .67em; font-weight: 700; }
strong, b { font-weight: 700; } em, i { font-style: italic; } small { font-size: .83em; }
img { display: block; }
:root { font-family: sans-serif; font-size: 16px; line-height: 1.2; color: #000; }`

// RootFontSize is the root's font size when the card's root declares none, and
// what rem is measured against then.
const RootFontSize = 16

// DefaultWidth and DefaultHeight are a card's size when its root declares none.
const (
	DefaultWidth  = 1200
	DefaultHeight = 630
)

// length is a css.Length with em and rem already turned into pixels: what is left
// to resolve is a percentage, against a containing block known only during layout.
type length struct {
	n       float64
	percent bool
	auto    bool
}

var autoLength = length{auto: true}

// resolve returns l in pixels against base, or NaN when l is auto or a
// percentage of an indefinite base.
func (l length) resolve(base float64) float64 {
	switch {
	case l.auto:
		return math.NaN()
	case l.percent:
		if math.IsNaN(base) {
			return math.NaN()
		}
		return l.n * base / 100
	}
	return l.n
}

// Style is an element's computed style: every property the layout and the
// painter read, inherited and defaulted, its lengths in pixels where CSS lets
// them be computed before layout.
type Style struct {
	// Layout.
	Column     bool
	Wrap       bool
	Justify    string
	AlignItems string
	// AlignSelf is resolved: auto has become the parent's align-items.
	AlignSelf string
	Grow      float64
	Shrink    float64
	Basis     length

	Width, Height       length
	MinWidth, MinHeight length // auto: the automatic minimum of a flex item
	MaxWidth, MaxHeight length // auto: none
	Margin              [4]length
	Padding             [4]length
	Border              float64 // the border's width, the same on every side; 0 without a style
	RowGap, ColumnGap   length
	Overflow            bool // overflow: hidden

	// Text, inherited.
	FontSize      float64
	LineHeight    float64 // a multiplier of FontSize when LineHeightPx is false
	LineHeightPx  bool
	FontFamily    []string
	FontWeight    float64
	Italic        bool
	LetterSpacing float64
	TextAlign     string
	Transform     string
	WhiteSpace    string
	Color         css.Color

	// RootFont is the root element's font size, what rem is measured against:
	// carried so a run's style can be computed from its block's.
	RootFont float64

	// Painting and text, not inherited.
	Background   css.Background
	BorderColor  css.Color
	HasBorderClr bool
	Radii        [4]length
	Opacity      float64
	Clamp        int
	Ellipsis     bool
	ObjectFit    string
}

// rootStyle is the style the card's root inherits from: :root of the
// stylesheet.
func rootStyle() *Style {
	return &Style{
		FontSize:   RootFontSize,
		LineHeight: 1.2,
		FontFamily: []string{"sans-serif"},
		FontWeight: 400,
		TextAlign:  "start",
		Transform:  "none",
		WhiteSpace: "normal",
		Color:      css.Color{A: 255},
	}
}

// Compute is n's style, inheriting from parent; rootFont is the root element's
// computed font size, for rem — zero while computing the root itself. It is how
// the text package styles the runs inside a text block, which have no box.
func Compute(n *dom.Node, parent *Style, rootFont float64) *Style {
	s := &Style{
		// Inherited.
		FontSize: parent.FontSize, LineHeight: parent.LineHeight, LineHeightPx: parent.LineHeightPx,
		FontFamily: parent.FontFamily, FontWeight: parent.FontWeight, Italic: parent.Italic,
		LetterSpacing: parent.LetterSpacing, TextAlign: parent.TextAlign, Transform: parent.Transform,
		WhiteSpace: parent.WhiteSpace, Color: parent.Color, RootFont: parent.RootFont,
		// Initial values of the rest.
		Justify: "flex-start", AlignItems: "stretch", AlignSelf: "auto",
		Shrink: 1, Basis: autoLength,
		Width: autoLength, Height: autoLength, MinWidth: autoLength, MinHeight: autoLength,
		MaxWidth: autoLength, MaxHeight: autoLength, Opacity: 1, ObjectFit: "fill",
	}
	if n.Kind == dom.Text {
		return s
	}

	// The stylesheet's element defaults, which the element's own style overrides.
	switch n.Tag {
	case "h1":
		s.FontSize, s.FontWeight = 2*parent.FontSize, 700
	case "h2":
		s.FontSize, s.FontWeight = 1.5*parent.FontSize, 700
	case "h3":
		s.FontSize, s.FontWeight = 1.17*parent.FontSize, 700
	case "h4":
		s.FontWeight = 700
	case "h5":
		s.FontSize, s.FontWeight = .83*parent.FontSize, 700
	case "h6":
		s.FontSize, s.FontWeight = .67*parent.FontSize, 700
	case "strong", "b":
		s.FontWeight = 700
	case "em", "i":
		s.Italic = true
	case "small":
		s.FontSize = .83 * parent.FontSize
	}

	// font-size first: em in every other property is this element's own.
	if d, ok := n.Last("font-size"); ok && !d.Dynamic {
		l := d.Value.(css.Length)
		switch l.Unit {
		case css.Em, css.Percent:
			if l.Unit == css.Percent {
				l.N /= 100
			}
			s.FontSize = l.N * parent.FontSize
		case css.Rem:
			s.FontSize = l.N * remBase(rootFont)
		default:
			s.FontSize = l.N
		}
	}
	if rootFont == 0 {
		rootFont = s.FontSize
	}
	s.RootFont = rootFont

	px := func(l css.Length) length {
		switch l.Unit {
		case css.Auto:
			return autoLength
		case css.Percent:
			return length{n: l.N, percent: true}
		case css.Em:
			return length{n: l.N * s.FontSize}
		case css.Rem:
			return length{n: l.N * rootFont}
		}
		return length{n: l.N}
	}

	// The stylesheet's "* { border: 0 solid }": a border is solid and has no
	// width until a width is declared, so border-width alone draws one.
	borderStyle, borderWidth := "solid", 0.0
	for _, d := range n.Style {
		if d.Dynamic {
			continue
		}
		switch v := d.Value.(type) {
		case css.Keyword:
			k := string(v)
			switch d.Property {
			case "flex-direction":
				s.Column = k == "column"
			case "flex-wrap":
				s.Wrap = k == "wrap"
			case "justify-content":
				s.Justify = k
			case "align-items":
				s.AlignItems = k
			case "align-self":
				s.AlignSelf = k
			case "overflow":
				s.Overflow = k == "hidden"
			case "border-style":
				borderStyle = k
			case "font-style":
				s.Italic = k == "italic"
			case "text-align":
				s.TextAlign = k
			case "text-transform":
				s.Transform = k
			case "white-space":
				s.WhiteSpace = k
			case "text-overflow":
				s.Ellipsis = k == "ellipsis"
			case "object-fit":
				s.ObjectFit = k
			}
		case css.Number:
			switch d.Property {
			case "flex-grow":
				s.Grow = float64(v)
			case "flex-shrink":
				s.Shrink = float64(v)
			case "font-weight":
				s.FontWeight = float64(v)
			case "line-height":
				s.LineHeight, s.LineHeightPx = float64(v), false
			case "opacity":
				s.Opacity = float64(v)
			case "-webkit-line-clamp":
				s.Clamp = int(v)
			}
		case css.Length:
			l := px(v)
			switch d.Property {
			case "flex-basis":
				s.Basis = l
			case "width":
				s.Width = l
			case "height":
				s.Height = l
			case "min-width":
				s.MinWidth = l
			case "min-height":
				s.MinHeight = l
			case "max-width":
				s.MaxWidth = l
			case "max-height":
				s.MaxHeight = l
			case "row-gap":
				s.RowGap = l
			case "column-gap":
				s.ColumnGap = l
			case "padding-top":
				s.Padding[0] = l
			case "padding-right":
				s.Padding[1] = l
			case "padding-bottom":
				s.Padding[2] = l
			case "padding-left":
				s.Padding[3] = l
			case "margin-top":
				s.Margin[0] = l
			case "margin-right":
				s.Margin[1] = l
			case "margin-bottom":
				s.Margin[2] = l
			case "margin-left":
				s.Margin[3] = l
			case "border-width":
				borderWidth = l.n
			case "line-height":
				if l.percent {
					s.LineHeight, s.LineHeightPx = l.n/100, false
				} else {
					s.LineHeight, s.LineHeightPx = l.n, true
				}
			case "letter-spacing":
				s.LetterSpacing = l.n
			}
		case css.Edges:
			e := [4]length{px(v[0]), px(v[1]), px(v[2]), px(v[3])}
			switch d.Property {
			case "padding":
				s.Padding = e
			case "margin":
				s.Margin = e
			case "gap":
				s.RowGap, s.ColumnGap = e[0], e[1]
			}
		case css.Flex:
			s.Grow, s.Shrink, s.Basis = float64(v.Grow), float64(v.Shrink), px(v.Basis)
		case css.Border:
			borderStyle, borderWidth = string(v.Style), px(v.Width).n
			if v.HasColor {
				s.BorderColor, s.HasBorderClr = v.Color, true
			}
		case css.Color:
			switch d.Property {
			case "color":
				s.Color = v
			case "background-color":
				s.Background.Color, s.Background.HasColor = v, true
			case "border-color":
				s.BorderColor, s.HasBorderClr = v, true
			}
		case css.Background:
			if d.Property == "background-image" {
				s.Background.Gradient, s.Background.Image = v.Gradient, v.Image
			} else {
				s.Background = v
			}
		case css.Radii:
			s.Radii = [4]length{px(v[0]), px(v[1]), px(v[2]), px(v[3])}
		case css.FontFamily:
			s.FontFamily = v
		}
	}
	// A border whose style is none has no width, as in CSS.
	if borderStyle == "solid" {
		s.Border = borderWidth
	}
	if !s.HasBorderClr {
		s.BorderColor = s.Color
	}
	return s
}

// remBase is the root font size for an element whose root's has not been
// computed yet — the root itself, whose rem is the initial font size.
func remBase(rootFont float64) float64 {
	if rootFont == 0 {
		return RootFontSize
	}
	return rootFont
}

// LinePx is the style's line height in pixels, which is what a line of its text
// takes.
func (s *Style) LinePx() float64 {
	if s.LineHeightPx {
		return s.LineHeight
	}
	return s.LineHeight * s.FontSize
}
