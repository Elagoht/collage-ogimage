// Package dom parses a card's HTML — the subset of DESIGN.md §6.1 — into a tree of
// nodes with their parsed styles, and refuses what the renderer does not draw,
// naming the line and column of each problem.
//
// Every element is one of two kinds. A flex container declares display:flex and
// holds boxes; a text block declares no display, or the line-clamp idiom, and holds
// only text and text-level runs. An element with a box among its children and no
// display:flex is refused: a browser would lay it out in block flow, which the
// renderer does not draw.
package dom

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Elagoht/collage-ogimage/internal/css"
	"golang.org/x/net/html"
)

// Kind is what a node is, once its children are known.
type Kind uint8

const (
	// Container is a flex container: display:flex, children laid out as boxes.
	Container Kind = iota
	// TextBlock is a paragraph: text and runs, wrapped together.
	TextBlock
	// Run is a text-level element inside a text block: span, strong, b, em, i,
	// small.
	Run
	// Text is a run of characters.
	Text
	// Image is an img.
	Image
	// Break is a br inside a text block.
	Break
	// Hidden is an element with display:none, drawn as nothing.
	Hidden
)

// Node is one element or one run of text.
type Node struct {
	Kind Kind
	// Tag is the element's name; empty for Text.
	Tag string
	// Text is a Text node's characters, with entities decoded.
	Text string
	// Style is the element's declarations, in order; a later one of a property
	// overrides an earlier.
	Style []css.Declaration
	// Src and Alt are an img's.
	Src, Alt string
	// Fit is set by the data-fit attribute.
	Fit bool
	// Children are the node's, in document order. Whitespace-only text between
	// a flex container's children is dropped, as a browser drops it.
	Children []*Node
	// Line and Col are where the node starts, 1-based; Col counts characters.
	Line, Col int
}

// Last returns the last declaration of property, if the node has one.
func (n *Node) Last(property string) (css.Declaration, bool) {
	for i := len(n.Style) - 1; i >= 0; i-- {
		if n.Style[i].Property == property {
			return n.Style[i], true
		}
	}
	return css.Declaration{}, false
}

// keyword returns the node's last keyword value for property, or "".
func (n *Node) keyword(property string) string {
	d, ok := n.Last(property)
	if !ok {
		return ""
	}
	if k, ok := d.Value.(css.Keyword); ok {
		return string(k)
	}
	return ""
}

// Error is one thing the renderer cannot draw, at a position in the card's HTML.
type Error struct {
	Line, Col int
	Msg       string
}

func (e Error) Error() string { return fmt.Sprintf("%d:%d: %s", e.Line, e.Col, e.Msg) }

// Errors is every problem found in one card, in document order.
type Errors []Error

func (es Errors) Error() string {
	msgs := make([]string, len(es))
	for i, e := range es {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "\n")
}

var (
	containers = []string{"div", "section", "header", "footer", "main", "article"}
	textBlocks = []string{"p", "h1", "h2", "h3", "h4", "h5", "h6"}
	runs       = []string{"span", "strong", "b", "em", "i", "small"}
	attributes = []string{"style", "src", "alt", "data-fit", "class"}
)

// Parse reads a card's HTML: one root element, and nothing but whitespace and
// comments around it. It returns the tree, and every problem found, together.
func Parse(src string) (*Node, Errors) {
	p := &parser{src: src, lines: lineStarts(src)}
	root := p.parse()
	if root != nil {
		p.check(root, nil)
	}
	slices.SortStableFunc(p.errs, func(a, b Error) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return a.Col - b.Col
	})
	return root, p.errs
}

type parser struct {
	src   string
	lines []int
	errs  Errors
}

func (p *parser) fail(offset int, msg string) {
	line, col := p.position(offset)
	p.errs = append(p.errs, Error{Line: line, Col: col, Msg: msg})
}

