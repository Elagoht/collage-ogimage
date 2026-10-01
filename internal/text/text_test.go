package text

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Elagoht/collage-ogimage/internal/dom"
	"github.com/Elagoht/collage-ogimage/internal/layout"
)

type rect struct {
	Tag        string
	X, Y, W, H float64
	Lines      []string
}

func measurer(t testing.TB) *Measurer {
	t.Helper()
	fonts, err := NewFonts(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &Measurer{Fonts: fonts, Locale: "en"}
}

func normalize(s string) string { return strings.Join(strings.Fields(s), " ") }

// Every text fixture's boxes are within a pixel of Chrome's, and every text
// block breaks into the lines Chrome breaks it into (DESIGN.md §12.3, phase 3).
func TestTextFixturesMatchChrome(t *testing.T) {
	fixtures, _ := filepath.Glob("testdata/*.html")
	if len(fixtures) == 0 {
		t.Fatal("no fixtures")
	}
	m := measurer(t)
	for _, fixture := range fixtures {
		name := strings.TrimSuffix(filepath.Base(fixture), ".html")
		t.Run(name, func(t *testing.T) {
			src, _ := os.ReadFile(fixture)
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
			box, err := layout.Layout(root, m)
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
					continue
				}
				if far(g.X, w.X) || far(g.Y, w.Y) || far(g.W, w.W) || far(g.H, w.H) {
					t.Errorf("box %d <%s> = %.2f,%.2f %.2f×%.2f, Chrome %.2f,%.2f %.2f×%.2f",
						i, w.Tag, g.X, g.Y, g.W, g.H, w.X, w.Y, w.W, w.H)
				}
				if w.Lines == nil || g.Node.Kind != dom.TextBlock {
					continue
				}
				fw := g.Padding[1] + g.Padding[3] + 2*g.Style.Border
				p := m.Layout(g.Node, g.Style, g.W-fw, false)
				var lines []string
				for _, ln := range p.Lines {
					if s := normalize(ln.Text); s != "" {
						lines = append(lines, s)
					}
				}
				compareLines(t, i, w.Tag, lines, w.Lines, g.Style.Clamp)
			}
		})
	}
}

func compareLines(t *testing.T, i int, tag string, got, want []string, clamp int) {
	t.Helper()
	if clamp > 0 && len(want) > clamp {
		// Chrome lays out the lines a clamp hides; the renderer stops at the
		// clamp and ends its last line with an ellipsis.
		if len(got) != clamp {
			t.Errorf("box %d <%s>: %d lines, want the clamp's %d", i, tag, len(got), clamp)
			return
		}
		for k := 0; k < clamp-1; k++ {
			if got[k] != want[k] {
				t.Errorf("box %d <%s> line %d = %q, Chrome %q", i, tag, k, got[k], want[k])
			}
		}
		last := strings.TrimSuffix(got[clamp-1], "…")
		if !strings.HasSuffix(got[clamp-1], "…") || !strings.HasPrefix(want[clamp-1], strings.TrimSpace(last)) {
			t.Errorf("box %d <%s> clamped line = %q, Chrome's line %q", i, tag, got[clamp-1], want[clamp-1])
		}
		return
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("box %d <%s> lines:\n  got   %q\n  Chrome %q", i, tag, got, want)
	}
}

func far(a, b float64) bool { return math.Abs(a-b) > 1 }

func boxesInOrder(root *dom.Node, box *layout.Box) []*layout.Box {
	byNode := map[*dom.Node]*layout.Box{}
	var index func(*layout.Box)
	index = func(b *layout.Box) {
		byNode[b.Node] = b
		for _, c := range b.Children {
			index(c)
		}
	}
	index(box)
	var out []*layout.Box
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
