package dom

import (
	"strings"
	"testing"

	"github.com/Elagoht/collage-ogimage/internal/css"
)

// The post card from README.md and DESIGN.md §3.1, executed: what the
// documentation shows must be what the renderer accepts.
const postCard = `<div style="display:flex; flex-direction:column; justify-content:space-between;
            width:1200px; height:630px; padding:72px;
            background:linear-gradient(135deg, #0f172a, #1e293b); color:#f8fafc;
            font-family:Outfit">
  <div style="display:flex; align-items:center; gap:16px">
    <img src="/static/icons/logo.png" style="width:56px; height:56px; border-radius:12px">
    <span style="font-size:28px; color:#94a3b8">furkanbaytekin.dev</span>
  </div>

  <h1 data-fit style="font-size:72px; font-weight:800; line-height:1.1;
                      display:-webkit-box; -webkit-box-orient:vertical;
                      -webkit-line-clamp:3; overflow:hidden">Building a framework, one fragment at a time</h1>

  <div style="display:flex; gap:24px; font-size:28px; color:#94a3b8">
    <span>Software</span>
    <span>2 October 2026</span>
  </div>
</div>`

// The default card from README.md.
const defaultCard = `<div style="display:flex; flex-direction:column; justify-content:center; gap:24px;
            width:1200px; height:630px; padding:96px; background:#ffffff; color:#0f172a">
  <span style="font-size:32px; color:#64748b">example.com</span>
  <h1 data-fit style="font-size:80px; font-weight:800; display:-webkit-box;
                      -webkit-box-orient:vertical; -webkit-line-clamp:2; overflow:hidden">About</h1>
  <p style="font-size:32px; color:#475569">Who I am &amp; what I build.</p>
</div>`

func parseOK(t *testing.T, src string) *Node {
	t.Helper()
	root, errs := Parse(src)
	if len(errs) > 0 {
		t.Fatalf("Parse: %v", errs)
	}
	return root
}

func TestTheDocumentedCardsParse(t *testing.T) {
	root := parseOK(t, postCard)
	if root.Kind != Container || len(root.Children) != 3 {
		t.Fatalf("root = %v with %d children, want a container of 3 (whitespace dropped)", root.Kind, len(root.Children))
	}
	header, title, footer := root.Children[0], root.Children[1], root.Children[2]
	if header.Kind != Container || header.Children[0].Kind != Image || header.Children[1].Kind != TextBlock {
		t.Errorf("header: %v, %v, %v", header.Kind, header.Children[0].Kind, header.Children[1].Kind)
	}
	if title.Kind != TextBlock || !title.Fit || title.Children[0].Text != "Building a framework, one fragment at a time" {
		t.Errorf("title: %+v", title)
	}
	if d, ok := title.Last("-webkit-line-clamp"); !ok || d.Value != css.Number(3) {
		t.Errorf("title clamp: %+v", d)
	}
	if footer.Kind != Container || len(footer.Children) != 2 {
		t.Errorf("footer: %v with %d", footer.Kind, len(footer.Children))
	}

	def := parseOK(t, defaultCard)
	if p := def.Children[2]; p.Tag != "p" || p.Children[0].Text != "Who I am & what I build." {
		t.Errorf("an entity was not decoded: %q", p.Children[0].Text)
	}
}

// A text block wraps across the runs inside it; a run is not a box.
func TestInlineRunsAreText(t *testing.T) {
	root := parseOK(t, `<p>a <b>modular</b> monolith, <em>with <strong>nested</strong> runs</em><br>and a break</p>`)
	if root.Kind != TextBlock {
		t.Fatalf("p = %v", root.Kind)
	}
	kinds := []Kind{}
	for _, c := range root.Children {
		kinds = append(kinds, c.Kind)
	}
	want := []Kind{Text, Run, Text, Run, Break, Text}
	if len(kinds) != len(want) {
		t.Fatalf("children %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("child %d = %v, want %v", i, kinds[i], want[i])
		}
	}
	if root.Children[3].Children[1].Kind != Run {
		t.Error("a run inside a run is not a run")
	}
	// Whitespace inside a text block is a space, kept.
	if root.Children[0].Text != "a " {
		t.Errorf("text %q", root.Children[0].Text)
	}
}

// Text directly in a flex container is a box of its own, as in a browser.
func TestTextInAContainer(t *testing.T) {
	root := parseOK(t, `<div style="display:flex">Hello <span>world</span></div>`)
	if len(root.Children) != 2 || root.Children[0].Kind != Text || root.Children[1].Kind != TextBlock {
		t.Errorf("children: %+v", root.Children)
	}
}

func TestHiddenIsNothing(t *testing.T) {
	root := parseOK(t, `<div style="display:flex"><div style="display:none"><img></div></div>`)
	if root.Children[0].Kind != Hidden {
		t.Errorf("display:none = %v", root.Children[0].Kind)
	}
}

