package css

import (
	"fmt"
	"math"
	"strings"
)

// Declaration is one property: value pair of a style attribute.
type Declaration struct {
	// Property is the property's name, lower-cased.
	Property string
	// Value is the parsed value; nil when Dynamic.
	Value Value
	// Dynamic is set when the value holds a Masked template action: the property
	// is known, its value is not yet.
	Dynamic bool
	// Offset is the byte offset of the property's name in the style string.
	Offset int
}

// Error is one declaration the renderer cannot draw.
type Error struct {
	// Offset is the byte offset in the style string of the declaration at fault.
	Offset int
	// Property is its name, lower-cased; empty when the declaration has none.
	Property string
	// Msg says what is wrong, and what to write instead where there is an
	// answer.
	Msg string
}

func (e Error) Error() string {
	if e.Property == "" {
		return e.Msg
	}
	return e.Property + ": " + e.Msg
}

// Parse reads a style attribute's declarations. It returns every declaration it
// could read and an error for each it could not, so one template's problems are
// reported together.
func Parse(style string) ([]Declaration, []Error) {
	var decls []Declaration
	var errs []Error
	for _, part := range splitDeclarations(style) {
		text := part.text
		lead := len(text) - len(strings.TrimLeft(text, " \t\r\n\f"))
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		at := part.offset + lead
		colon := strings.IndexByte(text, ':')
		if colon < 0 {
			errs = append(errs, Error{Offset: at, Msg: fmt.Sprintf("%q is not a declaration: write property: value", text)})
			continue
		}
		name := strings.ToLower(strings.TrimSpace(text[:colon]))
		raw := strings.TrimSpace(text[colon+1:])
		if strings.HasSuffix(strings.ToLower(raw), "!important") {
			// An inline style is the only style a card has; !important changes
			// nothing, and is accepted so copied CSS does not fail on it.
			raw = strings.TrimSpace(raw[:len(raw)-len("!important")])
		}
		parse, ok := properties[name]
		if !ok {
			errs = append(errs, Error{Offset: at, Property: name, Msg: unsupported(name)})
			continue
		}
		if raw == "" {
			errs = append(errs, Error{Offset: at, Property: name, Msg: "has no value"})
			continue
		}
		if strings.ContainsRune(raw, Masked) {
			decls = append(decls, Declaration{Property: name, Dynamic: true, Offset: at})
			continue
		}
		toks, err := tokenize(raw)
		if err != nil {
			errs = append(errs, Error{Offset: at, Property: name, Msg: err.Error()})
			continue
		}
		v, err := parse(toks)
		if err != nil {
			errs = append(errs, Error{Offset: at, Property: name, Msg: err.Error()})
			continue
		}
		decls = append(decls, Declaration{Property: name, Value: v, Offset: at})
	}
	return decls, errs
}

// ParseValue parses one value for a property, as Parse would. It is how a
// declaration that was Dynamic is checked once its template has run.
func ParseValue(property, raw string) (Value, error) {
	parse, ok := properties[property]
	if !ok {
		return nil, fmt.Errorf("%s: %s", property, unsupported(property))
	}
	toks, err := tokenize(raw)
	if err != nil {
		return nil, err
	}
	return parse(toks)
}

// Supported reports whether property is one the renderer draws.
func Supported(property string) bool {
	_, ok := properties[property]
	return ok
}

type part struct {
	text   string
	offset int
}

// splitDeclarations splits a style at the semicolons outside parentheses and
// quotes.
func splitDeclarations(s string) []part {
	var parts []part
	start, depth := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ';' && depth == 0:
			parts = append(parts, part{s[start:i], start})
			start = i + 1
		}
	}
	return append(parts, part{s[start:], start})
}

// unsupported is the message for a property the renderer does not draw, with
// what to do instead where there is something.
func unsupported(name string) string {
	switch name {
	case "position", "top", "right", "bottom", "left", "inset", "z-index":
		return "is not supported: there is no positioning; place boxes with flexbox — justify-content, align-items, margin: auto"
	case "grid", "grid-template", "grid-template-columns", "grid-template-rows", "grid-area", "grid-column", "grid-row":
		return "is not supported: lay out with flexbox"
	case "float", "clear":
		return "is not supported: lay out with flexbox"
	case "line-clamp":
		return "is not supported: browsers clamp only with display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: N; overflow: hidden"
	case "box-shadow", "text-shadow", "filter", "backdrop-filter", "transform", "transition", "animation", "mix-blend-mode", "clip-path", "mask":
		return "is not supported in v0.1"
	case "font":
		return "the font shorthand is not supported: write font-family, font-size, font-weight and font-style"
	case "flex-flow":
		return "is not supported: write flex-direction and flex-wrap"
	case "background-repeat", "background-position", "background-attachment":
		return "is not supported: a background image fills its box, with background-size: cover or contain"
	}
	if strings.HasPrefix(name, "--") {
		return "custom properties are not supported: a card has no stylesheet to define them in"
	}
	return "is not a property the renderer draws"
}

