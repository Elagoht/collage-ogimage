package css

import (
	"fmt"
	"math"
	"strconv"
)

// parseColor reads one colour token: #rgb, #rgba, #rrggbb, #rrggbbaa, rgb(),
// rgba(), hsl(), hsla(), transparent, or a CSS named colour.
func parseColor(t token) (Color, error) {
	switch t.kind {
	case hash:
		return parseHex(t.text)
	case ident:
		if t.text == "transparent" {
			return Color{}, nil
		}
		if t.text == "currentcolor" {
			return Color{}, fmt.Errorf("currentColor is not supported: name the colour")
		}
		if c, ok := named[t.text]; ok {
			return c, nil
		}
		return Color{}, fmt.Errorf("%q is not a colour", t.text)
	case function:
		switch t.text {
		case "rgb", "rgba":
			return parseRGB(t)
		case "hsl", "hsla":
			return parseHSL(t)
		}
		return Color{}, fmt.Errorf("%s() is not a colour function: use rgb(), rgba(), hsl() or hsla()", t.text)
	}
	return Color{}, fmt.Errorf("this is not a colour")
}

func parseHex(h string) (Color, error) {
	expand := func(c byte) uint8 {
		v, _ := strconv.ParseUint(string([]byte{c, c}), 16, 8)
		return uint8(v)
	}
	pair := func(s string) uint8 {
		v, _ := strconv.ParseUint(s, 16, 8)
		return uint8(v)
	}
	switch len(h) {
	case 3:
		return Color{expand(h[0]), expand(h[1]), expand(h[2]), 255}, nil
	case 4:
		return Color{expand(h[0]), expand(h[1]), expand(h[2]), expand(h[3])}, nil
	case 6:
		return Color{pair(h[0:2]), pair(h[2:4]), pair(h[4:6]), 255}, nil
	case 8:
		return Color{pair(h[0:2]), pair(h[2:4]), pair(h[4:6]), pair(h[6:8])}, nil
	}
	return Color{}, fmt.Errorf("#%s is not a colour: use 3, 4, 6 or 8 hex digits", h)
}

// colorArgs returns a colour function's components, accepting both the comma
// syntax, rgb(1, 2, 3) and rgba(1, 2, 3, .5), and the space syntax,
// rgb(1 2 3 / .5).
func colorArgs(t token) ([]token, token, bool, error) {
	var parts []token
	var alpha token
	hasAlpha := false
	if len(t.args) > 1 {
		for _, arg := range t.args {
			if len(arg) != 1 {
				return nil, token{}, false, fmt.Errorf("%s(): each component is one value", t.text)
			}
			parts = append(parts, arg[0])
		}
		if len(parts) == 4 {
			alpha, hasAlpha, parts = parts[3], true, parts[:3]
		}
	} else {
		arg := t.args[0]
		for i, tok := range arg {
			if tok.kind == slash {
				if i+2 != len(arg) {
					return nil, token{}, false, fmt.Errorf("%s(): one alpha follows the /", t.text)
				}
				alpha, hasAlpha = arg[i+1], true
				break
			}
			parts = append(parts, tok)
		}
	}
	if len(parts) != 3 {
		return nil, token{}, false, fmt.Errorf("%s() takes three components and an optional alpha", t.text)
	}
	return parts, alpha, hasAlpha, nil
}

func parseAlpha(t token) (uint8, error) {
	if t.kind != number || (t.unit != "" && t.unit != "%") {
		return 0, fmt.Errorf("an alpha is a number or a percentage")
	}
	a := t.n
	if t.unit == "%" {
		a /= 100
	}
	return clampByte(a * 255), nil
}

func parseRGB(t token) (Color, error) {
	parts, alpha, hasAlpha, err := colorArgs(t)
	if err != nil {
		return Color{}, err
	}
	var c [3]uint8
	for i, p := range parts {
		if p.kind != number || (p.unit != "" && p.unit != "%") {
			return Color{}, fmt.Errorf("%s(): a component is a number or a percentage", t.text)
		}
		v := p.n
		if p.unit == "%" {
			v = v * 255 / 100
		}
		c[i] = clampByte(v)
	}
	out := Color{c[0], c[1], c[2], 255}
	if hasAlpha {
		if out.A, err = parseAlpha(alpha); err != nil {
			return Color{}, fmt.Errorf("%s(): %w", t.text, err)
		}
	}
	return out, nil
}

