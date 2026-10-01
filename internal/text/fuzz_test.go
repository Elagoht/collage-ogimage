package text

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Elagoht/collage-ogimage/internal/dom"
	"github.com/Elagoht/collage-ogimage/internal/layout"
)

// Whatever text a card holds — at render time, the page's data — laying it out
// finishes, and every line and box is finite.
func FuzzLayoutText(f *testing.F) {
	fixtures, _ := filepath.Glob("testdata/*.html")
	for _, p := range fixtures {
		if src, err := os.ReadFile(p); err == nil {
			f.Add(string(src))
		}
	}
	m := measurer(f)
	f.Fuzz(func(t *testing.T, src string) {
		root, errs := dom.Parse(src)
		if len(errs) > 0 || root == nil {
			return
		}
		done := make(chan *layout.Box, 1)
		go func() {
			box, _ := layout.Layout(root, m)
			done <- box
		}()
		var box *layout.Box
		select {
		case box = <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("Layout did not finish in 2s: %q", src)
		}
		var check func(*layout.Box)
		check = func(b *layout.Box) {
			for _, v := range []float64{b.X, b.Y, b.W, b.H} {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					t.Fatalf("<%s> %v,%v %v×%v in %q", b.Node.Tag, b.X, b.Y, b.W, b.H, src)
				}
			}
			if b.Node.Kind == dom.TextBlock {
				p := m.Layout(b.Node, b.Style, math.Max(0, b.W), true)
				if math.IsNaN(p.Height) || math.IsInf(p.Height, 0) {
					t.Fatalf("text height %v in %q", p.Height, src)
				}
			}
			for _, c := range b.Children {
				check(c)
			}
		}
		if box != nil {
			check(box)
		}
	})
}