// parser reads a value's tokens.
type parser func([]token) (Value, error)

var properties map[string]parser

func init() {
	length := lengthParser(lengthOpts{percent: true})
	sizing := lengthParser(lengthOpts{percent: true, auto: true})
	limit := lengthParser(lengthOpts{percent: true, none: true})
	gapLength := lengthParser(lengthOpts{percent: true})
	properties = map[string]parser{
		// Layout.
		"display":            keywords("flex", "none", "-webkit-box"),
		"flex-direction":     keywords("row", "column"),
		"flex-wrap":          keywords("nowrap", "wrap"),
		"justify-content":    keywords("flex-start", "flex-end", "center", "space-between", "space-around", "space-evenly", "start", "end"),
		"align-items":        keywords("flex-start", "flex-end", "center", "stretch", "start", "end"),
		"align-self":         keywords("auto", "flex-start", "flex-end", "center", "stretch", "start", "end"),
		"flex-grow":          numberParser(0, math.Inf(1)),
		"flex-shrink":        numberParser(0, math.Inf(1)),
		"flex-basis":         sizing,
		"flex":               parseFlex,
		"gap":                pairParser(gapLength),
		"row-gap":            gapLength,
		"column-gap":         gapLength,
		"width":              sizing,
		"height":             sizing,
		"min-width":          length,
		"min-height":         length,
		"max-width":          limit,
		"max-height":         limit,
		"padding":            edgesParser(lengthOpts{percent: true}),
		"margin":             edgesParser(lengthOpts{percent: true, auto: true, negative: true}),
		"box-sizing":         keywords("border-box"),
		"-webkit-box-orient": keywords("vertical"),

		// Painting.
		"background":       parseBackground,
		"background-color": colorParser,
		"background-image": parseBackgroundImage,
		"background-size":  keywords("cover", "contain"),
		"color":            colorParser,
		"border":           parseBorder,
		"border-width":     lengthParser(lengthOpts{}),
		"border-color":     colorParser,
		"border-style":     keywords("solid", "none"),
		"border-radius":    parseRadii,
		"opacity":          parseOpacity,
		"overflow":         keywords("visible", "hidden"),

		// Text.
		"font-family":        parseFontFamily,
		"font-size":          lengthParser(lengthOpts{percent: true}),
		"font-weight":        parseFontWeight,
		"font-style":         keywords("normal", "italic"),
		"line-height":        parseLineHeight,
		"letter-spacing":     parseLetterSpacing,
		"text-align":         keywords("left", "center", "right", "start", "end"),
		"text-transform":     keywords("none", "uppercase", "lowercase"),
		"white-space":        keywords("normal", "nowrap", "pre-wrap"),
		"-webkit-line-clamp": numberParser(1, 1000),
		"text-overflow":      keywords("clip", "ellipsis"),

		// Images.
		"object-fit":      keywords("cover", "contain", "fill"),
		"object-position": parseObjectPosition,
	}
	for _, side := range []string{"top", "right", "bottom", "left"} {
		properties["padding-"+side] = lengthParser(lengthOpts{percent: true})
		properties["margin-"+side] = lengthParser(lengthOpts{percent: true, auto: true, negative: true})
	}
}

func single(toks []token) (token, error) {
	if len(toks) != 1 {
		return token{}, fmt.Errorf("takes one value")
	}
	return toks[0], nil
}

func keywords(allowed ...string) parser {
	return func(toks []token) (Value, error) {
		t, err := single(toks)
		if err != nil || t.kind != ident {
			return nil, fmt.Errorf("is one of %s", strings.Join(allowed, ", "))
		}
		for _, a := range allowed {
			if t.text == a {
				return Keyword(t.text), nil
			}
		}
		return nil, fmt.Errorf("%q is not supported: use one of %s", t.text, strings.Join(allowed, ", "))
	}
}