func parseHSL(t token) (Color, error) {
	parts, alpha, hasAlpha, err := colorArgs(t)
	if err != nil {
		return Color{}, err
	}
	h := parts[0]
	if h.kind != number || (h.unit != "" && h.unit != "deg") {
		return Color{}, fmt.Errorf("%s(): the hue is a number of degrees", t.text)
	}
	var sl [2]float64
	for i, p := range parts[1:] {
		if p.kind != number || p.unit != "%" {
			return Color{}, fmt.Errorf("%s(): saturation and lightness are percentages", t.text)
		}
		sl[i] = math.Max(0, math.Min(100, p.n)) / 100
	}
	r, g, b := hslToRGB(math.Mod(math.Mod(h.n, 360)+360, 360), sl[0], sl[1])
	out := Color{clampByte(r * 255), clampByte(g * 255), clampByte(b * 255), 255}
	if hasAlpha {
		if out.A, err = parseAlpha(alpha); err != nil {
			return Color{}, fmt.Errorf("%s(): %w", t.text, err)
		}
	}
	return out, nil
}

// hslToRGB is CSS Color 4's conversion.
func hslToRGB(h, s, l float64) (float64, float64, float64) {
	f := func(n float64) float64 {
		k := math.Mod(n+h/30, 12)
		a := s * math.Min(l, 1-l)
		return l - a*math.Max(-1, math.Min(k-3, math.Min(9-k, 1)))
	}
	return f(0), f(8), f(4)
}

func clampByte(v float64) uint8 {
	return uint8(math.Round(math.Max(0, math.Min(255, v))))
}

