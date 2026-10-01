package layout

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Elagoht/collage-ogimage/internal/dom"
)

// Whatever valid card it is given, Layout finishes, and every box it returns has
// a finite position and a non-negative, finite size: a NaN would draw nothing,
// silently, and an infinity would never finish drawing.
func FuzzLayout(f *testing.F) {
	fixtures, _ := filepath.Glob("testdata/*.html")
	for _, p := range fixtures {
		if src, err := os.ReadFile(p); err == nil {
			f.Add(string(src))
		}
	}
	f.Fuzz(func(t *testing.T, src string) {
		root, errs := dom.Parse(src)
		if len(errs) > 0 || root == nil {
			return
		}
		done := make(chan struct{})
		var box *Box
		var err error
		go func() {
			defer close(done)
			box, err = Layout(root, noText{})
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("Layout did not finish in 2s: %q", src)
		}
		if err != nil {
			t.Fatal(err)
		}
		var check func(*Box)
		check = func(b *Box) {
			for _, v := range []float64{b.X, b.Y, b.W, b.H} {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					t.Fatalf("<%s> at %v,%v %v×%v in %q", b.Node.Tag, b.X, b.Y, b.W, b.H, src)
				}
			}
			if b.W < 0 || b.H < 0 {
				t.Fatalf("<%s> has a negative size %v×%v in %q", b.Node.Tag, b.W, b.H, src)
			}
			for _, c := range b.Children {
				check(c)
			}
		}
		check(box)
	})
}