func numberParser(lo, hi float64) parser {
	return func(toks []token) (Value, error) {
		t, err := single(toks)
		if err != nil || t.kind != number || t.unit != "" {
			return nil, fmt.Errorf("is a number")
		}
		if t.n < lo || t.n > hi {
			return nil, fmt.Errorf("%g is out of range", t.n)
		}
		return Number(t.n), nil
	}
}

type lengthOpts struct {
	percent, auto, none, negative bool
}

func lengthParser(o lengthOpts) parser {
	return func(toks []token) (Value, error) {
		t, err := single(toks)
		if err != nil {
			return nil, err
		}
		return toLength(t, o)
	}
}

func toLength(t token, o lengthOpts) (Length, error) {
	if t.kind == ident {
		switch {
		case t.text == "auto" && o.auto, t.text == "none" && o.none:
			return Length{Unit: Auto}, nil
		}
		return Length{}, fmt.Errorf("%q is not a length", t.text)
	}
	if t.kind != number {
		return Length{}, fmt.Errorf("this is not a length")
	}
	if t.n < 0 && !o.negative {
		return Length{}, fmt.Errorf("%g is negative", t.n)
	}
	switch t.unit {
	case "px":
		return Length{t.n, Px}, nil
	case "em":
		return Length{t.n, Em}, nil
	case "rem":
		return Length{t.n, Rem}, nil
	case "%":
		if !o.percent {
			return Length{}, fmt.Errorf("a percentage is not supported here")
		}
		return Length{t.n, Percent}, nil
	case "":
		if t.n == 0 {
			return Length{0, Px}, nil
		}
		return Length{}, fmt.Errorf("%g has no unit: write %gpx", t.n, t.n)
	}
	return Length{}, fmt.Errorf("the unit %q is not supported: use px, %%, em or rem", t.unit)
}

func edgesParser(o lengthOpts) parser {
	return func(toks []token) (Value, error) {
		if len(toks) < 1 || len(toks) > 4 {
			return nil, fmt.Errorf("takes one to four lengths")
		}
		var l [4]Length
		for i, t := range toks {
			v, err := toLength(t, o)
			if err != nil {
				return nil, err
			}
			l[i] = v
		}
		switch len(toks) {
		case 1:
			return Edges{l[0], l[0], l[0], l[0]}, nil
		case 2:
			return Edges{l[0], l[1], l[0], l[1]}, nil
		case 3:
			return Edges{l[0], l[1], l[2], l[1]}, nil
		}
		return Edges(l), nil
	}
}

// pairParser reads gap: one length for both axes, or a row gap and a column gap.
// The result is Edges with the row gap first and the column gap second.
func pairParser(each parser) parser {
	return func(toks []token) (Value, error) {
		if len(toks) < 1 || len(toks) > 2 {
			return nil, fmt.Errorf("takes one or two lengths")
		}
		row, err := each(toks[:1])
		if err != nil {
			return nil, err
		}
		col := row
		if len(toks) == 2 {
			if col, err = each(toks[1:]); err != nil {
				return nil, err
			}
		}
		return Edges{row.(Length), col.(Length)}, nil
	}
}

func parseFlex(toks []token) (Value, error) {
	if len(toks) == 1 && toks[0].kind == ident {
		switch toks[0].text {
		case "none":
			return Flex{0, 0, Length{Unit: Auto}}, nil
		case "auto":
			return Flex{1, 1, Length{Unit: Auto}}, nil
		}
		return nil, fmt.Errorf("%q is not supported: use none, auto, or a grow, shrink and basis", toks[0].text)
	}
	if len(toks) < 1 || len(toks) > 3 {
		return nil, fmt.Errorf("takes a grow, an optional shrink and an optional basis")
	}
	// A single number is CSS's "flex: <grow>", with a basis of 0.
	f := Flex{Grow: 0, Shrink: 1, Basis: Length{0, Px}}
	numbers := 0
	for _, t := range toks {
		if t.kind == number && t.unit == "" && numbers < 2 {
			if t.n < 0 {
				return nil, fmt.Errorf("%g is negative", t.n)
			}
			if numbers == 0 {
				f.Grow = Number(t.n)
			} else {
				f.Shrink = Number(t.n)
			}
			numbers++
			continue
		}
		b, err := toLength(t, lengthOpts{percent: true, auto: true})
		if err != nil {
			return nil, err
		}
		f.Basis = b
	}
	return f, nil
}

func colorParser(toks []token) (Value, error) {
	t, err := single(toks)
	if err != nil {
		return nil, err
	}
	return parseColor(t)
}

