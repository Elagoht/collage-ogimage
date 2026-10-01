package text

import (
	"strings"
	"testing"

	"github.com/Elagoht/collage-ogimage/internal/dom"
	"github.com/Elagoht/collage-ogimage/internal/layout"
)

// block parses a single text block and returns it with its computed style, as
// the root of a card computes it.
func block(t *testing.T, html string) (*dom.Node, *layout.Style) {
	t.Helper()
	root, errs := dom.Parse(`<div style="display:flex; font-family:Go">` + html + `</div>`)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	box, err := layout.Layout(root, measurer(t))
	if err != nil {
		t.Fatal(err)
	}
	return box.Children[0].Node, box.Children[0].Style
}

func lineTexts(p *Paragraph) []string {
	var out []string
	for _, ln := range p.Lines {
		out = append(out, ln.Text)
	}
	return out
}

func TestTurkishUppercase(t *testing.T) {
	m := measurer(t)
	m.Locale = "tr"
	n, s := block(t, `<p style="text-transform:uppercase">istanbul ılık</p>`)
	if got := m.Layout(n, s, 1000, false).Lines[0].Text; got != "İSTANBUL ILIK" {
		t.Errorf("tr uppercase = %q", got)
	}
	m.Locale = "en"
	if got := m.Layout(n, s, 1000, false).Lines[0].Text; got != "ISTANBUL ILIK" {
		t.Errorf("en uppercase = %q", got)
	}
}

// data-fit shrinks a title 2px at a time until it fits its clamp, and never
// below half its size.
func TestDataFit(t *testing.T) {
	m := measurer(t)
	clamp := `display:-webkit-box; -webkit-box-orient:vertical; -webkit-line-clamp:2; overflow:hidden`
	n, s := block(t, `<h1 data-fit style="font-size:80px; `+clamp+`">A title long enough that at eighty pixels it takes three or four lines</h1>`)
	p := m.Layout(n, s, 1000, false)
	if p.Scale >= 1 || len(p.Lines) > 2 || strings.HasSuffix(p.Lines[len(p.Lines)-1].Text, "…") {
		t.Errorf("fit: scale %v, lines %q", p.Scale, lineTexts(p))
	}
	if size := 80 * p.Scale; int(size)%2 != 0 {
		t.Errorf("fitted size %v is not a 2px step", size)
	}
	// Without data-fit the same text is clamped instead.
	n2, s2 := block(t, `<h1 style="font-size:80px; `+clamp+`">A title long enough that at eighty pixels it takes three or four lines</h1>`)
	p2 := m.Layout(n2, s2, 1000, false)
	if p2.Scale != 1 || len(p2.Lines) != 2 || !strings.HasSuffix(p2.Lines[1].Text, "…") {
		t.Errorf("clamp: scale %v, lines %q", p2.Scale, lineTexts(p2))
	}
	// A word that cannot fit stops at half the size.
	n3, s3 := block(t, `<h1 data-fit style="font-size:80px">Supercalifragilisticexpialidocious</h1>`)
	if p3 := m.Layout(n3, s3, 100, false); p3.Scale < 0.5 {
		t.Errorf("fit went below half: %v", p3.Scale)
	}
}

func TestEllipsisFitsTheWidth(t *testing.T) {
	m := measurer(t)
	n, s := block(t, `<p style="font-size:20px; display:-webkit-box; -webkit-box-orient:vertical; -webkit-line-clamp:1; overflow:hidden">one two three four five six seven eight nine ten</p>`)
	p := m.Layout(n, s, 200, false)
	if len(p.Lines) != 1 || !strings.HasSuffix(p.Lines[0].Text, "…") || p.Lines[0].Width > 200 {
		t.Errorf("lines %q width %v", lineTexts(p), p.Lines[0].Width)
	}
	if strings.HasSuffix(strings.TrimSuffix(p.Lines[0].Text, "…"), " ") {
		t.Error("the ellipsis follows a space")
	}
}

func TestFontMatching(t *testing.T) {
	fonts := measurer(t).Fonts
	for _, c := range []struct {
		weight float64
		italic bool
		want   int
	}{{400, false, 400}, {700, false, 700}, {800, false, 700}, {300, false, 400}, {600, false, 700}, {500, false, 400}, {700, true, 700}} {
		fc := fonts.match([]string{"Go"}, c.weight, c.italic)[0]
		if fc.weight != c.want || fc.italic != c.italic {
			t.Errorf("weight %v italic %v matched %d/%v, want %d", c.weight, c.italic, fc.weight, fc.italic, c.want)
		}
	}
	if !fonts.Has("sans-serif") || !fonts.Has("monospace") || fonts.Has("Outfit") {
		t.Error("Has")
	}
}

// A character the family lacks comes from the Go fonts; one no font has is the
// first font's .notdef box.
func TestGlyphFallback(t *testing.T) {
	fonts := measurer(t).Fonts
	faces := fonts.match([]string{"Go Mono"}, 400, false)
	if fc, g := fonts.glyph(faces, 'A'); fc.family != "Go Mono" || g == 0 {
		t.Errorf("A from %s", fc.family)
	}
	if _, g := fonts.glyph(faces, '\U0001F600'); g != 0 {
		t.Error("an emoji no font has is not .notdef")
	}
}

func TestEmptyAndSpacesOnly(t *testing.T) {
	m := measurer(t)
	n, s := block(t, `<p>   </p>`)
	if p := m.Layout(n, s, 500, false); len(p.Lines) != 0 || p.Height != 0 {
		t.Errorf("spaces only: %d lines, height %v", len(p.Lines), p.Height)
	}
}

func TestNowrapEllipsisWhenDrawn(t *testing.T) {
	m := measurer(t)
	n, s := block(t, `<p style="font-size:20px; white-space:nowrap; overflow:hidden; text-overflow:ellipsis">one two three four five six seven eight</p>`)
	if p := m.Layout(n, s, 150, false); strings.HasSuffix(p.Lines[0].Text, "…") {
		t.Error("measuring cut the line")
	}
	p := m.Layout(n, s, 150, true)
	if !strings.HasSuffix(p.Lines[0].Text, "…") || p.Lines[0].Width > 150 {
		t.Errorf("drawn: %q %v", p.Lines[0].Text, p.Lines[0].Width)
	}
}
