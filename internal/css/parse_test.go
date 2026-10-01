package css

import (
	"reflect"
	"strings"
	"testing"
)

func px(n float64) Length  { return Length{n, Px} }
func pct(n float64) Length { return Length{n, Percent} }

var auto = Length{Unit: Auto}

func one(t *testing.T, style string) Value {
	t.Helper()
	decls, errs := Parse(style)
	if len(errs) > 0 {
		t.Fatalf("Parse(%q): %v", style, errs)
	}
	if len(decls) != 1 {
		t.Fatalf("Parse(%q) = %d declarations", style, len(decls))
	}
	return decls[0].Value
}

// Every property of DESIGN.md §6, in each form it is documented to take.
func TestEverySupportedPropertyParses(t *testing.T) {
	for style, want := range map[string]Value{
		// §6.2 layout
		"display: flex":                  Keyword("flex"),
		"display: none":                  Keyword("none"),
		"display: -webkit-box":           Keyword("-webkit-box"),
		"flex-direction: column":         Keyword("column"),
		"flex-wrap: wrap":                Keyword("wrap"),
		"justify-content: space-between": Keyword("space-between"),
		"justify-content: space-evenly":  Keyword("space-evenly"),
		"align-items: center":            Keyword("center"),
		"align-self: stretch":            Keyword("stretch"),
		"flex-grow: 1":                   Number(1),
		"flex-shrink: 0":                 Number(0),
		"flex-basis: 50%":                pct(50),
		"flex-basis: auto":               auto,
		"flex: 1":                        Flex{1, 1, px(0)},
		"flex: 2 0 120px":                Flex{2, 0, px(120)},
		"flex: none":                     Flex{0, 0, auto},
		"gap: 16px":                      Edges{px(16), px(16)},
		"gap: 8px 24px":                  Edges{px(8), px(24)},
		"row-gap: 1em":                   Length{1, Em},
		"column-gap: 2rem":               Length{2, Rem},
		"width: 1200px":                  px(1200),
		"height: auto":                   auto,
		"min-width: 0":                   px(0),
		"max-width: none":                auto,
		"max-height: 80%":                pct(80),
		"padding: 72px":                  Edges{px(72), px(72), px(72), px(72)},
		"padding: 10px 20px":             Edges{px(10), px(20), px(10), px(20)},
		"padding: 1px 2px 3px":           Edges{px(1), px(2), px(3), px(2)},
		"padding: 1px 2px 3px 4px":       Edges{px(1), px(2), px(3), px(4)},
		"padding-left: 5%":               pct(5),
		"margin: 0 auto":                 Edges{px(0), auto, px(0), auto},
		"margin-top: -8px":               px(-8),
		"box-sizing: border-box":         Keyword("border-box"),
		"-webkit-box-orient: vertical":   Keyword("vertical"),
		// §6.3 painting
		"background: #0f172a":                 Background{Color: Color{15, 23, 42, 255}, HasColor: true},
		"background-color: rgba(0, 0, 0, .5)": Color{0, 0, 0, 128},
		"background-size: cover":              Keyword("cover"),
		"color: white":                        Color{255, 255, 255, 255},
		"color: #fff8":                        Color{255, 255, 255, 136},
		"color: #11223344":                    Color{0x11, 0x22, 0x33, 0x44},
		"color: rgb(255 0 0 / 50%)":           Color{255, 0, 0, 128},
		"color: hsl(120, 100%, 25%)":          Color{0, 128, 0, 255},
		"color: hsla(0deg 0% 100% / 1)":       Color{255, 255, 255, 255},
		"color: transparent":                  Color{},
		"color: RebeccaPurple":                Color{102, 51, 153, 255},
		"border: 2px solid #000":              Border{Width: px(2), Style: "solid", Color: Color{0, 0, 0, 255}, HasColor: true},
		"border: solid 1px":                   Border{Width: px(1), Style: "solid"},
		"border: none":                        Border{Style: "none"},
		"border-width: 3px":                   px(3),
		"border-color: red":                   Color{255, 0, 0, 255},
		"border-style: solid":                 Keyword("solid"),
		"border-radius: 12px":                 Radii{px(12), px(12), px(12), px(12)},
		"border-radius: 50%":                  Radii{pct(50), pct(50), pct(50), pct(50)},
		"border-radius: 4px 8px":              Radii{px(4), px(8), px(4), px(8)},
		"opacity: .4":                         Number(.4),
		"opacity: 40%":                        Number(.4),
		"overflow: hidden":                    Keyword("hidden"),
		// §6.4 text
		"font-family: Outfit":                  FontFamily{"outfit"},
		`font-family: "Open Sans", sans-serif`: FontFamily{"Open Sans", "sans-serif"},
		"font-family: Fira Code, monospace":    FontFamily{"fira code", "monospace"},
		"font-size: 72px":                      px(72),
		"font-size: 1.5em":                     Length{1.5, Em},
		"font-weight: 800":                     Number(800),
		"font-weight: bold":                    Number(700),
		"font-weight: normal":                  Number(400),
		"font-style: italic":                   Keyword("italic"),
		"line-height: 1.1":                     Number(1.1),
		"line-height: 40px":                    px(40),
		"line-height: normal":                  Number(1.2),
		"letter-spacing: -0.02em":              Length{-0.02, Em},
		"letter-spacing: normal":               px(0),
		"text-align: center":                   Keyword("center"),
		"text-transform: uppercase":            Keyword("uppercase"),
		"white-space: nowrap":                  Keyword("nowrap"),
		"-webkit-line-clamp: 3":                Number(3),
		"text-overflow: ellipsis":              Keyword("ellipsis"),
		// §6.5 images
		"object-fit: cover":                    Keyword("cover"),
		"object-position: center":              Keyword("center"),
		"background-image: url(/static/a.png)": Background{Image: "/static/a.png"},
	} {
		if got := one(t, style); !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %#v, want %#v", style, got, want)
		}
	}
}