func parseGradient(t token) (*Gradient, error) {
	if t.text != "linear-gradient" {
		return nil, fmt.Errorf("%s() is not supported: use linear-gradient()", t.text)
	}
	g := &Gradient{Angle: 180}
	args := t.args
	if len(args) > 0 && len(args[0]) > 0 {
		first := args[0]
		switch {
		case first[0].kind == number && first[0].unit != "" && first[0].unit != "%" && first[0].unit != "px" && first[0].unit != "em" && first[0].unit != "rem":
			if len(first) != 1 {
				return nil, fmt.Errorf("linear-gradient(): an angle is one value")
			}
			deg, err := degrees(first[0])
			if err != nil {
				return nil, err
			}
			g.Angle = deg
			args = args[1:]
		case first[0].kind == ident && first[0].text == "to":
			var dirs []string
			for _, d := range first[1:] {
				dirs = append(dirs, d.text)
			}
			if err := g.direction(dirs); err != nil {
				return nil, err
			}
			args = args[1:]
		}
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("linear-gradient() needs at least two colours")
	}
	for _, arg := range args {
		if len(arg) < 1 || len(arg) > 2 {
			return nil, fmt.Errorf("linear-gradient(): a stop is a colour and an optional position")
		}
		c, err := parseColor(arg[0])
		if err != nil {
			return nil, fmt.Errorf("linear-gradient(): %w", err)
		}
		s := Stop{Color: c}
		if len(arg) == 2 {
			at, err := toLength(arg[1], lengthOpts{percent: true, negative: true})
			if err != nil {
				return nil, fmt.Errorf("linear-gradient(): %w", err)
			}
			s.At, s.HasAt = at, true
		}
		g.Stops = append(g.Stops, s)
	}
	return g, nil
}

func degrees(t token) (float64, error) {
	switch t.unit {
	case "deg":
		return t.n, nil
	case "rad":
		return t.n * 180 / math.Pi, nil
	case "turn":
		return t.n * 360, nil
	case "grad":
		return t.n * 0.9, nil
	}
	return 0, fmt.Errorf("the angle unit %q is not supported: use deg, rad, turn or grad", t.unit)
}

func (g *Gradient) direction(dirs []string) error {
	sides := map[string]float64{"top": 0, "right": 90, "bottom": 180, "left": 270}
	switch len(dirs) {
	case 1:
		a, ok := sides[dirs[0]]
		if !ok {
			return fmt.Errorf("linear-gradient(): %q is not a side", dirs[0])
		}
		g.Angle = a
		return nil
	case 2:
		v, h := dirs[0], dirs[1]
		if v == "left" || v == "right" {
			v, h = h, v
		}
		if (v != "top" && v != "bottom") || (h != "left" && h != "right") {
			return fmt.Errorf("linear-gradient(): \"to %s\" is not a corner", strings.Join(dirs, " "))
		}
		g.Corner = v + " " + h
		return nil
	}
	return fmt.Errorf("linear-gradient(): \"to\" is followed by a side or a corner")
}

func parseBackgroundImage(toks []token) (Value, error) {
	t, err := single(toks)
	if err != nil || t.kind != function {
		return nil, fmt.Errorf("is a url() or a linear-gradient()")
	}
	if t.text == "url" {
		return Background{Image: URL(t.args[0][0].text)}, nil
	}
	g, err := parseGradient(t)
	if err != nil {
		return nil, err
	}
	return Background{Gradient: g}, nil
}

// parseBackground reads one layer: a colour, a gradient, or an image — with a
// colour beneath a gradient or an image allowed.
func parseBackground(toks []token) (Value, error) {
	var b Background
	for _, t := range toks {
		switch {
		case t.kind == function && t.text == "url" && b.Image == "" && b.Gradient == nil:
			b.Image = URL(t.args[0][0].text)
		case t.kind == function && strings.HasSuffix(t.text, "gradient") && b.Image == "" && b.Gradient == nil:
			g, err := parseGradient(t)
			if err != nil {
				return nil, err
			}
			b.Gradient = g
		case !b.HasColor:
			c, err := parseColor(t)
			if err != nil {
				return nil, fmt.Errorf("%w; a background is a colour, a linear-gradient() or a url(), and positions and repeats are not supported", err)
			}
			b.Color, b.HasColor = c, true
		default:
			return nil, fmt.Errorf("one layer only: a colour, and a gradient or an image")
		}
	}
	if !b.HasColor && b.Gradient == nil && b.Image == "" {
		return nil, fmt.Errorf("has no value")
	}
	return b, nil
}

