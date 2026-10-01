package paint

import (
	"image"
	"image/color"
	"math"

	"github.com/Elagoht/collage-ogimage/internal/css"
	"github.com/Elagoht/collage-ogimage/internal/layout"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/vector"
)

// corner is one corner's radii, horizontal and vertical: a percentage of a
// non-square box is an ellipse.
type corner struct{ rx, ry float64 }

// radii resolves a box's border-radius, and scales every corner down together
// when two that share a side would overlap, as CSS does.
func radii(b *layout.Box) [4]corner {
	var cs [4]corner
	for i, l := range b.Style.Radii {
		if l.Percent() {
			cs[i] = corner{l.N() * b.W / 100, l.N() * b.H / 100}
		} else {
			cs[i] = corner{l.N(), l.N()}
		}
	}
	f := 1.0
	for _, pair := range [][3]float64{
		{cs[0].rx + cs[1].rx, b.W}, {cs[3].rx + cs[2].rx, b.W},
		{cs[0].ry + cs[3].ry, b.H}, {cs[1].ry + cs[2].ry, b.H},
	} {
		if pair[0] > 0 && pair[0] > pair[1] {
			f = math.Min(f, pair[1]/pair[0])
		}
	}
	for i := range cs {
		cs[i].rx *= f
		cs[i].ry *= f
	}
	return cs
}

// shrink is the corners of a box inset by d: each radius less d, never below 0.
func shrink(cs [4]corner, d float64) [4]corner {
	for i := range cs {
		cs[i] = corner{math.Max(0, cs[i].rx-d), math.Max(0, cs[i].ry-d)}
	}
	return cs
}

// pathBuilder records a path in the card's coordinates, so its bounds are known
// before a rasterizer of their size is made, and replays it offset into one.
type pathBuilder struct {
	ops                    []op
	minX, minY, maxX, maxY float64
	started                bool
}

type op struct {
	kind byte // 'M', 'L', 'Q', 'C', 'Z'
	pts  [3][2]float64
}

func (p *pathBuilder) see(x, y float64) {
	if !p.started {
		p.minX, p.minY, p.maxX, p.maxY, p.started = x, y, x, y, true
		return
	}
	p.minX, p.minY = math.Min(p.minX, x), math.Min(p.minY, y)
	p.maxX, p.maxY = math.Max(p.maxX, x), math.Max(p.maxY, y)
}

func (p *pathBuilder) add(kind byte, pts ...[2]float64) {
	o := op{kind: kind}
	copy(o.pts[:], pts)
	for _, pt := range pts {
		p.see(pt[0], pt[1])
	}
	p.ops = append(p.ops, o)
}

func (p *pathBuilder) bounds() image.Rectangle {
	if !p.started {
		return image.Rectangle{}
	}
	return image.Rect(int(math.Floor(p.minX)), int(math.Floor(p.minY)), int(math.Ceil(p.maxX)), int(math.Ceil(p.maxY)))
}

func (p *pathBuilder) replay(r *vector.Rasterizer, ox, oy float32) {
	pt := func(v [2]float64) (float32, float32) { return float32(v[0]) - ox, float32(v[1]) - oy }
	for _, o := range p.ops {
		switch o.kind {
		case 'M':
			r.MoveTo(pt(o.pts[0]))
		case 'L':
			r.LineTo(pt(o.pts[0]))
		case 'Q':
			x1, y1 := pt(o.pts[0])
			x2, y2 := pt(o.pts[1])
			r.QuadTo(x1, y1, x2, y2)
		case 'C':
			x1, y1 := pt(o.pts[0])
			x2, y2 := pt(o.pts[1])
			x3, y3 := pt(o.pts[2])
			r.CubeTo(x1, y1, x2, y2, x3, y3)
		case 'Z':
			r.ClosePath()
		}
	}
}

// kappa places a cubic Bézier's handles to approximate a quarter ellipse.
const kappa = 0.5522847498