func TestGradients(t *testing.T) {
	for style, want := range map[string]Gradient{
		"background: linear-gradient(#000, #fff)": {Angle: 180, Stops: []Stop{
			{Color: Color{0, 0, 0, 255}}, {Color: Color{255, 255, 255, 255}}}},
		"background: linear-gradient(135deg, red 0%, blue 100%)": {Angle: 135, Stops: []Stop{
			{Color: Color{255, 0, 0, 255}, At: pct(0), HasAt: true}, {Color: Color{0, 0, 255, 255}, At: pct(100), HasAt: true}}},
		"background: linear-gradient(to right, red, blue)": {Angle: 90, Stops: []Stop{
			{Color: Color{255, 0, 0, 255}}, {Color: Color{0, 0, 255, 255}}}},
		"background: linear-gradient(0.25turn, red, blue)": {Angle: 90, Stops: []Stop{
			{Color: Color{255, 0, 0, 255}}, {Color: Color{0, 0, 255, 255}}}},
		"background: linear-gradient(to left top, red, blue)": {Corner: "top left", Angle: 180, Stops: []Stop{
			{Color: Color{255, 0, 0, 255}}, {Color: Color{0, 0, 255, 255}}}},
	} {
		got := one(t, style).(Background)
		if got.Gradient == nil || !reflect.DeepEqual(*got.Gradient, want) {
			t.Errorf("%s = %+v, want %+v", style, got.Gradient, want)
		}
	}
	got := one(t, "background: #fff linear-gradient(red, blue)").(Background)
	if !got.HasColor || got.Gradient == nil {
		t.Errorf("a colour beneath a gradient: %+v", got)
	}
}

// What the renderer does not draw is refused, with a message that says what is
// wrong — and, where there is one, what to write instead.
func TestUnsupportedIsRefusedWithAReason(t *testing.T) {
	for style, wantInMsg := range map[string]string{
		"position: absolute":                      "flexbox",
		"grid-template-columns: 1fr 1fr":          "flexbox",
		"box-shadow: 0 0 4px #000":                "v0.1",
		"line-clamp: 3":                           "-webkit-box",
		"font: 12px serif":                        "font-family",
		"--brand: red":                            "custom properties",
		"transform: rotate(4deg)":                 "v0.1",
		"display: block":                          "flex, none, -webkit-box",
		"display: grid":                           "flex, none, -webkit-box",
		"width: 12":                               "write 12px",
		"width: 3vw":                              "px, %, em or rem",
		"padding: -4px":                           "negative",
		"color: currentColor":                     "name the colour",
		"color: #12345":                           "hex digits",
		"color: notacolour":                       "not a colour",
		"color: lab(50% 20 30)":                   "colour function",
		"border: 1px red":                         "add solid",
		"border: 2px dashed red":                  "use solid",
		"border-radius: 10px / 20px":              "elliptical",
		"background: radial-gradient(red, blue)":  "linear-gradient",
		"background: linear-gradient(red)":        "two colours",
		"background: url(a.png) no-repeat center": "not supported",
		"font-weight: bolder":                     "relative",
		"object-position: top left":               "center",
		"-webkit-line-clamp: 0":                   "out of range",
		"text-align: justify":                     "left, center, right",
		"flex-wrap: wrap-reverse":                 "nowrap, wrap",
		"width":                                   "property: value",
		"height:":                                 "no value",
		"color: 'red":                             "not closed",
	} {
		_, errs := Parse(style)
		if len(errs) != 1 {
			t.Errorf("%s: %d errors, want 1: %v", style, len(errs), errs)
			continue
		}
		if !strings.Contains(errs[0].Error(), wantInMsg) {
			t.Errorf("%s: %q does not say %q", style, errs[0].Error(), wantInMsg)
		}
	}
}

func TestDeclarationsAndOffsets(t *testing.T) {
	style := "display:flex;  gap: 8px ; background: url('a;b.png'); COLOR: Red !important"
	decls, errs := Parse(style)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	var names []string
	for _, d := range decls {
		names = append(names, d.Property)
		if !strings.HasPrefix(strings.ToLower(style[d.Offset:]), d.Property) {
			t.Errorf("%s: offset %d points at %q", d.Property, d.Offset, style[d.Offset:])
		}
	}
	if strings.Join(names, ",") != "display,gap,background,color" {
		t.Errorf("properties %v", names)
	}
	if decls[2].Value.(Background).Image != "a;b.png" {
		t.Errorf("a semicolon inside quotes split the declaration: %+v", decls[2].Value)
	}
}

func TestAllErrorsAreReportedTogether(t *testing.T) {
	_, errs := Parse("position: absolute; color: nope; display: flex; float: left")
	if len(errs) != 3 {
		t.Fatalf("%d errors, want 3: %v", len(errs), errs)
	}
}

// A value holding a template action is checked once the template runs; its
// property is checked now.
func TestMaskedValuesAreDynamic(t *testing.T) {
	m := string([]rune{Masked, Masked, Masked})
	decls, errs := Parse("color: " + m + "; position: " + m)
	if len(decls) != 1 || !decls[0].Dynamic || decls[0].Value != nil {
		t.Fatalf("decls = %+v", decls)
	}
	if len(errs) != 1 || errs[0].Property != "position" {
		t.Errorf("an unsupported property with a dynamic value was not refused: %v", errs)
	}
	if v, err := ParseValue("color", "#000"); err != nil || v != (Color{0, 0, 0, 255}) {
		t.Errorf("ParseValue = %v, %v", v, err)
	}
}
