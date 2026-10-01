// Package paint draws a laid-out card into an image (DESIGN.md §6.3, §6.5):
// backgrounds of colour, gradient or image, solid borders, rounded corners,
// overflow: hidden, opacity, images placed by object-fit, and text.
package paint

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/Elagoht/collage-ogimage/internal/css"
	"github.com/Elagoht/collage-ogimage/internal/dom"
	"github.com/Elagoht/collage-ogimage/internal/layout"
	"github.com/Elagoht/collage-ogimage/internal/text"
	"golang.org/x/image/vector"
)

// Images returns a loaded image for a URL, or nil when it could not be loaded.
type Images func(src string) image.Image

// Draw paints root — laid out, the root at the origin — into a new image of its
// size. m lays its text out again at the final widths; images supplies what the
// card's <img> and url() name.
func Draw(root *layout.Box, m *text.Measurer, images Images) *image.RGBA {
	w, h := int(math.Ceil(root.W)), int(math.Ceil(root.H))
	dst := image.NewRGBA(image.Rect(0, 0, max(1, w), max(1, h)))
	p := &painter{m: m, images: images}
	p.box(&canvas{dst: dst}, root)
	return dst
}

type painter struct {
	m      *text.Measurer
	images Images
}

// canvas is where drawing goes: an image, and the clip every shape is masked
// by — nil for none.
type canvas struct {
	dst  *image.RGBA
	clip *image.Alpha
}

func (p *painter) box(c *canvas, b *layout.Box) {
	s := b.Style
	if s.Opacity <= 0 {
		return
	}
	if s.Opacity < 1 {
		// The box and everything in it draw as one layer, then blend: two
		// overlapping children of a half-transparent box do not show through
		// each other, as in a browser.
		layer := &canvas{dst: image.NewRGBA(c.dst.Bounds()), clip: c.clip}
		p.contents(layer, b)
		mask := image.NewUniform(color.Alpha{A: uint8(math.Round(s.Opacity * 255))})
		draw.DrawMask(c.dst, c.dst.Bounds(), layer.dst, image.Point{}, mask, image.Point{}, draw.Over)
		return
	}
	p.contents(c, b)
}

func (p *painter) contents(c *canvas, b *layout.Box) {
	s := b.Style
	outer := radii(b)
	border := s.Border

	// Background, under the border, clipped to the border box's curve.
	p.background(c, b, outer)

	// Border: the outer curve less the inner one.
	if border > 0 {
		inner := shrink(outer, border)
		p.fill(c, func(r *pathBuilder) {
			r.roundRect(b.X, b.Y, b.W, b.H, outer, false)
			r.roundRect(b.X+border, b.Y+border, b.W-2*border, b.H-2*border, inner, true)
		}, rgba(s.BorderColor))
	}

	// What is inside: clipped to the padding box when overflow is hidden.
	inside := c
	if s.Overflow {
		inside = p.clipped(c, b.X+border, b.Y+border, b.W-2*border, b.H-2*border, shrink(outer, border))
	}

	switch b.Node.Kind {
	case dom.Image:
		p.image(inside, b, outer)
	case dom.TextBlock, dom.Text:
		p.text(inside, b)
	}
	for _, child := range b.Children {
		p.box(inside, child)
	}
}

// background draws a box's background layer: a colour, then a gradient or an
// image over it.
func (p *painter) background(c *canvas, b *layout.Box, corners [4]corner) {
	bg := b.Style.Background
	shape := func(r *pathBuilder) { r.roundRect(b.X, b.Y, b.W, b.H, corners, false) }
	if bg.HasColor && bg.Color.A > 0 {
		p.fill(c, shape, rgba(bg.Color))
	}
	if bg.Gradient != nil {
		p.fillImage(c, shape, newGradient(bg.Gradient, b.X, b.Y, b.W, b.H))
	}
	if bg.Image != "" && p.images != nil {
		if img := p.images(string(bg.Image)); img != nil {
			// background-size: cover, contain, or the image at its own size
			// at the top-left corner (auto).
			fit := "none"
			if d, ok := b.Node.Last("background-size"); ok && !d.Dynamic {
				fit = string(d.Value.(css.Keyword))
			}
			x, y, w, h := place(img.Bounds(), b.X, b.Y, b.W, b.H, fit)
			p.fillImage(c, shape, scaled(img, x, y, w, h))
		}
	}
}

// image draws an <img> into its content box by object-fit, clipped to the
// box's corners.
func (p *painter) image(c *canvas, b *layout.Box, corners [4]corner) {
	if p.images == nil {
		return
	}
	img := p.images(b.Node.Src)
	if img == nil {
		return
	}
	border := b.Style.Border
	cx := b.X + border + b.Padding[3]
	cy := b.Y + border + b.Padding[0]
	cw := b.W - 2*border - b.Padding[1] - b.Padding[3]
	ch := b.H - 2*border - b.Padding[0] - b.Padding[2]
	if cw <= 0 || ch <= 0 {
		return
	}
	x, y, w, h := place(img.Bounds(), cx, cy, cw, ch, b.Style.ObjectFit)
	content := shrink(corners, border)
	p.fillImage(c, func(r *pathBuilder) { r.roundRect(cx, cy, cw, ch, content, false) }, scaled(img, x, y, w, h))
}