// roundRect adds a rectangle with elliptical corners; reverse draws it the other
// way round, which cuts it out of a shape drawn before it.
func (p *pathBuilder) roundRect(x, y, w, h float64, cs [4]corner, reverse bool) {
	if w <= 0 || h <= 0 {
		return
	}
	tl, tr, br, bl := cs[0], cs[1], cs[2], cs[3]
	type seg struct {
		kind byte
		pts  [][2]float64
	}
	// Clockwise from the top edge.
	path := []seg{
		{'M', [][2]float64{{x + tl.rx, y}}},
		{'L', [][2]float64{{x + w - tr.rx, y}}},
		{'C', [][2]float64{{x + w - tr.rx + tr.rx*kappa, y}, {x + w, y + tr.ry - tr.ry*kappa}, {x + w, y + tr.ry}}},
		{'L', [][2]float64{{x + w, y + h - br.ry}}},
		{'C', [][2]float64{{x + w, y + h - br.ry + br.ry*kappa}, {x + w - br.rx + br.rx*kappa, y + h}, {x + w - br.rx, y + h}}},
		{'L', [][2]float64{{x + bl.rx, y + h}}},
		{'C', [][2]float64{{x + bl.rx - bl.rx*kappa, y + h}, {x, y + h - bl.ry + bl.ry*kappa}, {x, y + h - bl.ry}}},
		{'L', [][2]float64{{x, y + tl.ry}}},
		{'C', [][2]float64{{x, y + tl.ry - tl.ry*kappa}, {x + tl.rx - tl.rx*kappa, y}, {x + tl.rx, y}}},
	}
	if !reverse {
		for _, s := range path {
			p.add(s.kind, s.pts...)
		}
		p.add('Z')
		return
	}
	// Reversed: walk the same points backwards. Each segment ends where the
	// next begins, so a segment reversed starts at its own end and runs back
	// to the previous segment's end.
	end := func(i int) [2]float64 { return path[i].pts[len(path[i].pts)-1] }
	p.add('M', end(len(path)-1))
	for i := len(path) - 1; i >= 1; i-- {
		s := path[i]
		prev := end(i - 1)
		switch s.kind {
		case 'L':
			p.add('L', prev)
		case 'C':
			p.add('C', s.pts[1], s.pts[0], prev)
		}
	}
	p.add('Z')
}

// glyph adds a glyph's outline with its origin at x, baseline.
func (p *pathBuilder) glyph(segs sfnt.Segments, x, baseline float64) {
	pt := func(v [2]float64) [2]float64 { return [2]float64{x + v[0], baseline + v[1]} }
	for _, s := range segs {
		var pts [3][2]float64
		for i, a := range s.Args {
			pts[i] = [2]float64{float64(a.X) / 64, float64(a.Y) / 64}
		}
		switch s.Op {
		case sfnt.SegmentOpMoveTo:
			p.add('M', pt(pts[0]))
		case sfnt.SegmentOpLineTo:
			p.add('L', pt(pts[0]))
		case sfnt.SegmentOpQuadTo:
			p.add('Q', pt(pts[0]), pt(pts[1]))
		case sfnt.SegmentOpCubeTo:
			p.add('C', pt(pts[0]), pt(pts[1]), pt(pts[2]))
		}
	}
}

// gradient is a linear-gradient over a box, as an image: the colour at each
// point along the gradient line, interpolated in premultiplied sRGB as CSS
// interpolates it.
type gradient struct {
	x0, y0, dx, dy, length float64
	stops                  []stop
}

type stop struct {
	at float64 // 0..1 along the line
	c  [4]float64
}

func newGradient(g *css.Gradient, x, y, w, h float64) *gradient {
	angle := g.Angle * math.Pi / 180
	if g.Corner != "" {
		// A corner's angle makes the line perpendicular to the diagonal
		// through the other two corners: CSS's "magic corners".
		sx, sy := 1.0, -1.0
		if g.Corner == "bottom right" || g.Corner == "bottom left" {
			sy = 1
		}
		if g.Corner == "top left" || g.Corner == "bottom left" {
			sx = -1
		}
		angle = math.Atan2(sx*h, -sy*w)
	}
	dx, dy := math.Sin(angle), -math.Cos(angle)
	length := math.Abs(w*dx) + math.Abs(h*dy)
	cx, cy := x+w/2, y+h/2
	gr := &gradient{x0: cx - dx*length/2, y0: cy - dy*length/2, dx: dx, dy: dy, length: length}

	n := len(g.Stops)
	ats := make([]float64, n)
	known := make([]bool, n)
	for i, s := range g.Stops {
		if s.HasAt {
			v := s.At.N
			if s.At.Unit == css.Percent {
				v /= 100
			} else if length > 0 {
				v /= length
			}
			ats[i], known[i] = v, true
		}
	}
	if !known[0] {
		ats[0], known[0] = 0, true
	}
	if !known[n-1] {
		ats[n-1], known[n-1] = 1, true
	}
	// Positions never go back, and unset ones spread evenly between set ones.
	for i := 1; i < n; i++ {
		if known[i] && ats[i] < ats[i-1] {
			ats[i] = ats[i-1]
		}
	}
	for i := 0; i < n; {
		j := i + 1
		for j < n && !known[j] {
			j++
		}
		for k := i + 1; k < j; k++ {
			ats[k] = ats[i] + (ats[j]-ats[i])*float64(k-i)/float64(j-i)
		}
		i = j
	}
	for i, s := range g.Stops {
		a := float64(s.Color.A) / 255
		gr.stops = append(gr.stops, stop{at: ats[i], c: [4]float64{
			float64(s.Color.R) * a, float64(s.Color.G) * a, float64(s.Color.B) * a, a * 255}})
	}
	return gr
}

