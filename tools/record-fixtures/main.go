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
)

// Rect is one element's border box, as getBoundingClientRect reports it.
type Rect struct {
	Tag string  `json:"tag"`
	X   float64 `json:"x"`
	Y   float64 `json:"y"`
	W   float64 `json:"w"`
	H   float64 `json:"h"`
}

const page = `<!doctype html>
<html><head><meta charset="utf-8"><style>%s</style></head>
<body>%s
<script>
const root = document.body.firstElementChild;
const out = [root, ...root.querySelectorAll("*")].map(e => {
  const r = e.getBoundingClientRect();
  return {tag: e.tagName.toLowerCase(), x: r.x, y: r.y, w: r.width, h: r.height};
});
const pre = document.createElement("pre");
pre.id = "rects";
pre.textContent = JSON.stringify(out);
document.body.appendChild(pre);
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

	for _, fixture := range fixtures {
		card, err := os.ReadFile(fixture)
		if err != nil {
			fail(err)
		}
		wrapped := filepath.Join(tmp, filepath.Base(fixture))
		if err := os.WriteFile(wrapped, fmt.Appendf(nil, page, layout.Stylesheet, strings.TrimSpace(string(card))), 0o600); err != nil {
			fail(err)
		}
		dom, err := exec.Command(*chrome, "--headless=new", "--disable-gpu", "--hide-scrollbars",
			"--window-size=4000,4000", "--dump-dom", "file://"+wrapped).Output()
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
