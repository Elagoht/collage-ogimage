// Package css parses the subset of CSS a card's inline styles may use into typed
// values, and refuses everything else with a message naming what was wrong.
//
// It knows nothing of elements or layout: whether display:-webkit-box is allowed
// where it appears is the dom package's business. What it decides is whether a
// declaration is one of the properties the renderer draws, written in a form it
// understands.
package css

import "fmt"

// Masked stands in for a template action in a style that has not been executed
// yet. A value holding it is dynamic: its property is checked, its value is not,
// because it is only known once the template runs.
const Masked = ''

// Value is a parsed property value. Its concrete type is fixed per property; see
// the property table in parse.go.
type Value interface{ value() }

// Unit is the unit of a Length.
type Unit uint8

const (
	// Px is CSS pixels, which are the card's pixels.
	Px Unit = iota
	// Percent is a percentage of the containing box, or of the font size where
	// CSS says so.
	Percent
	// Em is the element's own font size, or its parent's for font-size.
	Em
	// Rem is the root element's font size.
	Rem
	// Auto is the keyword auto, where a length may also be auto.
	Auto
)

func (u Unit) String() string {
	switch u {
	case Px:
		return "px"
	case Percent:
		return "%"
	case Em:
		return "em"
	case Rem:
		return "rem"
	default:
		return "auto"
	}
}

// Length is a number with a unit, or auto.
type Length struct {
	N    float64
	Unit Unit
}

func (l Length) String() string {
	if l.Unit == Auto {
		return "auto"
	}
	return fmt.Sprintf("%g%s", l.N, l.Unit)
}

// Number is a unitless number: flex-grow, opacity, a line-height multiplier, a
// line clamp.
type Number float64

// Keyword is an identifier from a property's fixed set, lower-cased.
type Keyword string

// Color is a colour with straight (not premultiplied) alpha.
type Color struct {
	R, G, B, A uint8
}

// Stop is one colour of a gradient, at an optional position along it.
type Stop struct {
	Color Color
	At    Length
	HasAt bool
}

// Gradient is a linear-gradient().
type Gradient struct {
	// Angle is in degrees, clockwise from "to top", as CSS defines it. It is
	// meaningful when Corner is empty.
	Angle float64
	// Corner is a "to" direction naming a corner — "top right", "bottom left" —
	// whose angle depends on the box's shape, and so is resolved when painted.
	Corner string
	Stops  []Stop
}

// URL is a url() or an <img src>: a path, an absolute URL or a data: URL.
type URL string

// Edges is the four sides of padding or margin, in CSS order: top, right,
// bottom, left.
type Edges [4]Length

// Radii is the four corners of border-radius: top-left, top-right, bottom-right,
// bottom-left.
type Radii [4]Length

// Border is the border shorthand.
type Border struct {
	Width Length
	Style Keyword
	Color Color
	// HasColor is false when no colour was given: the border is drawn in the
	// element's color, as CSS's currentcolor.
	HasColor bool
}

// Background is one background layer: a colour, a gradient or an image.
type Background struct {
	Color    Color
	HasColor bool
	Gradient *Gradient
	Image    URL
}

// FontFamily is a font-family list, most preferred first.
type FontFamily []string

// Flex is the flex shorthand.
type Flex struct {
	Grow, Shrink Number
	Basis        Length
}

func (Length) value()     {}
func (Number) value()     {}
func (Keyword) value()    {}
func (Color) value()      {}
func (Gradient) value()   {}
func (URL) value()        {}
func (Edges) value()      {}
func (Radii) value()      {}
func (Border) value()     {}
func (Background) value() {}
func (FontFamily) value() {}
func (Flex) value()       {}