func (g *gradient) ColorModel() color.Model { return color.RGBAModel }
func (g *gradient) Bounds() image.Rectangle {
	return image.Rect(-1e9, -1e9, 1e9, 1e9)
}

func (g *gradient) At(x, y int) color.Color {
	t := 0.0
	if g.length > 0 {
		t = ((float64(x)+0.5-g.x0)*g.dx + (float64(y)+0.5-g.y0)*g.dy) / g.length
	}
	s := g.stops
	var c [4]float64
	switch {
	case t <= s[0].at:
		c = s[0].c
	case t >= s[len(s)-1].at:
		c = s[len(s)-1].c
	default:
		for i := 1; i < len(s); i++ {
			if t <= s[i].at {
				span := s[i].at - s[i-1].at
				f := 0.0
				if span > 0 {
					f = (t - s[i-1].at) / span
				}
				for k := range c {
					c[k] = s[i-1].c[k] + (s[i].c[k]-s[i-1].c[k])*f
				}
				break
			}
		}
	}
	return color.RGBA{R: uint8(c[0] + .5), G: uint8(c[1] + .5), B: uint8(c[2] + .5), A: uint8(c[3] + .5)}
}

// scaledImage is an image drawn into the rectangle x, y, w, h of the card, as a
// source: sampled bilinearly at each card pixel.
type scaledImage struct {
	src        image.Image
	x, y, w, h float64
}

func scaled(src image.Image, x, y, w, h float64) image.Image {
	return &scaledImage{src: src, x: x, y: y, w: w, h: h}
}

func (s *scaledImage) ColorModel() color.Model { return color.RGBAModel }
func (s *scaledImage) Bounds() image.Rectangle {
	return image.Rect(-1e9, -1e9, 1e9, 1e9)
}

func (s *scaledImage) At(x, y int) color.Color {
	b := s.src.Bounds()
	if s.w <= 0 || s.h <= 0 {
		return color.RGBA{}
	}
	u := (float64(x) + 0.5 - s.x) / s.w * float64(b.Dx())
	v := (float64(y) + 0.5 - s.y) / s.h * float64(b.Dy())
	if u < 0 || v < 0 || u >= float64(b.Dx()) || v >= float64(b.Dy()) {
		return color.RGBA{}
	}
	return bilinear(s.src, b, u-0.5, v-0.5)
}

func bilinear(src image.Image, b image.Rectangle, u, v float64) color.Color {
	x0, y0 := int(math.Floor(u)), int(math.Floor(v))
	fx, fy := u-float64(x0), v-float64(y0)
	at := func(x, y int) [4]float64 {
		x = min(max(x, 0), b.Dx()-1)
		y = min(max(y, 0), b.Dy()-1)
		r, g, bl, a := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
		return [4]float64{float64(r), float64(g), float64(bl), float64(a)}
	}
	c00, c10, c01, c11 := at(x0, y0), at(x0+1, y0), at(x0, y0+1), at(x0+1, y0+1)
	var out [4]uint8
	for k := range 4 {
		top := c00[k] + (c10[k]-c00[k])*fx
		bot := c01[k] + (c11[k]-c01[k])*fx
		out[k] = uint8((top + (bot-top)*fy) / 257)
	}
	return color.RGBA{R: out[0], G: out[1], B: out[2], A: out[3]}
}
