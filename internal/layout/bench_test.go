package layout

import (
	"strings"
	"testing"

	"github.com/Elagoht/collage-ogimage/internal/dom"
)

// nestedColumns is depth columns inside each other, each holding a box and the
// next: the shape that makes a naive layout measure the same subtree over and
// over.
func nestedColumns(depth int) string {
	open := strings.Repeat(`<div style="display:flex; flex-direction:column; padding:2px; gap:2px"><div style="height:10px"></div>`, depth)
	return `<div style="display:flex; flex-direction:column; width:1200px; height:630px">` + open + strings.Repeat(`</div>`, depth) + `</div>`
}

func BenchmarkNestedColumns(b *testing.B) {
	for _, depth := range []int{4, 8, 12} {
		root, errs := dom.Parse(nestedColumns(depth))
		if len(errs) > 0 {
			b.Fatal(errs)
		}
		b.Run(strings.Repeat("d", depth), func(b *testing.B) {
			for b.Loop() {
				if _, err := Layout(root, noText{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