// Each problem is named at its own line and column, and all of them are
// reported together.
func TestErrorsNameTheirPosition(t *testing.T) {
	src := "<div style=\"display:flex\">\n" + // line 1
		"  <div style=\"position: absolute\">x</div>\n" + // line 2: col of "position" is 15
		"  <span onclick=\"x\">y</span>\n" + // line 3: col of onclick is 9
		"  <svg></svg>\n" + // line 4
		"  <p>title <div style=\"display:flex\">box</div></p>\n" + // line 5: inner div at col 12
		"</div>"
	_, errs := Parse(src)
	want := []struct {
		line, col int
		msg       string
	}{
		{2, 15, "position"},
		{3, 9, `attribute "onclick"`},
		{4, 3, "<svg>"},
		{5, 12, "is a box inside <p>"},
	}
	if len(errs) != len(want) {
		t.Fatalf("%d errors, want %d:\n%v", len(errs), len(want), errs)
	}
	for i, w := range want {
		e := errs[i]
		if e.Line != w.line || e.Col != w.col || !strings.Contains(e.Msg, w.msg) {
			t.Errorf("error %d = %v, want %d:%d %q", i, e, w.line, w.col, w.msg)
		}
	}
}

// Columns count characters, so a Turkish line points where an editor does.
func TestColumnsCountCharacters(t *testing.T) {
	src := `<p>Günaydın <span style="float: left">x</span></p>`
	_, errs := Parse(src)
	if len(errs) != 1 {
		t.Fatalf("%v", errs)
	}
	if want := strings.Index(src, "float"); errs[0].Col != len([]rune(src[:want]))+1 {
		t.Errorf("column %d, want %d", errs[0].Col, len([]rune(src[:want]))+1)
	}
}

func TestTheTwoKinds(t *testing.T) {
	for src, wantInMsg := range map[string]string{
		`<div><div>a</div><div>b</div></div>`:          "give <div> display:flex",
		`<div><img src="a.png"></div>`:                 "<img> is a box inside <div>",
		`<h1>Title <p>sub</p></h1>`:                    "<p> is a box inside <h1>",
		`<p>a <span style="display:flex">b</span></p>`: "is a box inside <p>",
		`<div style="display:flex; "><b>a</b></div>`:   "", // a run in a container is a text block: fine
		`<span style="gap: 4px">x</span>`:              "add display:flex",
		`<div style="justify-content:center">x</div>`:  "add display:flex",
		`<p style="object-fit: cover">x</p>`:           "applies to an <img>",
	} {
		_, errs := Parse(src)
		if wantInMsg == "" {
			if len(errs) > 0 {
				t.Errorf("%s: %v", src, errs)
			}
			continue
		}
		if len(errs) == 0 || !strings.Contains(errs.Error(), wantInMsg) {
			t.Errorf("%s: %v, want %q", src, errs, wantInMsg)
		}
	}
}

// Clamping is the four declarations a browser needs, together, on text.
func TestTheClampIdiom(t *testing.T) {
	ok := `display:-webkit-box; -webkit-box-orient:vertical; -webkit-line-clamp:2; overflow:hidden`
	parseOK(t, `<p style="`+ok+`">a long title</p>`)
	for style, wantInMsg := range map[string]string{
		`-webkit-line-clamp:2`: "add display: -webkit-box; -webkit-box-orient: vertical; overflow: hidden",
		`display:-webkit-box; -webkit-line-clamp:2; overflow:hidden`:        "add -webkit-box-orient: vertical",
		`display:-webkit-box; -webkit-box-orient:vertical; overflow:hidden`: "add -webkit-line-clamp: N",
		`-webkit-box-orient: vertical`:                                      "only in the line-clamp idiom",
		`text-overflow: ellipsis`:                                           "white-space: nowrap and overflow: hidden",
	} {
		_, errs := Parse(`<p style="` + style + `">x</p>`)
		if len(errs) == 0 || !strings.Contains(errs.Error(), wantInMsg) {
			t.Errorf("%s: %v, want %q", style, errs, wantInMsg)
		}
	}
	parseOK(t, `<p style="white-space:nowrap; overflow:hidden; text-overflow:ellipsis">x</p>`)
}

func TestDocumentShape(t *testing.T) {
	for src, wantInMsg := range map[string]string{
		``:                                     "there is none",
		`   `:                                  "there is none",
		`<div>a</div><div>b</div>`:             "second one",
		`stray <div>a</div>`:                   "text outside",
		`<!doctype html><div>a</div>`:          "doctype",
		`<div style="display:flex"><p>a</div>`: "closes nothing",
		`<div>a`:                               "not closed",
		`<style>p{}</style>`:                   "inline",
		`<a href="/">x</a>`:                    "drawn, not used",
		`<img>`:                                "no src",
	} {
		_, errs := Parse(src)
		if len(errs) == 0 || !strings.Contains(errs.Error(), wantInMsg) {
			t.Errorf("%q: %v, want %q", src, errs, wantInMsg)
		}
	}
	// Comments are ignored, and class is accepted and ignored.
	parseOK(t, `<!-- a card --><div class="card" style="display:flex"><!-- x --><span>a</span></div>`)
}

// A template action, masked, is text where it stands for text and a dynamic
// value where it stands in a style.
func TestMaskedTemplateActions(t *testing.T) {
	m := strings.Repeat(string(css.Masked), 8)
	root := parseOK(t, `<div style="display:flex; color:`+m+`"><h1>`+m+`</h1>`+m+`</div>`)
	if d, _ := root.Last("color"); !d.Dynamic {
		t.Error("a masked colour is not dynamic")
	}
	if root.Children[0].Children[0].Text != m {
		t.Error("masked text was not kept as text")
	}
}
