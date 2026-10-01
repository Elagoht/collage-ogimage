package layout

import (
	"fmt"
	"math"

	"github.com/Elagoht/collage-ogimage/internal/dom"
)

// Measurer sizes what layout cannot: text, and images that do not declare their
// size. Text arrives in phase 3; images in phase 4.
type Measurer interface {
	// MeasureText returns the size of a text block's content — a TextBlock, or
	// the anonymous box of text directly inside a flex container — laid out at
	// most maxWidth wide: math.Inf(1) for its max-content width, 0 for its
	// min-content width.
	MeasureText(n *dom.Node, s *Style, maxWidth float64) (width, height float64)
	// ImageSize returns an image's natural size, and false when it is not known.
	ImageSize(n *dom.Node) (width, height float64, ok bool)
}

// Box is one laid-out element.
type Box struct {
	Node  *dom.Node
	Style *Style
	// X, Y, W and H are the border box, in the card's pixels, the root at 0, 0.
	X, Y, W, H float64
	// Padding and Margin are resolved, in CSS order: top, right, bottom, left.
	Padding, Margin [4]float64
	Children        []*Box

	// Auto margins, which absorb free space.
	autoMargin [4]bool
}

// Layout lays root out and returns its box tree.
func Layout(root *dom.Node, m Measurer) (*Box, error) {
	if root == nil {
		return nil, fmt.Errorf("layout: no root")
	}
	l := &layouter{m: m, heights: map[heightKey]float64{}, widths: map[widthKey]float64{}}
	b := l.build(root, rootStyle(), 0)
	w := b.Style.Width.resolve(math.NaN())
	h := b.Style.Height.resolve(math.NaN())
	if math.IsNaN(w) {
		w = DefaultWidth
	}
	if math.IsNaN(h) {
		h = DefaultHeight
	}
	l.resolveEdges(b, w)
	l.layout(b, w, h, w, h)
	place(b, 0, 0)
	return b, nil
}

type layouter struct {
	m Measurer
	// heights and widths remember measurements: a flex container sizes an item
	// by laying it out to see how tall it comes out, and a nested container does
	// the same to its own, so without them the same subtree is measured a number
	// of times exponential in the depth. A measurement depends only on the
	// subtree and the sizes it is measured against, which are the keys.
	heights map[heightKey]float64
	widths  map[widthKey]float64
}

type heightKey struct {
	b           *Box
	w, cbW, cbH float64
}

type widthKey struct {
	b          *Box
	maxContent bool
}

// key makes a NaN usable in a map key, where NaN never equals itself.
func key(v float64) float64 {
	if math.IsNaN(v) {
		return math.Inf(-1)
	}
	return v
}

// measureHeight is b's border-box height laid out w wide with its height from
// its content, remembered.
func (l *layouter) measureHeight(b *Box, w, cbW, cbH float64) float64 {
	k := heightKey{b, key(w), key(cbW), key(cbH)}
	if h, ok := l.heights[k]; ok {
		return h
	}
	l.layout(b, w, math.NaN(), cbW, cbH)
	l.heights[k] = b.H
	return b.H
}

// build makes the box tree, computing each element's style. Hidden elements
// have no box; text directly inside a flex container is an anonymous box.
func (l *layouter) build(n *dom.Node, parent *Style, rootFont float64) *Box {
	s := compute(n, parent, rootFont)
	if rootFont == 0 {
		rootFont = s.FontSize
	}
	b := &Box{Node: n, Style: s}
	if n.Kind == dom.Container {
		for _, c := range n.Children {
			if c.Kind == dom.Hidden {
				continue
			}
			b.Children = append(b.Children, l.build(c, s, rootFont))
		}
	}
	// AlignSelf auto is the parent's align-items.
	for _, c := range b.Children {
		if c.Style.AlignSelf == "auto" {
			c.Style.AlignSelf = s.AlignItems
		}
	}
	return b
}

// resolveEdges resolves b's padding and margin against its containing block's
// width — which is what CSS resolves both against, vertical sides included.
func (l *layouter) resolveEdges(b *Box, cbWidth float64) {
	for i := range 4 {
		b.Padding[i] = zeroIfNaN(b.Style.Padding[i].resolve(cbWidth))
		if b.Style.Margin[i].auto {
			b.autoMargin[i] = true
			b.Margin[i] = 0
		} else {
			b.Margin[i] = zeroIfNaN(b.Style.Margin[i].resolve(cbWidth))
		}
	}
}

func zeroIfNaN(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return v
}

// frame is b's padding and border, horizontally and vertically.
func (b *Box) frame() (float64, float64) {
	return b.Padding[1] + b.Padding[3] + 2*b.Style.Border, b.Padding[0] + b.Padding[2] + 2*b.Style.Border
}