// place is where an image of bounds goes in a box, by fit: fill stretches it,
// cover covers the box, contain fits inside it, and none keeps its size at the
// box's corner. cover and contain centre it.
func place(bounds image.Rectangle, x, y, w, h float64, fit string) (float64, float64, float64, float64) {
	iw, ih := float64(bounds.Dx()), float64(bounds.Dy())
	if iw <= 0 || ih <= 0 {
		return x, y, w, h
	}
	switch fit {
	case "cover", "contain":
		sx, sy := w/iw, h/ih
		k := math.Max(sx, sy)
		if fit == "contain" {
			k = math.Min(sx, sy)
		}
		nw, nh := iw*k, ih*k
		return x + (w-nw)/2, y + (h-nh)/2, nw, nh
	case "none":
		return x, y, iw, ih
	}
	return x, y, w, h
}

// text draws a text block's lines in their runs' colours.
func (p *painter) text(c *canvas, b *layout.Box) {
	border := b.Style.Border
	cx := b.X + border + b.Padding[3]
	cy := b.Y + border + b.Padding[0]
	cw := b.W - 2*border - b.Padding[1] - b.Padding[3]
	para := p.m.Layout(b.Node, b.Style, math.Max(0, cw), true)
	for _, ln := range para.Lines {
		baseline := cy + ln.Baseline
		// One shape per colour on a line: a whole line rasterized at once.
		byColor := map[css.Color][]text.Glyph{}
		var order []css.Color
		for _, g := range ln.Glyphs {
			col := g.Style.Color
			if _, ok := byColor[col]; !ok {
				order = append(order, col)
			}
			byColor[col] = append(byColor[col], g)
		}
		for _, col := range order {
			glyphs := byColor[col]
			p.fill(c, func(r *pathBuilder) {
				for _, g := range glyphs {
					segs, err := p.m.Fonts.Outline(g, g.Size)
					if err != nil {
						continue
					}
					r.glyph(segs, cx+g.X, baseline)
				}
			}, rgba(col))
		}
	}
}

func rgba(c css.Color) color.Color {
	return color.NRGBA{R: c.R, G: c.G, B: c.B, A: c.A}
}

// fill draws a shape in a colour.
func (p *painter) fill(c *canvas, shape func(*pathBuilder), col color.Color) {
	p.fillImage(c, shape, image.NewUniform(col))
}

// fillImage draws a shape, filled from src in the card's coordinates, masked by
// the canvas's clip.
func (p *painter) fillImage(c *canvas, shape func(*pathBuilder), src image.Image) {
	pb := &pathBuilder{}
	shape(pb)
	bounds := pb.bounds().Intersect(c.dst.Bounds())
	if bounds.Empty() {
		return
	}
	r := vector.NewRasterizer(bounds.Dx(), bounds.Dy())
	pb.replay(r, float32(bounds.Min.X), float32(bounds.Min.Y))
	mask := image.NewAlpha(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	r.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	if c.clip != nil {
		for y := 0; y < bounds.Dy(); y++ {
			for x := 0; x < bounds.Dx(); x++ {
				i := mask.PixOffset(x, y)
				if mask.Pix[i] == 0 {
					continue
				}
				clip := c.clip.AlphaAt(bounds.Min.X+x, bounds.Min.Y+y).A
				mask.Pix[i] = uint8(uint16(mask.Pix[i]) * uint16(clip) / 255)
			}
		}
	}
	draw.DrawMask(c.dst, bounds, src, bounds.Min, mask, image.Point{}, draw.Over)
}

// clipped is c with its clip narrowed to a rounded rectangle.
func (p *painter) clipped(c *canvas, x, y, w, h float64, corners [4]corner) *canvas {
	mask := image.NewAlpha(c.dst.Bounds())
	pb := &pathBuilder{}
	pb.roundRect(x, y, w, h, corners, false)
	bounds := pb.bounds().Intersect(c.dst.Bounds())
	if !bounds.Empty() {
		r := vector.NewRasterizer(bounds.Dx(), bounds.Dy())
		pb.replay(r, float32(bounds.Min.X), float32(bounds.Min.Y))
		r.Draw(mask, bounds, image.Opaque, image.Point{})
	}
	if c.clip != nil {
		for i := range mask.Pix {
			mask.Pix[i] = uint8(uint16(mask.Pix[i]) * uint16(c.clip.Pix[i]) / 255)
		}
	}
	return &canvas{dst: c.dst, clip: mask}
}
