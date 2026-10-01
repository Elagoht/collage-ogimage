package css

import "testing"

// Whatever a style holds, Parse returns declarations and errors; it never panics.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"display:flex; gap: 8px", "color: rgb(1 2 3 / 50%)", "background: linear-gradient(to top right, red 10%, #0008)",
		"font-family: 'a', b c, serif", "border: 1px solid hsl(10deg 20% 30%)", "padding: 1px 2px 3px 4px 5px",
		"x:(", "a:'", "color: rgb(", ";;;", "flex: 1 2 3 4", "background: url(", "--x: y",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		decls, errs := Parse(s)
		for _, d := range decls {
			if d.Offset < 0 || d.Offset > len(s) {
				t.Fatalf("offset %d outside %q", d.Offset, s)
			}
		}
		for _, e := range errs {
			if e.Offset < 0 || e.Offset > len(s) {
				t.Fatalf("error offset %d outside %q", e.Offset, s)
			}
		}
	})
}
