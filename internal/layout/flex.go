package layout

import "math"

// item is one flex item while its container lays it out.
type item struct {
	box *Box
	// base is the flex base size; hyp the hypothetical main size, base clamped
	// by min and max; target the size the algorithm settles on. All border-box.
	base, hyp, target float64
	min, max          float64
	frozen            bool
	// marginMain is the item's non-auto margins on the main axis; autoMain how
	// many of its main-axis margins are auto.
	marginMain float64
	autoMain   int
	// cross is the item's border-box cross size; stretch whether it is to fill
	// its line.
	cross   float64
	stretch bool
}

type line struct {
	items []*item
	cross float64
}

// flex lays the container b's children out. contentW is its content width; h its
// border-box height, NaN when it comes from the content; fh its vertical frame.
func (l *layouter) flex(b *Box, contentW, h, fh float64) {
	s := b.Style
	contentH := math.NaN()
	if !math.IsNaN(h) {
		contentH = h - fh
	}
	row := !s.Column
	mainSize, crossSize := contentW, contentH
	if !row {
		mainSize, crossSize = contentH, contentW
	}
	colGap := zeroIfNaN(s.ColumnGap.resolve(contentW))
	rowGap := zeroIfNaN(s.RowGap.resolve(contentH))
	mainGap, crossGap := colGap, rowGap
	if !row {
		mainGap, crossGap = rowGap, colGap
	}

	items := make([]*item, 0, len(b.Children))
	for _, c := range b.Children {
		l.resolveEdges(c, contentW)
		items = append(items, l.prepare(c, row, mainSize, crossSize, contentW, contentH))
	}

	// Lines: one, unless the container wraps and has a main size to wrap at.
	var lines []*line
	if !s.Wrap || math.IsNaN(mainSize) {
		lines = []*line{{items: items}}
	} else {
		cur := &line{}
		used := 0.0
		for _, it := range items {
			outer := it.hyp + it.marginMain
			gap := 0.0
			if len(cur.items) > 0 {
				gap = mainGap
			}
			if len(cur.items) > 0 && used+gap+outer > mainSize {
				lines = append(lines, cur)
				cur, used, gap = &line{}, 0, 0
			}
			cur.items = append(cur.items, it)
			used += gap + outer
		}
		if len(cur.items) > 0 || len(lines) == 0 {
			lines = append(lines, cur)
		}
	}

	// Main sizes.
	usedMain := 0.0
	for _, ln := range lines {
		gaps := mainGap * float64(max(0, len(ln.items)-1))
		if math.IsNaN(mainSize) {
			for _, it := range ln.items {
				it.target = it.hyp
			}
		} else {
			resolveFlexible(ln.items, mainSize, gaps)
		}
		sum := gaps
		for _, it := range ln.items {
			sum += it.target + it.marginMain
		}
		usedMain = math.Max(usedMain, sum)
	}
	if math.IsNaN(mainSize) {
		mainSize = usedMain
	}

	// Cross sizes: an item's own, then its line's.
	for _, ln := range lines {
		for _, it := range ln.items {
			if row && math.IsNaN(it.cross) {
				it.cross = l.measureHeight(it.box, it.target, contentW, contentH)
			}
			ln.cross = math.Max(ln.cross, it.cross+l.marginCross(it.box, row))
		}
	}
	if len(lines) == 1 && !math.IsNaN(crossSize) {
		lines[0].cross = crossSize
	}
	usedCross := crossGap * float64(max(0, len(lines)-1))
	for _, ln := range lines {
		usedCross += ln.cross
	}
	if math.IsNaN(crossSize) {
		crossSize = usedCross
	} else if len(lines) > 1 && crossSize > usedCross {
		// align-content: normal stretches the lines into the free space.
		extra := (crossSize - usedCross) / float64(len(lines))
		for _, ln := range lines {
			ln.cross += extra
		}
	}

	// Stretch, and the final layout of every item at its size.
	for _, ln := range lines {
		for _, it := range ln.items {
			c := it.box
			if it.stretch {
				it.cross = clamp(ln.cross-l.marginCross(c, row),
					l.minSize(c, !row, crossBase(row, contentW, contentH)),
					l.maxSize(c, !row, crossBase(row, contentW, contentH)))
			}
			if row {
				l.layout(c, it.target, it.cross, contentW, contentH)
			} else {
				l.layout(c, it.cross, it.target, contentW, contentH)
			}
		}
	}

	// Positions, relative to b's border box.
	originMain, originCross := b.Padding[3]+s.Border, b.Padding[0]+s.Border
	if !row {
		originMain, originCross = b.Padding[0]+s.Border, b.Padding[3]+s.Border
	}
	crossAt := originCross
	for _, ln := range lines {
		positionLine(ln, row, s.Justify, mainSize, mainGap, originMain, crossAt)
		crossAt += ln.cross + crossGap
	}

	if math.IsNaN(h) {
		inner := crossSize
		if !row {
			inner = mainSize
		}
		b.H = inner + fh
	} else {
		b.H = h
	}
}

