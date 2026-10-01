package layout

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elagoht/collage-ogimage/internal/dom"
)

// noText measures nothing: phase 2's fixtures hold boxes, not text.
type noText struct{}

func (noText) MeasureText(*dom.Node, *Style, float64) (float64, float64) { return 0, 0 }
func (noText) ImageSize(*dom.Node) (float64, float64, bool)              { return 0, 0, false }

type rect struct {
	Tag        string
	X, Y, W, H float64
}

// Every fixture's boxes are within a pixel of the ones Chrome reported for the
// same HTML (DESIGN.md §12.3, phase 2). Record a fixture with
// go run ./tools/record-fixtures internal/layout/testdata.
func TestFixturesMatchChrome(t *testing.T) {
	fixtures, err := filepath.Glob("testdata/*.html")
	if err != nil || len(fixtures) == 0 {
		t.Fatal("no fixtures")
	}
	for _, fixture := range fixtures {
		name := strings.TrimSuffix(filepath.Base(fixture), ".html")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(strings.TrimSuffix(fixture, ".html") + ".json")
			if err != nil {
				t.Fatalf("%v: record the fixture", err)
			}
			var want []rect
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			root, errs := dom.Parse(string(src))
			if len(errs) > 0 {
				t.Fatalf("parse: %v", errs)
			}
			box, err := Layout(root, noText{})
			if err != nil {
				t.Fatal(err)
			}
			got := boxesInOrder(root, box)
			if len(got) != len(want) {
				t.Fatalf("%d boxes, Chrome has %d", len(got), len(want))
			}
			for i, w := range want {
				g := got[i]
				if g == nil {
					// A display:none element: Chrome reports it at zero size,
					// and the renderer draws nothing for it.
					if w.W != 0 || w.H != 0 {
						t.Errorf("box %d <%s>: hidden here, %+v in Chrome", i, w.Tag, w)
					}
					continue
				}
				if far(g.X, w.X) || far(g.Y, w.Y) || far(g.W, w.W) || far(g.H, w.H) {
					t.Errorf("box %d <%s> = %.2f,%.2f %.2f×%.2f, Chrome %.2f,%.2f %.2f×%.2f",
						i, w.Tag, g.X, g.Y, g.W, g.H, w.X, w.Y, w.W, w.H)
				}
			}
		})
	}
}

func far(a, b float64) bool { return math.Abs(a-b) > 1 }

// boxesInOrder lists every element's box in document order, as the recorder
// lists Chrome's, with nil for an element that has no box.
func boxesInOrder(root *dom.Node, box *Box) []*Box {
	byNode := map[*dom.Node]*Box{}
	var index func(*Box)
	index = func(b *Box) {
		byNode[b.Node] = b
		for _, c := range b.Children {
			index(c)
		}
	}
	index(box)
	var out []*Box
	var walk func(*dom.Node)
	walk = func(n *dom.Node) {
		if n.Kind == dom.Text {
			return
		}
		out = append(out, byNode[n])
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return out
}

// A card's root without a size is the size every network expects.
func TestTheRootDefaultsTo1200By630(t *testing.T) {
	root, _ := dom.Parse(`<div style="display:flex"></div>`)
	box, _ := Layout(root, noText{})
	if box.W != DefaultWidth || box.H != DefaultHeight {
		t.Errorf("root %v×%v", box.W, box.H)
	}
}

func TestComputedStyleDefaultsAndInheritance(t *testing.T) {
	root, errs := dom.Parse(`<div style="display:flex; font-size:20px; color:red"><h1 style="padding:1em">x</h1><small>y</small><p style="font-size:2rem">z</p></div>`)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	box, _ := Layout(root, noText{})
	h1, small, p := box.Children[0], box.Children[1], box.Children[2]
	if h1.Style.FontSize != 40 || h1.Style.FontWeight != 700 || h1.Padding[0] != 40 {
		t.Errorf("h1: size %v weight %v padding %v", h1.Style.FontSize, h1.Style.FontWeight, h1.Padding[0])
	}
	if math.Abs(small.Style.FontSize-16.6) > 1e-9 || small.Style.Color.R != 255 {
		t.Errorf("small: size %v color %v", small.Style.FontSize, small.Style.Color)
	}
	if p.Style.FontSize != 40 {
		t.Errorf("rem is the root's font size: %v", p.Style.FontSize)
	}
}