func lineStarts(s string) []int {
	starts := []int{0}
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// position turns a byte offset into a 1-based line and a 1-based column counted
// in characters.
func (p *parser) position(offset int) (int, int) {
	line, _ := slices.BinarySearch(p.lines, offset+1)
	start := p.lines[line-1]
	return line, utf8.RuneCountInString(p.src[start:offset]) + 1
}

// parse builds the tree from the tokenizer's tokens, tracking each token's
// offset.
func (p *parser) parse() *Node {
	z := html.NewTokenizer(strings.NewReader(p.src))
	var root *Node
	var stack []*Node
	offset := 0
	for {
		tt := z.Next()
		raw := z.Raw()
		at := offset
		offset += len(raw)
		switch tt {
		case html.ErrorToken:
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				p.fail(p.lines[top.Line-1], fmt.Sprintf("<%s> is not closed", top.Tag))
			}
			if root == nil {
				p.fail(0, "a card is one root element, and there is none")
			}
			return root
		case html.CommentToken:
			continue
		case html.DoctypeToken:
			p.fail(at, "a card is an element, not a document: remove the doctype")
			continue
		case html.TextToken:
			text := html.UnescapeString(string(raw))
			if len(stack) == 0 {
				if strings.TrimSpace(text) != "" {
					p.fail(at, "text outside the root element")
				}
				continue
			}
			n := p.node(at)
			n.Kind, n.Text = Text, text
			top := stack[len(stack)-1]
			top.Children = append(top.Children, n)
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			n := p.element(at, string(raw), string(name))
			switch {
			case len(stack) > 0:
				top := stack[len(stack)-1]
				top.Children = append(top.Children, n)
			case root == nil:
				root = n
			default:
				p.fail(at, fmt.Sprintf("a card is one root element: <%s> is a second one", n.Tag))
			}
			if tt == html.StartTagToken && n.Tag != "img" && n.Tag != "br" {
				stack = append(stack, n)
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)
			if tag == "img" || tag == "br" {
				continue
			}
			if len(stack) == 0 || stack[len(stack)-1].Tag != tag {
				p.fail(at, fmt.Sprintf("</%s> closes nothing that is open", tag))
				continue
			}
			stack = stack[:len(stack)-1]
		}
	}
}

func (p *parser) node(offset int) *Node {
	line, col := p.position(offset)
	return &Node{Line: line, Col: col}
}

// element makes a node of a start tag, reading its attributes from the raw tag
// so each one's position is known.
func (p *parser) element(at int, raw, tag string) *Node {
	n := p.node(at)
	n.Tag = strings.ToLower(tag)
	known := slices.Contains(containers, n.Tag) || slices.Contains(textBlocks, n.Tag) ||
		slices.Contains(runs, n.Tag) || n.Tag == "img" || n.Tag == "br"
	if !known {
		switch n.Tag {
		case "style":
			p.fail(at, "<style> is not supported: a card's styles are inline, in style attributes")
		case "svg":
			p.fail(at, "<svg> is not supported in v0.1: use an <img> of a PNG, JPEG or WebP")
		case "a", "button", "ul", "ol", "li", "table":
			p.fail(at, fmt.Sprintf("<%s> is not supported: a card is drawn, not used; use a <div> or a <span>", n.Tag))
		default:
			p.fail(at, fmt.Sprintf("<%s> is not supported: a card is made of div, section, header, footer, main, article, p, h1–h6, span, strong, b, em, i, small, img and br", n.Tag))
		}
	}
	for _, a := range scanAttributes(raw) {
		name := strings.ToLower(a.name)
		value := html.UnescapeString(a.value)
		switch name {
		case "style":
			decls, errs := css.Parse(a.value)
			n.Style = append(n.Style, decls...)
			for _, e := range errs {
				p.fail(at+a.valueAt+e.Offset, fmt.Sprintf("<%s> style: %s", n.Tag, e.Error()))
			}
		case "src":
			n.Src = value
		case "alt":
			n.Alt = value
		case "data-fit":
			n.Fit = true
		case "class":
			// Accepted and ignored, so markup copied from a page does not
			// fail on it.
		default:
			p.fail(at+a.nameAt, fmt.Sprintf("<%s> attribute %q is not supported: a card's elements take %s", n.Tag, name, strings.Join(attributes, ", ")))
		}
	}
	return n
}

