// Command record-fixtures records, from headless Chrome, what the renderer is
// tested against: for each fixture's HTML, every element's border box.
//
//	go run ./tools/record-fixtures [-chrome path] internal/layout/testdata
//
// Each NAME.html in the directory is a card — one root element — and NAME.json is
// written beside it: the boxes of the card's elements, in document order, as
// Chrome lays them out with the renderer's default stylesheet injected. The tests
// compare boxes, not pixels: Chrome rasterizes text with Skia and hinting, the
// renderer with x/image/vector, and the two never agree to the pixel.
//
// Chrome is needed to record a fixture, not to run the tests. Re-record only when
// a fixture's HTML changes, and review the diff of its JSON.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/Elagoht/collage-ogimage/internal/layout"
	"github.com/Elagoht/collage-ogimage/internal/text"
)

// Rect is one element's border box, as getBoundingClientRect reports it, and —
// for an element holding text — the text of each line Chrome broke it into.
type Rect struct {
	Tag   string   `json:"tag"`
	X     float64  `json:"x"`
	Y     float64  `json:"y"`
	W     float64  `json:"w"`
	H     float64  `json:"h"`
	Lines []string `json:"lines,omitempty"`
}

// page wraps a card with the stylesheet, the bundled fonts as @font-face, and a
// script that reports, once the fonts have loaded, every element's box and the
// lines of every element holding text. A character starts a new line when its
// top is at or below the bottom of the line so far.
const page = `<!doctype html>
<html><head><meta charset="utf-8"><style>%s
%s</style></head>
<body>%s
<script>
function lines(e) {
  const walk = document.createTreeWalker(e, NodeFilter.SHOW_TEXT);
  const out = [];
  let cur = null, bottom = -1e9, n;
  while ((n = walk.nextNode())) {
    for (let i = 0; i < n.length; i++) {
      const range = document.createRange();
      range.setStart(n, i); range.setEnd(n, i + 1);
      const rects = range.getClientRects();
      if (!rects.length) continue;
      const r = rects[0];
      if (cur === null || r.top >= bottom - 0.5) {
        cur = {t: ""}; out.push(cur); bottom = r.bottom;
      } else {
        bottom = Math.max(bottom, r.bottom);
      }
      cur.t += n.data[i];
    }
  }
  return out.map(l => l.t.replace(/\s+/g, " ").trim()).filter(t => t !== "");
}
function holdsText(e) {
  return [...e.childNodes].some(c => c.nodeType === 3 && c.data.trim() !== "") &&
    getComputedStyle(e).display !== "flex";
}
document.fonts.ready.then(() => {
  const root = document.body.firstElementChild;
  const out = [root, ...root.querySelectorAll("*")].map(e => {
    const r = e.getBoundingClientRect();
    const o = {tag: e.tagName.toLowerCase(), x: r.x, y: r.y, w: r.width, h: r.height};
    if (holdsText(e) && !e.parentElement.closest("p,h1,h2,h3,h4,h5,h6,span,strong,b,em,i,small") ) o.lines = lines(e);
    return o;
  });
  const pre = document.createElement("pre");
  pre.id = "rects";
  pre.textContent = JSON.stringify(out);
  document.body.appendChild(pre);
});
</script></body></html>`

var rects = regexp.MustCompile(`(?s)<pre id="rects">(.*?)</pre>`)

func main() {
	chrome := flag.String("chrome", defaultChrome(), "the Chrome or Chromium binary")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: record-fixtures [-chrome path] DIR")
		os.Exit(2)
	}
	dir := flag.Arg(0)
	fixtures, err := filepath.Glob(filepath.Join(dir, "*.html"))
	if err != nil || len(fixtures) == 0 {
		fmt.Fprintf(os.Stderr, "record-fixtures: no fixtures in %s\n", dir)
		os.Exit(1)
	}
	tmp, err := os.MkdirTemp("", "record-fixtures")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(tmp)
	fonts := fontFaces(tmp)

	for _, fixture := range fixtures {
		card, err := os.ReadFile(fixture)
		if err != nil {
			fail(err)
		}
		wrapped := filepath.Join(tmp, filepath.Base(fixture))
		if err := os.WriteFile(wrapped, fmt.Appendf(nil, page, layout.Stylesheet, fonts, strings.TrimSpace(string(card))), 0o600); err != nil {
			fail(err)
		}
		dom, err := exec.Command(*chrome, "--headless=new", "--disable-gpu", "--hide-scrollbars",
			"--window-size=4000,4000", "--virtual-time-budget=10000", "--dump-dom", "file://"+wrapped).Output()
		if err != nil {
			fail(fmt.Errorf("%s: chrome: %w", fixture, err))
		}
		m := rects.FindSubmatch(dom)
		if m == nil {
			fail(fmt.Errorf("%s: chrome did not report the boxes", fixture))
		}
		var boxes []Rect
		if err := json.Unmarshal([]byte(html.UnescapeString(string(m[1]))), &boxes); err != nil {
			fail(fmt.Errorf("%s: %w", fixture, err))
		}
		out, err := json.MarshalIndent(boxes, "", "  ")
		if err != nil {
			fail(err)
		}
		target := strings.TrimSuffix(fixture, ".html") + ".json"
		if err := os.WriteFile(target, append(out, '\n'), 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("%s: %d boxes\n", target, len(boxes))
	}
}

// fontFaces writes the bundled fonts into dir and returns @font-face rules for
// them, so Chrome draws a fixture's text in the very files the renderer does.
func fontFaces(dir string) string {
	var css strings.Builder
	for i, f := range text.Bundled {
		path := filepath.Join(dir, fmt.Sprintf("font%d.ttf", i))
		if err := os.WriteFile(path, f.Data, 0o600); err != nil {
			fail(err)
		}
		style := "normal"
		if f.Italic {
			style = "italic"
		}
		fmt.Fprintf(&css, "@font-face { font-family: %q; font-weight: %d; font-style: %s; font-display: block; src: url(%q); }\n",
			f.Family, f.Weight, style, "file://"+path)
	}
	return css.String()
}

func defaultChrome() string {
	if runtime.GOOS == "darwin" {
		return "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return "google-chrome"
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "record-fixtures:", err)
	os.Exit(1)
}