// layout sizes b to w by h (h NaN: from its content) and lays its children out
// inside it. cbW and cbH are b's containing block, for percentages.
func (l *layouter) layout(b *Box, w, h, cbW, cbH float64) {
	b.W = w
	fw, fh := b.frame()
	switch b.Node.Kind {
	case dom.Container:
		l.flex(b, w-fw, h, fh)
	case dom.Image:
		if math.IsNaN(h) {
			h = l.imageHeight(b, w)
		}
		b.H = h
	default:
		if math.IsNaN(h) {
			_, th := l.m.MeasureText(b.Node, b.Style, math.Max(0, w-fw))
			h = th + fh
		}
		b.H = h
	}
	b.H = clamp(b.H, l.minSize(b, false, cbH), l.maxSize(b, false, cbH))
}

func (l *layouter) imageHeight(b *Box, w float64) float64 {
	fw, fh := b.frame()
	if nw, nh, ok := l.m.ImageSize(b.Node); ok && nw > 0 {
		return (w-fw)*nh/nw + fh
	}
	return fh
}

func clamp(v, lo, hi float64) float64 {
	if !math.IsNaN(hi) && v > hi {
		v = hi
	}
	if !math.IsNaN(lo) && v < lo {
		v = lo
	}
	return v
}

// minSize and maxSize are b's declared minimum and maximum width (or height),
// NaN when there is none; an auto minimum is 0 here — a flex item's automatic
// minimum is the flex algorithm's to apply.
func (l *layouter) minSize(b *Box, horizontal bool, cb float64) float64 {
	s := b.Style.MinHeight
	if horizontal {
		s = b.Style.MinWidth
	}
	if s.auto {
		return 0
	}
	return s.resolve(cb)
}

func (l *layouter) maxSize(b *Box, horizontal bool, cb float64) float64 {
	s := b.Style.MaxHeight
	if horizontal {
		s = b.Style.MaxWidth
	}
	return s.resolve(cb)
}

// definite is b's declared width (or height) in pixels, or NaN when it is auto
// or a percentage of an indefinite containing block.
func definite(b *Box, horizontal bool, cb float64) float64 {
	if horizontal {
		return b.Style.Width.resolve(cb)
	}
	return b.Style.Height.resolve(cb)
}

// contentWidth is b's max-content (maxContent true) or min-content border-box
// width: what it is when nothing constrains it, or when everything does. A
// declared width is the answer when there is one.
func (l *layouter) contentWidth(b *Box, maxContent bool) float64 {
	if w := b.Style.Width.resolve(math.NaN()); !math.IsNaN(w) {
		return w
	}
	return l.intrinsicWidth(b, maxContent)
}

// intrinsicWidth is contentWidth from b's content alone, whatever width b
// declares: what CSS calls the content size, and what a flex item's automatic
// minimum is made of.
func (l *layouter) intrinsicWidth(b *Box, maxContent bool) float64 {
	k := widthKey{b, maxContent}
	if w, ok := l.widths[k]; ok {
		return w
	}
	w := l.measureIntrinsicWidth(b, maxContent)
	l.widths[k] = w
	return w
}

func (l *layouter) measureIntrinsicWidth(b *Box, maxContent bool) float64 {
	fw, _ := b.frame()
	var inner float64
	switch b.Node.Kind {
	case dom.Container:
		gap := zeroIfNaN(b.Style.ColumnGap.resolve(math.NaN()))
		for i, c := range b.Children {
			cw := l.contribution(c, maxContent)
			switch {
			case b.Style.Column, b.Style.Wrap && !maxContent:
				inner = math.Max(inner, cw)
			default:
				inner += cw
				if i > 0 {
					inner += gap
				}
			}
		}
	case dom.Image:
		if nw, _, ok := l.m.ImageSize(b.Node); ok {
			inner = nw
		}
	default:
		width := math.Inf(1)
		if !maxContent {
			width = 0
		}
		inner, _ = l.m.MeasureText(b.Node, b.Style, width)
	}
	return inner + fw
}

// contribution is c's outer content width as its parent sizes itself by it:
// clamped by its declared minimum and maximum, with its margins.
func (l *layouter) contribution(c *Box, maxContent bool) float64 {
	l.resolveEdges(c, math.NaN())
	w := clamp(l.contentWidth(c, maxContent), l.minSize(c, true, math.NaN()), l.maxSize(c, true, math.NaN()))
	return w + c.Margin[1] + c.Margin[3]
}

// place turns b's children's positions, relative to b, into the card's.
func place(b *Box, x, y float64) {
	b.X += x
	b.Y += y
	for _, c := range b.Children {
		place(c, b.X, b.Y)
	}
}