// named is CSS Color 4's named colours.
var named = map[string]Color{
	"aliceblue": {240, 248, 255, 255}, "antiquewhite": {250, 235, 215, 255}, "aqua": {0, 255, 255, 255},
	"aquamarine": {127, 255, 212, 255}, "azure": {240, 255, 255, 255}, "beige": {245, 245, 220, 255},
	"bisque": {255, 228, 196, 255}, "black": {0, 0, 0, 255}, "blanchedalmond": {255, 235, 205, 255},
	"blue": {0, 0, 255, 255}, "blueviolet": {138, 43, 226, 255}, "brown": {165, 42, 42, 255},
	"burlywood": {222, 184, 135, 255}, "cadetblue": {95, 158, 160, 255}, "chartreuse": {127, 255, 0, 255},
	"chocolate": {210, 105, 30, 255}, "coral": {255, 127, 80, 255}, "cornflowerblue": {100, 149, 237, 255},
	"cornsilk": {255, 248, 220, 255}, "crimson": {220, 20, 60, 255}, "cyan": {0, 255, 255, 255},
	"darkblue": {0, 0, 139, 255}, "darkcyan": {0, 139, 139, 255}, "darkgoldenrod": {184, 134, 11, 255},
	"darkgray": {169, 169, 169, 255}, "darkgreen": {0, 100, 0, 255}, "darkgrey": {169, 169, 169, 255},
	"darkkhaki": {189, 183, 107, 255}, "darkmagenta": {139, 0, 139, 255}, "darkolivegreen": {85, 107, 47, 255},
	"darkorange": {255, 140, 0, 255}, "darkorchid": {153, 50, 204, 255}, "darkred": {139, 0, 0, 255},
	"darksalmon": {233, 150, 122, 255}, "darkseagreen": {143, 188, 143, 255}, "darkslateblue": {72, 61, 139, 255},
	"darkslategray": {47, 79, 79, 255}, "darkslategrey": {47, 79, 79, 255}, "darkturquoise": {0, 206, 209, 255},
	"darkviolet": {148, 0, 211, 255}, "deeppink": {255, 20, 147, 255}, "deepskyblue": {0, 191, 255, 255},
	"dimgray": {105, 105, 105, 255}, "dimgrey": {105, 105, 105, 255}, "dodgerblue": {30, 144, 255, 255},
	"firebrick": {178, 34, 34, 255}, "floralwhite": {255, 250, 240, 255}, "forestgreen": {34, 139, 34, 255},
	"fuchsia": {255, 0, 255, 255}, "gainsboro": {220, 220, 220, 255}, "ghostwhite": {248, 248, 255, 255},
	"gold": {255, 215, 0, 255}, "goldenrod": {218, 165, 32, 255}, "gray": {128, 128, 128, 255},
	"green": {0, 128, 0, 255}, "greenyellow": {173, 255, 47, 255}, "grey": {128, 128, 128, 255},
	"honeydew": {240, 255, 240, 255}, "hotpink": {255, 105, 180, 255}, "indianred": {205, 92, 92, 255},
	"indigo": {75, 0, 130, 255}, "ivory": {255, 255, 240, 255}, "khaki": {240, 230, 140, 255},
	"lavender": {230, 230, 250, 255}, "lavenderblush": {255, 240, 245, 255}, "lawngreen": {124, 252, 0, 255},
	"lemonchiffon": {255, 250, 205, 255}, "lightblue": {173, 216, 230, 255}, "lightcoral": {240, 128, 128, 255},
	"lightcyan": {224, 255, 255, 255}, "lightgoldenrodyellow": {250, 250, 210, 255}, "lightgray": {211, 211, 211, 255},
	"lightgreen": {144, 238, 144, 255}, "lightgrey": {211, 211, 211, 255}, "lightpink": {255, 182, 193, 255},
	"lightsalmon": {255, 160, 122, 255}, "lightseagreen": {32, 178, 170, 255}, "lightskyblue": {135, 206, 250, 255},
	"lightslategray": {119, 136, 153, 255}, "lightslategrey": {119, 136, 153, 255}, "lightsteelblue": {176, 196, 222, 255},
	"lightyellow": {255, 255, 224, 255}, "lime": {0, 255, 0, 255}, "limegreen": {50, 205, 50, 255},
	"linen": {250, 240, 230, 255}, "magenta": {255, 0, 255, 255}, "maroon": {128, 0, 0, 255},
	"mediumaquamarine": {102, 205, 170, 255}, "mediumblue": {0, 0, 205, 255}, "mediumorchid": {186, 85, 211, 255},
	"mediumpurple": {147, 112, 219, 255}, "mediumseagreen": {60, 179, 113, 255}, "mediumslateblue": {123, 104, 238, 255},
	"mediumspringgreen": {0, 250, 154, 255}, "mediumturquoise": {72, 209, 204, 255}, "mediumvioletred": {199, 21, 133, 255},
	"midnightblue": {25, 25, 112, 255}, "mintcream": {245, 255, 250, 255}, "mistyrose": {255, 228, 225, 255},
	"moccasin": {255, 228, 181, 255}, "navajowhite": {255, 222, 173, 255}, "navy": {0, 0, 128, 255},
	"oldlace": {253, 245, 230, 255}, "olive": {128, 128, 0, 255}, "olivedrab": {107, 142, 35, 255},
	"orange": {255, 165, 0, 255}, "orangered": {255, 69, 0, 255}, "orchid": {218, 112, 214, 255},
	"palegoldenrod": {238, 232, 170, 255}, "palegreen": {152, 251, 152, 255}, "paleturquoise": {175, 238, 238, 255},
	"palevioletred": {219, 112, 147, 255}, "papayawhip": {255, 239, 213, 255}, "peachpuff": {255, 218, 185, 255},
	"peru": {205, 133, 63, 255}, "pink": {255, 192, 203, 255}, "plum": {221, 160, 221, 255},
	"powderblue": {176, 224, 230, 255}, "purple": {128, 0, 128, 255}, "rebeccapurple": {102, 51, 153, 255},
	"red": {255, 0, 0, 255}, "rosybrown": {188, 143, 143, 255}, "royalblue": {65, 105, 225, 255},
	"saddlebrown": {139, 69, 19, 255}, "salmon": {250, 128, 114, 255}, "sandybrown": {244, 164, 96, 255},
	"seagreen": {46, 139, 87, 255}, "seashell": {255, 245, 238, 255}, "sienna": {160, 82, 45, 255},
	"silver": {192, 192, 192, 255}, "skyblue": {135, 206, 235, 255}, "slateblue": {106, 90, 205, 255},
	"slategray": {112, 128, 144, 255}, "slategrey": {112, 128, 144, 255}, "snow": {255, 250, 250, 255},
	"springgreen": {0, 255, 127, 255}, "steelblue": {70, 130, 180, 255}, "tan": {210, 180, 140, 255},
	"teal": {0, 128, 128, 255}, "thistle": {216, 191, 216, 255}, "tomato": {255, 99, 71, 255},
	"turquoise": {64, 224, 208, 255}, "violet": {238, 130, 238, 255}, "wheat": {245, 222, 179, 255},
	"white": {255, 255, 255, 255}, "whitesmoke": {245, 245, 245, 255}, "yellow": {255, 255, 0, 255},
	"yellowgreen": {154, 205, 50, 255},
}