func parseBorder(toks []token) (Value, error) {
	if len(toks) == 1 && toks[0].kind == ident && toks[0].text == "none" {
		return Border{Style: "none"}, nil
	}
	b := Border{Width: Length{3, Px}, Style: "none"}
	seenWidth, seenStyle := false, false
	for _, t := range toks {
		switch {
		case t.kind == number && !seenWidth:
			w, err := toLength(t, lengthOpts{})
			if err != nil {
				return nil, err
			}
			b.Width, seenWidth = w, true
		case t.kind == ident && (t.text == "solid" || t.text == "none") && !seenStyle:
			b.Style, seenStyle = Keyword(t.text), true
		case t.kind == ident && (t.text == "dashed" || t.text == "dotted" || t.text == "double" || t.text == "groove" || t.text == "ridge" || t.text == "inset" || t.text == "outset"):
			return nil, fmt.Errorf("%q borders are not supported: use solid", t.text)
		case !b.HasColor:
			c, err := parseColor(t)
			if err != nil {
				return nil, err
			}
			b.Color, b.HasColor = c, true
		default:
			return nil, fmt.Errorf("is a width, a style and a colour")
		}
	}
	if !seenStyle {
		// CSS's initial border-style is none, so "border: 1px red" draws
		// nothing in a browser.
		return nil, fmt.Errorf("has no style, so a browser draws nothing: add solid")
	}
	return b, nil
}

func parseRadii(toks []token) (Value, error) {
	for _, t := range toks {
		if t.kind == slash {
			return nil, fmt.Errorf("elliptical corners (with /) are not supported")
		}
	}
	e, err := edgesParser(lengthOpts{percent: true})(toks)
	if err != nil {
		return nil, err
	}
	// border-radius lists corners clockwise from top-left, which is where the
	// one-to-four rule of the edges puts them.
	return Radii(e.(Edges)), nil
}

func parseOpacity(toks []token) (Value, error) {
	t, err := single(toks)
	if err != nil || t.kind != number || (t.unit != "" && t.unit != "%") {
		return nil, fmt.Errorf("is a number from 0 to 1, or a percentage")
	}
	v := t.n
	if t.unit == "%" {
		v /= 100
	}
	return Number(math.Max(0, math.Min(1, v))), nil
}

func parseFontFamily(toks []token) (Value, error) {
	var families FontFamily
	var name []string
	flush := func() error {
		if len(name) == 0 {
			return fmt.Errorf("an empty family name")
		}
		families = append(families, strings.Join(name, " "))
		name = nil
		return nil
	}
	for _, t := range toks {
		switch t.kind {
		case comma:
			if err := flush(); err != nil {
				return nil, err
			}
		case str, ident:
			name = append(name, t.text)
		default:
			return nil, fmt.Errorf("is a list of family names")
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return families, nil
}

func parseFontWeight(toks []token) (Value, error) {
	t, err := single(toks)
	if err != nil {
		return nil, err
	}
	switch {
	case t.kind == ident && t.text == "normal":
		return Number(400), nil
	case t.kind == ident && t.text == "bold":
		return Number(700), nil
	case t.kind == ident && (t.text == "bolder" || t.text == "lighter"):
		return nil, fmt.Errorf("%q is relative to the parent: write a number", t.text)
	case t.kind == number && t.unit == "" && t.n >= 1 && t.n <= 1000:
		return Number(t.n), nil
	}
	return nil, fmt.Errorf("is normal, bold, or a number from 1 to 1000")
}

func parseLineHeight(toks []token) (Value, error) {
	t, err := single(toks)
	if err != nil {
		return nil, err
	}
	switch {
	case t.kind == ident && t.text == "normal":
		return Number(1.2), nil
	case t.kind == number && t.unit == "":
		if t.n < 0 {
			return nil, fmt.Errorf("%g is negative", t.n)
		}
		return Number(t.n), nil
	}
	return toLength(t, lengthOpts{percent: true})
}

func parseLetterSpacing(toks []token) (Value, error) {
	t, err := single(toks)
	if err != nil {
		return nil, err
	}
	if t.kind == ident && t.text == "normal" {
		return Length{0, Px}, nil
	}
	return toLength(t, lengthOpts{negative: true})
}

func parseObjectPosition(toks []token) (Value, error) {
	for _, t := range toks {
		if t.kind != ident || t.text != "center" {
			return nil, fmt.Errorf("only center is supported in v0.1")
		}
	}
	if len(toks) == 0 || len(toks) > 2 {
		return nil, fmt.Errorf("only center is supported in v0.1")
	}
	return Keyword("center"), nil
}