// check settles each node's kind from its tag, its display and its children, and
// refuses what breaks the two kinds. parent is nil for the root.
func (p *parser) check(n *Node, parent *Node) {
	at := p.lines[n.Line-1] + byteCol(p.src[p.lines[n.Line-1]:], n.Col)
	display := n.keyword("display")

	switch n.Tag {
	case "img":
		n.Kind = Image
		if n.Src == "" {
			p.fail(at, "<img> has no src")
		}
		p.checkStyle(n, at)
		return
	case "br":
		n.Kind = Break
		return
	}

	if display == "none" {
		n.Kind = Hidden
		p.checkStyle(n, at)
		return
	}

	// Whitespace between a flex container's children is not a child, as in a
	// browser; inside a text block it is a space.
	if display == "flex" {
		n.Children = slices.DeleteFunc(n.Children, func(c *Node) bool {
			return c.Kind == Text && strings.TrimFunc(c.Text, unicode.IsSpace) == ""
		})
	}

	switch {
	case display == "flex":
		n.Kind = Container
		if slices.Contains(runs, n.Tag) && parent != nil && parent.Kind != Container {
			p.fail(at, fmt.Sprintf("<%s> with display:flex is a box, and it sits inside text", n.Tag))
		}
	case slices.Contains(runs, n.Tag) && parent != nil && (parent.Kind == TextBlock || parent.Kind == Run):
		n.Kind = Run
	default:
		n.Kind = TextBlock
	}

	if n.Kind != Container {
		for _, c := range n.Children {
			if c.Kind == Text {
				continue
			}
			if c.Tag == "br" {
				c.Kind = Break
				continue
			}
			if slices.Contains(runs, c.Tag) && c.keyword("display") != "flex" {
				continue
			}
			line, col := c.Line, c.Col
			p.errs = append(p.errs, Error{Line: line, Col: col, Msg: fmt.Sprintf(
				"<%s> is a box inside <%s>, which holds text: give <%s> display:flex to lay its children out, or keep only text and span, strong, b, em, i, small and br in it",
				c.Tag, n.Tag, n.Tag)})
		}
	}

	p.checkStyle(n, at)
	for _, c := range n.Children {
		if c.Kind == Text {
			continue
		}
		p.check(c, n)
	}
}

// checkStyle refuses the declarations that only mean something in a combination,
// or on a kind of element, where they appear outside it.
func (p *parser) checkStyle(n *Node, at int) {
	_, clamp := n.Last("-webkit-line-clamp")
	display := n.keyword("display")
	if display == "-webkit-box" || clamp {
		missing := []string{}
		if display != "-webkit-box" {
			missing = append(missing, "display: -webkit-box")
		}
		if n.keyword("-webkit-box-orient") != "vertical" {
			missing = append(missing, "-webkit-box-orient: vertical")
		}
		if !clamp {
			missing = append(missing, "-webkit-line-clamp: N")
		}
		if n.keyword("overflow") != "hidden" {
			missing = append(missing, "overflow: hidden")
		}
		if len(missing) > 0 {
			p.fail(at, fmt.Sprintf("<%s> clamps lines only with all four of display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: N; overflow: hidden — add %s",
				n.Tag, strings.Join(missing, "; ")))
		}
		if n.Kind == Container || n.Kind == Image {
			p.fail(at, fmt.Sprintf("<%s> is not text, and only text is clamped", n.Tag))
		}
	} else if _, ok := n.Last("-webkit-box-orient"); ok {
		p.fail(at, fmt.Sprintf("<%s> -webkit-box-orient means something only in the line-clamp idiom", n.Tag))
	}

	if n.keyword("text-overflow") == "ellipsis" && (n.keyword("white-space") != "nowrap" || n.keyword("overflow") != "hidden") {
		p.fail(at, fmt.Sprintf("<%s> text-overflow: ellipsis shows only with white-space: nowrap and overflow: hidden", n.Tag))
	}

	if n.Kind != Container {
		for _, prop := range []string{"flex-direction", "flex-wrap", "justify-content", "align-items", "gap", "row-gap", "column-gap"} {
			if _, ok := n.Last(prop); ok {
				p.fail(at, fmt.Sprintf("<%s> %s lays out a flex container's children, and <%s> is not one: add display:flex", n.Tag, prop, n.Tag))
			}
		}
	}
	if n.Kind != Image {
		for _, prop := range []string{"object-fit", "object-position"} {
			if _, ok := n.Last(prop); ok {
				p.fail(at, fmt.Sprintf("<%s> %s applies to an <img>", n.Tag, prop))
			}
		}
	}
}

// byteCol turns a 1-based character column on a line back into a byte offset.
func byteCol(line string, col int) int {
	offset := 0
	for i := 1; i < col && offset < len(line); i++ {
		_, size := utf8.DecodeRuneInString(line[offset:])
		offset += size
	}
	return offset
}