// crossBase is what a percentage on the cross axis resolves against.
func crossBase(row bool, contentW, contentH float64) float64 {
	if row {
		return contentH
	}
	return contentW
}

// prepare computes an item's flex base size, hypothetical main size, its limits,
// and — on a column's cross axis — its width.
func (l *layouter) prepare(c *Box, row bool, mainSize, crossSize, contentW, contentH float64) *item {
	it := &item{box: c, cross: math.NaN()}
	m := c.Margin
	if row {
		it.marginMain = m[1] + m[3]
		it.autoMain = count(c.autoMargin[1], c.autoMargin[3])
	} else {
		it.marginMain = m[0] + m[2]
		it.autoMain = count(c.autoMargin[0], c.autoMargin[2])
	}
	autoCross := c.autoMargin[0] || c.autoMargin[2]
	if !row {
		autoCross = c.autoMargin[1] || c.autoMargin[3]
	}

	// The cross size, where it is known before the main one.
	declaredCross := definite(c, !row, crossSize)
	switch {
	case !math.IsNaN(declaredCross):
		it.cross = declaredCross
	case c.Style.AlignSelf == "stretch" && !autoCross:
		it.stretch = true
		if !row && !math.IsNaN(crossSize) {
			it.cross = crossSize - l.marginCross(c, row)
		}
	}
	if !row && math.IsNaN(it.cross) {
		// A column's item of auto width is as wide as its content, up to the
		// column's width.
		avail := crossSize - l.marginCross(c, row)
		it.cross = math.Min(l.contentWidth(c, true), math.Max(l.contentWidth(c, false), avail))
	}
	if !row {
		it.cross = clamp(it.cross, l.minSize(c, true, contentW), l.maxSize(c, true, contentW))
	}

	// The flex base size.
	base := c.Style.Basis.resolve(mainSize)
	if math.IsNaN(base) {
		base = definite(c, row, mainSize)
	}
	if math.IsNaN(base) {
		base = l.mainContent(c, row, it.cross, contentW, contentH)
	}
	it.base = base

	// The limits, the minimum automatic unless declared.
	it.max = l.maxSize(c, row, mainSize)
	if minDecl := mainMin(c, row); !minDecl.auto {
		it.min = zeroIfNaN(minDecl.resolve(mainSize))
	} else if c.Style.Overflow {
		it.min = 0
	} else {
		// The content-based minimum: a row item's min-content width; a
		// column item's height at its width, which is its min-content height.
		var auto float64
		if row {
			auto = l.intrinsicWidth(c, false)
		} else {
			auto = l.mainContent(c, row, it.cross, contentW, contentH)
		}
		if spec := definite(c, row, mainSize); !math.IsNaN(spec) {
			auto = math.Min(auto, spec)
		}
		if !math.IsNaN(it.max) {
			auto = math.Min(auto, it.max)
		}
		it.min = auto
	}
	it.hyp = clamp(it.base, it.min, it.max)
	return it
}

func mainMin(c *Box, row bool) length {
	if row {
		return c.Style.MinWidth
	}
	return c.Style.MinHeight
}

// mainContent is c's max-content size on the main axis: its max-content width in
// a row, its height at width crossW in a column.
func (l *layouter) mainContent(c *Box, row bool, crossW, contentW, contentH float64) float64 {
	if row {
		return l.contentWidth(c, true)
	}
	return l.measureHeight(c, crossW, contentW, contentH)
}

func (l *layouter) marginCross(c *Box, row bool) float64 {
	if row {
		return c.Margin[0] + c.Margin[2]
	}
	return c.Margin[1] + c.Margin[3]
}

func count(bs ...bool) int {
	n := 0
	for _, b := range bs {
		if b {
			n++
		}
	}
	return n
}

