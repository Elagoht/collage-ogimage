package ogimage

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Elagoht/collage-ogimage/internal/css"
)

// The post card as README.md documents it, unexecuted: it must pass as written.
const postTemplate = `<div style="display:flex; flex-direction:column; justify-content:space-between;
            width:1200px; height:630px; padding:72px;
            background:linear-gradient(135deg, #0f172a, #1e293b); color:#f8fafc;
            font-family:Outfit">
  <div style="display:flex; align-items:center; gap:16px">
    <img src="/static/icons/logo.png" style="width:56px; height:56px; border-radius:12px">
    <span style="font-size:28px; color:#94a3b8">{{.Site.Name}}</span>
  </div>

  <h1 data-fit style="font-size:72px; font-weight:800; line-height:1.1;
                      display:-webkit-box; -webkit-box-orient:vertical;
                      -webkit-line-clamp:3; overflow:hidden">{{.Title}}</h1>

  <div style="display:flex; gap:24px; font-size:28px; color:#94a3b8">
    <span>{{.Label}}</span>
    <span>{{.Fields.date}}</span>
  </div>
</div>`

func TestTheDocumentedTemplatePasses(t *testing.T) {
	if err := checkTemplate("og/post.html", postTemplate); err != nil {
		t.Fatal(err)
	}
}

// A comment produces nothing, so a template may open with one, and hold one
// between a flex container's children.
func TestCommentsAreNotText(t *testing.T) {
	src := "{{/* The share card.\n   Two lines. */}}\n" +
		`<div style="display:flex">{{- /* trimmed */ -}}<span>{{.Title}}</span></div>`
	if err := checkTemplate("og/post.html", src); err != nil {
		t.Fatal(err)
	}
}

// Masking keeps the template's lines and columns: an error after an action is
// reported where an editor shows it.
func TestMaskKeepsPositions(t *testing.T) {
	src := "<p>{{.Title}} and {{if .X}}\n{{.Y}}{{end}}</p>"
	m := mask(src)
	if utf8.RuneCountInString(m) != utf8.RuneCountInString(src) || strings.Count(m, "\n") != 1 {
		t.Fatalf("mask changed the shape: %q", m)
	}
	if strings.Contains(m, "{{") || !strings.Contains(m, " and ") || !strings.HasPrefix(m, "<p>") {
		t.Errorf("mask = %q", m)
	}
	for _, r := range []rune(m)[3:13] {
		if r != css.Masked {
			t.Fatalf("the action was not masked: %q", m)
		}
	}
	// An action holding "}}" inside a string, and a comment holding one, end
	// where text/template ends them.
	for _, s := range []string{`{{printf "}}"}}x`, "{{`}}`}}x", `{{/* }} */}}x`} {
		if got := []rune(mask(s)); got[len(got)-1] != 'x' || strings.ContainsRune(string(got[:len(got)-1]), 'x') {
			t.Errorf("mask(%q) = %q", s, string(got))
		}
	}
}

func TestTemplateErrorsNameTheTemplateLineAndColumn(t *testing.T) {
	src := "<div style=\"display:flex\">\n" +
		"  <h1 style=\"color: {{.Color}}; position: relative\">{{.Title}}</h1>\n" +
		"</div>"
	err := checkTemplate("og/post.html", src)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
	var te *TemplateError
	if !errors.As(err, &te) {
		t.Fatalf("err = %T", err)
	}
	col := strings.Index(strings.Split(src, "\n")[1], "position") + 1
	if te.Template != "og/post.html" || te.Line != 2 || te.Col != col {
		t.Errorf("error at %s:%d:%d, want og/post.html:2:%d", te.Template, te.Line, te.Col, col)
	}
	if want := "og/post.html:2:"; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("message %q", err.Error())
	}
}

// Every problem in a template is reported at once, not one per restart.
func TestEveryProblemIsReported(t *testing.T) {
	err := checkTemplate("og/x.html", `<div><svg></svg><p style="float:left; transform: none">x</p></div>`)
	if n := strings.Count(err.Error(), "og/x.html:"); n < 4 {
		t.Errorf("%d problems reported, want at least 4:\n%v", n, err)
	}
}

// A template whose structure comes from actions — a range of runs, a branch — is
// valid as long as what it writes literally is.
func TestStructureFromActions(t *testing.T) {
	for _, src := range []string{
		`<p>{{range .Fields}}<b>{{.}}</b> {{end}}</p>`,
		`<div style="display:flex">{{if .Image}}<img src="{{.Image}}" style="width:64px">{{end}}<span>{{.Title}}</span></div>`,
		`<div style="display:flex; background:{{.Fields.bg}}"><p>{{.Title}}</p></div>`,
	} {
		if err := checkTemplate("og/t.html", src); err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}
}
