package dom

import "testing"

// Whatever a card's HTML holds — and at render time it holds the page's data —
// Parse returns a tree and errors at positions inside it; it never panics.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		postCard, defaultCard, `<p>a <b>b</b><br>c</p>`, `<div style="display:flex"><img src=x></div>`,
		`<div`, `<p style="`, `</p>`, `<div style='display:flex' data-fit><!--`, "<p>ğüşiöç</p>",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_, errs := Parse(s)
		lines := len(lineStarts(s))
		for _, e := range errs {
			if e.Line < 1 || e.Line > lines || e.Col < 1 {
				t.Fatalf("error at %d:%d in %q", e.Line, e.Col, s)
			}
		}
	})
}
