package ogimage

import (
	"os"
	"testing"
)

// What drawing one card costs: what the first request for a card waits for.
func BenchmarkDrawPostCard(b *testing.B) {
	src, err := os.ReadFile("testdata/golden/post-card.html")
	if err != nil {
		b.Fatal(err)
	}
	r, err := newRenderer(goldenConfig().withDefaults())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := r.draw(b.Context(), string(src), "en"); err != nil {
			b.Fatal(err)
		}
	}
}