// resolveFlexible is CSS Flexbox §9.7: it grows or shrinks a line's items into
// avail, freezing each item that hits a limit and handing its share to the rest.
func resolveFlexible(items []*item, avail, gaps float64) {
	sumHyp := gaps
	for _, it := range items {
		sumHyp += it.hyp + it.marginMain
	}
	grow := sumHyp < avail
	factor := func(it *item) float64 {
		if grow {
			return it.box.Style.Grow
		}
		return it.box.Style.Shrink
	}
	for _, it := range items {
		it.frozen = false
		it.target = it.base
		if factor(it) == 0 || (grow && it.base > it.hyp) || (!grow && it.base < it.hyp) {
			it.frozen, it.target = true, it.hyp
		}
	}
	space := func() float64 {
		used := gaps
		for _, it := range items {
			used += it.marginMain
			if it.frozen {
				used += it.target
			} else {
				used += it.base
			}
		}
		return avail - used
	}
	initial := space()

	// Every round freezes at least one item, so len(items) rounds always end
	// it; the bound is what keeps a NaN — which fails every comparison that
	// freezes — from looping for ever.
	for round := 0; round <= len(items); round++ {
		var unfrozen []*item
		sumF := 0.0
		for _, it := range items {
			if !it.frozen {
				unfrozen = append(unfrozen, it)
				sumF += factor(it)
			}
		}
		if len(unfrozen) == 0 {
			return
		}
		remaining := space()
		if sumF < 1 {
			if v := initial * sumF; math.Abs(v) < math.Abs(remaining) {
				remaining = v
			}
		}
		if grow {
			for _, it := range unfrozen {
				it.target = it.base
				if sumF > 0 {
					it.target += remaining * factor(it) / sumF
				}
			}
		} else {
			sumScaled := 0.0
			for _, it := range unfrozen {
				sumScaled += factor(it) * it.base
			}
			for _, it := range unfrozen {
				it.target = it.base
				if sumScaled > 0 {
					it.target += remaining * factor(it) * it.base / sumScaled
				}
			}
		}
		total := 0.0
		clamped := make([]float64, len(unfrozen))
		for i, it := range unfrozen {
			v := math.Max(0, clamp(it.target, it.min, it.max))
			clamped[i] = v
			total += v - it.target
		}
		froze := false
		for i, it := range unfrozen {
			v := clamped[i]
			switch {
			case total == 0,
				total > 0 && v > it.target,
				total < 0 && v < it.target:
				it.frozen, froze = true, true
			}
			it.target = v
		}
		if !froze {
			for _, it := range unfrozen {
				it.frozen = true
			}
		}
	}
	for _, it := range items {
		if math.IsNaN(it.target) {
			it.target = it.hyp
		}
	}
}

// positionLine places a line's items: along the main axis by justify-content and
// auto margins, across it by align-self and auto margins.
func positionLine(ln *line, row bool, justify string, mainSize, gap, originMain, crossAt float64) {
	free := mainSize - gap*float64(max(0, len(ln.items)-1))
	autos := 0
	for _, it := range ln.items {
		free -= it.target + it.marginMain
		autos += it.autoMain
	}

	start, between := 0.0, gap
	perAuto := 0.0
	n := float64(len(ln.items))
	switch {
	case autos > 0:
		if free > 0 {
			perAuto = free / float64(autos)
		}
	case justify == "flex-end" || justify == "end":
		start = free
	case justify == "center":
		start = free / 2
	case justify == "space-between":
		if free > 0 && n > 1 {
			between += free / (n - 1)
		}
	case justify == "space-around":
		// Without room to spare, space-around and space-evenly fall back to
		// "safe center", which overflowing is the start (CSS Box Alignment).
		if free > 0 {
			between += free / n
			start = free / n / 2
		}
	case justify == "space-evenly":
		if free > 0 {
			between += free / (n + 1)
			start = free / (n + 1)
		}
	}

	at := originMain + start
	for _, it := range ln.items {
		c := it.box
		lead, trail := c.Margin[3], c.Margin[1]
		autoLead, autoTrail := c.autoMargin[3], c.autoMargin[1]
		if !row {
			lead, trail = c.Margin[0], c.Margin[2]
			autoLead, autoTrail = c.autoMargin[0], c.autoMargin[2]
		}
		if autoLead {
			lead += perAuto
		}
		if autoTrail {
			trail += perAuto
		}
		mainPos := at + lead
		at = mainPos + it.target + trail + between

		// Across.
		size := c.H
		before, after := c.Margin[0], c.Margin[2]
		autoBefore, autoAfter := c.autoMargin[0], c.autoMargin[2]
		if !row {
			size = c.W
			before, after = c.Margin[3], c.Margin[1]
			autoBefore, autoAfter = c.autoMargin[3], c.autoMargin[1]
		}
		spare := ln.cross - size - before - after
		offset := before
		switch {
		case (autoBefore || autoAfter) && spare > 0:
			if autoBefore && autoAfter {
				offset += spare / 2
			} else if autoBefore {
				offset += spare
			}
		case c.Style.AlignSelf == "flex-end" || c.Style.AlignSelf == "end":
			offset += spare
		case c.Style.AlignSelf == "center":
			offset += spare / 2
		}
		crossPos := crossAt + offset

		if row {
			c.X, c.Y = mainPos, crossPos
		} else {
			c.X, c.Y = crossPos, mainPos
		}
	}
}
