package ogimage

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	meta "github.com/Elagoht/collage-meta"
	"github.com/Elagoht/collage/pkg/collage"
	"github.com/Elagoht/collage/pkg/collagetest"
)

const testPostCard = `<div style="display:flex; flex-direction:column; justify-content:space-between; width:1200px; height:630px; padding:72px; background:#0f172a; color:#fff; font-family:Go">
  <span style="font-size:28px">{{.Site.Name}}</span>
  <h1 data-fit style="font-size:72px">{{.Title}}</h1>
  <span style="font-size:28px">{{.Label}} · {{.Fields.date}}</span>
</div>`

const testDefaultCard = `<div style="display:flex; flex-direction:column; justify-content:center; gap:24px; width:1200px; height:630px; padding:96px; background:#fff; color:#0f172a; font-family:Go">
  <h1 style="font-size:80px">{{.Title}}</h1>
  <p style="font-size:32px">{{.Description}}</p>
</div>`

var templates = fstest.MapFS{
	"t/layout.html":      {Data: []byte(`<!doctype html><html><head>{{hoist "head"}}</head><body>{{slot "content"}}</body></html>`)},
	"t/page.html":        {Data: []byte(`<p>{{.}}</p>`)},
	"t/og/post.html":     {Data: []byte(testPostCard)},
	"t/og/default.html":  {Data: []byte(testDefaultCard)},
	"t/og/broken.html.x": {Data: []byte(`not a card`)},
}

type site struct {
	app  *collage.App
	og   *Plugin
	sets *atomic.Int32
}

// newSite is an application with three pages: one that sets its card, one that
// sets none and gets the default, and one whose cover elagoht/meta declares.
func newSite(t *testing.T, dir string, extra ...collage.Plugin) *site {
	t.Helper()
	og := NewWith(Config{Templates: templates, Root: "t", Default: "og/default.html", Dir: dir})
	sets := &atomic.Int32{}
	app, err := collage.New(&collage.Config{
		BaseURL:  "https://example.com",
		Template: collage.TemplateConfig{FS: templates, Root: "t"},
		Cache:    collage.CacheConfig{Enabled: true, Type: "memory", DefaultTTL: time.Hour},
		Plugins:  append(extra, og),
	})
	if err != nil {
		t.Fatal(err)
	}
	layout := func() *collage.Fragment { return collage.NewFragment("layout", "layout.html").Build() }
	page := func(name, path string, handler collage.DataHandlerFunc) *collage.Page {
		return collage.NewPage(name).WithLayouts(layout()).
			WithContent(collage.NewFragment(name+"-content", "page.html").WithDataHandler(handler).Build()).
			WithPath("en", path).Incremental(time.Hour).Build()
	}
	err = app.Register(
		page("post", "/post", collage.Load(func(_ context.Context, rc *collage.RenderContext) (string, error) {
			sets.Add(1)
			rc.HoistTitle("A post")
			return "post", Set(rc, "og/post.html", Card{Title: "Hello, cards", Label: "Software", Fields: map[string]string{"date": "2 October 2026"}})
		})),
		page("about", "/about", collage.Load(func(_ context.Context, rc *collage.RenderContext) (string, error) {
			rc.HoistTitle("About me")
			meta.Set(rc, meta.Page{Description: "Who I am & what I build."})
			return "about", nil
		})),
		page("covered", "/covered", collage.Load(func(_ context.Context, rc *collage.RenderContext) (string, error) {
			meta.Set(rc, meta.Page{Image: "/static/cover.png"})
			return "covered", nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	return &site{app: app, og: og, sets: sets}
}

var ogImage = regexp.MustCompile(`<meta property="og:image" content="([^"]+)">`)

func cardURL(t *testing.T, body string) string {
	t.Helper()
	m := ogImage.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no og:image in:\n%s", body)
	}
	return m[1]
}

// origins resolves two hosts, as elagoht/tenant would.
type origins struct{}

func (origins) Name() string                             { return "test/origins" }
func (origins) Version() string                          { return "0" }
func (origins) Init(context.Context, collage.Host) error { return nil }
func (origins) Shutdown(context.Context) error           { return nil }
func (origins) Origin(_ context.Context, host string) (string, bool) {
	switch host {
	case "a.test":
		return "https://a.example", true
	case "b.test":
		return "https://b.example", true
	}
	return "", false
}

// hostSite is newSite's application with origins registered before the plugin
// and no Config.BaseURL.
func hostSite(t *testing.T) http.Handler {
	t.Helper()
	og := NewWith(Config{Templates: templates, Root: "t", Default: "og/default.html"})
	app, err := collage.New(&collage.Config{
		Template: collage.TemplateConfig{FS: templates, Root: "t"},
		Cache:    collage.CacheConfig{Enabled: true, Type: "memory", DefaultTTL: time.Hour},
		Plugins:  []collage.Plugin{origins{}, og},
	})
	if err != nil {
		t.Fatal(err)
	}
	layout := collage.NewFragment("layout", "layout.html").Build()
	about := collage.NewPage("about").WithLayouts(layout).
		WithContent(collage.NewFragment("about-content", "page.html").WithDataHandler(
			collage.Load(func(_ context.Context, rc *collage.RenderContext) (string, error) {
				rc.HoistTitle("About me")
				return "about", nil
			})).Build()).
		WithPath("en", "/about").Incremental(time.Hour).Build()
	if err := app.Register(about); err != nil {
		t.Fatal(err)
	}
	return app.Handler()
}

// Without Config.BaseURL, a card's URL follows the request's origin.
func TestCard_OriginFollowsHost(t *testing.T) {
	h := hostSite(t)
	for host, want := range map[string]string{"a.test": "https://a.example/_og/", "b.test": "https://b.example/_og/"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://"+host+"/about", nil))
		if got := cardURL(t, rec.Body.String()); !strings.HasPrefix(got, want) {
			t.Errorf("%s og:image %q, want prefix %s", host, got, want)
		}
	}
}

func TestAPageThatSetsItsCard(t *testing.T) {
	s := newSite(t, "", meta.New(meta.Options{}))
	c := collagetest.New(t, s.app.Handler())
	page := c.Get("/post").WantStatus(http.StatusOK).Body
	url := cardURL(t, page)
	if !strings.HasPrefix(url, "https://example.com/_og/") || !strings.HasSuffix(url, ".png") {
		t.Fatalf("og:image %q", url)
	}
	for _, want := range []string{
		`<meta property="og:image:width" content="1200">`,
		`<meta property="og:image:height" content="630">`,
		`<meta property="og:image:type" content="image/png">`,
		`<meta property="og:image:alt" content="Hello, cards">`,
		`<meta name="twitter:card" content="summary_large_image">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the head lacks %s", want)
		}
	}
	if n := strings.Count(page, `property="og:image"`); n != 1 {
		t.Errorf("%d og:image tags", n)
	}

	res := c.Get(url).WantStatus(http.StatusOK)
	if ct := res.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type %q", ct)
	}
	if cc := res.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control %q", cc)
	}
	img, err := png.Decode(strings.NewReader(res.Body))
	if err != nil || img.Bounds().Dx() != 1200 || img.Bounds().Dy() != 630 {
		t.Fatalf("the card: %v %v", img, err)
	}

	rec, ok := s.og.Recorded("/post")
	if !ok || rec.Template != "og/post.html" || rec.Card.Title != "Hello, cards" || rec.URL != url {
		t.Errorf("Recorded = %+v, %v", rec, ok)
	}
	if !strings.Contains(rec.HTML, "example.com") || !strings.Contains(rec.HTML, "Software · 2 October 2026") {
		t.Errorf("the executed card: %s", rec.HTML)
	}
}

// A page with no card gets the default, drawn from its title and description,
// and a large Twitter card over elagoht/meta's summary (DESIGN.md §15.1).
func TestTheDefaultCard(t *testing.T) {
	s := newSite(t, "", meta.New(meta.Options{}))
	c := collagetest.New(t, s.app.Handler())
	page := c.Get("/about").WantStatus(http.StatusOK).Body
	url := cardURL(t, page)
	rec, ok := s.og.Recorded("/about")
	if !ok || rec.Template != "og/default.html" || rec.Card.Title != "About me" || rec.Card.Description != "Who I am & what I build." {
		t.Fatalf("Recorded = %+v, %v", rec, ok)
	}
	if rec.URL != url {
		t.Errorf("the head's og:image is not the recorded card")
	}
	if !strings.Contains(page, `<meta name="twitter:card" content="summary_large_image">`) || strings.Contains(page, `content="summary">`) {
		t.Errorf("twitter:card:\n%s", page)
	}
	c.Get(url).WantStatus(http.StatusOK)
}

// A page whose head already has an og:image keeps it.
func TestACoverIsKept(t *testing.T) {
	s := newSite(t, "", meta.New(meta.Options{}))
	page := collagetest.New(t, s.app.Handler()).Get("/covered").WantStatus(http.StatusOK).Body
	if url := cardURL(t, page); url != "https://example.com/static/cover.png" {
		t.Errorf("og:image %q", url)
	}
	if _, ok := s.og.Recorded("/covered"); ok {
		t.Error("a card was recorded for a page with its own image")
	}
}

// Only a hash a render recorded is drawn; anything else is a 404.
func TestUnknownCardsAreNotFound(t *testing.T) {
	s := newSite(t, "")
	c := collagetest.New(t, s.app.Handler())
	for _, target := range []string{"/_og/0123456789abcdef0123456789abcdef.png", "/_og/nothex.png", "/_og/../t/page.html", "/_og/"} {
		if res := c.Get(target); res.Status != http.StatusNotFound && res.Status != http.StatusMovedPermanently {
			t.Errorf("GET %s = %d", target, res.Status)
		}
	}
}

// Requests for one card arriving together draw it once.
func TestConcurrentRequestsShareADraw(t *testing.T) {
	s := newSite(t, "")
	h := s.app.Handler()
	url := cardURL(t, collagetest.New(t, h).Get("/post").Body)
	var wg sync.WaitGroup
	bodies := make([]string, 16)
	for i := range bodies {
		wg.Go(func() { bodies[i] = collagetest.New(t, h).Get(url).WantStatus(http.StatusOK).Body })
	}
	wg.Wait()
	for _, b := range bodies {
		if b != bodies[0] {
			t.Fatal("concurrent requests got different cards")
		}
	}
}

// A card recorded before a restart is drawn after it, from Dir, without its page
// rendering again.
func TestACardSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	before := newSite(t, dir)
	url := cardURL(t, collagetest.New(t, before.app.Handler()).Get("/post").Body)

	after := newSite(t, dir)
	res := collagetest.New(t, after.app.Handler()).Get(url).WantStatus(http.StatusOK)
	if _, err := png.Decode(strings.NewReader(res.Body)); err != nil {
		t.Fatal(err)
	}
	if after.sets.Load() != 0 {
		t.Error("the page rendered again to draw its card")
	}
}

// A static export writes the cards its pages carry.
func TestTheExportWritesTheCards(t *testing.T) {
	s := newSite(t, "")
	out := t.TempDir()
	b, err := collage.NewBuilder(s.app, collage.BuildOptions{OutDir: out})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	cards, _ := filepath.Glob(filepath.Join(out, "_og", "*.png"))
	// /post's own card and /about's default; /covered has an image of its own.
	if len(cards) != 2 {
		t.Fatalf("%d cards exported, want 2", len(cards))
	}
	page, err := os.ReadFile(filepath.Join(out, "post", "index.html"))
	if err != nil {
		page, err = os.ReadFile(filepath.Join(out, "post.html"))
	}
	if err != nil {
		t.Fatal(err)
	}
	url := cardURL(t, string(page))
	if _, err := os.Stat(filepath.Join(out, "_og", filepath.Base(url))); err != nil {
		t.Errorf("the page's card %s was not exported", url)
	}
}

// A card template the renderer cannot draw stops the application at startup,
// naming the template, line and column.
func TestABrokenTemplateRefusesToStart(t *testing.T) {
	bad := fstest.MapFS{
		"t/layout.html": {Data: []byte(`<html><head>{{hoist "head"}}</head><body>{{slot "content"}}</body></html>`)},
		"t/og/bad.html": {Data: []byte("<div style=\"display:flex\">\n  <div style=\"position:absolute\">x</div>\n</div>")},
	}
	app, err := collage.New(&collage.Config{
		BaseURL:  "https://example.com",
		Template: collage.TemplateConfig{FS: bad, Root: "t"},
		Plugins:  []collage.Plugin{NewWith(Config{Templates: bad, Root: "t"})},
	})
	if err == nil {
		err = app.Start()
	}
	if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "og/bad.html:2:") {
		t.Errorf("start = %v, want og/bad.html:2: ... ErrUnsupported", err)
	}
}

func TestTheApplicationNeedsABaseURL(t *testing.T) {
	app, err := collage.New(&collage.Config{
		Template: collage.TemplateConfig{FS: templates, Root: "t"},
		Plugins:  []collage.Plugin{NewWith(Config{Templates: templates, Root: "t"})},
	})
	if err == nil {
		err = app.Start()
	}
	if !errors.Is(err, ErrNoBaseURL) {
		t.Errorf("start = %v, want ErrNoBaseURL", err)
	}
}

// Set without the plugin on the render — a test with no plugin — does nothing.
func TestSetWithoutThePlugin(t *testing.T) {
	if err := Set(nil, "og/post.html", Card{}); err != nil {
		t.Error(err)
	}
}

func TestTheCommandDrawsACard(t *testing.T) {
	s := newSite(t, "")
	if err := s.app.Start(); err != nil {
		t.Fatal(err)
	}
	cardJSON := filepath.Join(t.TempDir(), "card.json")
	_ = os.WriteFile(cardJSON, []byte(`{"title":"From the command","label":"CLI"}`), 0o644)
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := s.og.command().Run(context.Background(), []string{"og/post.html", cardJSON})
	w.Close()
	os.Stdout = old
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	_, _ = b.ReadFrom(r)
	if img, err := png.Decode(&b); err != nil || img.Bounds().Dx() != 1200 {
		t.Fatalf("the command's PNG: %v", err)
	}
}

// Sibling fragments' handlers run concurrently, and both set a card: the page's
// card is the one its head ends up with, by collage's hoist rules, every time
// (DESIGN.md §15.3).
func TestSiblingCardsResolveByTheHead(t *testing.T) {
	og := NewWith(Config{Templates: templates, Root: "t"})
	app, err := collage.New(&collage.Config{
		BaseURL:  "https://example.com",
		Template: collage.TemplateConfig{FS: templates, Root: "t"},
		Plugins:  []collage.Plugin{og},
	})
	if err != nil {
		t.Fatal(err)
	}
	part := func(name, title string) *collage.Fragment {
		return collage.NewInlineFragment(name, `<i>{{.}}</i>`).WithDataHandler(collage.Load(func(_ context.Context, rc *collage.RenderContext) (string, error) {
			return name, Set(rc, "og/post.html", Card{Title: title})
		})).Build()
	}
	content := collage.NewInlineFragment("both", `<div>{{slot "a"}}{{slot "b"}}</div>`).
		WithSlotFragment("a", part("a", "First")).WithSlotFragment("b", part("b", "Second")).Build()
	if err := app.RegisterPage(collage.NewPage("both").WithLayouts(collage.NewFragment("layout", "layout.html").Build()).
		WithContent(content).WithPath("en", "/both").Dynamic().Build()); err != nil {
		t.Fatal(err)
	}
	h := app.Handler()
	var first string
	for range 20 {
		page := collagetest.New(t, h).Get("/both").Body
		url := cardURL(t, page)
		rec, _ := og.Recorded("/both")
		if rec.URL != url {
			t.Fatalf("recorded %s, the head has %s", rec.URL, url)
		}
		if first == "" {
			first = url
		} else if url != first {
			t.Fatal("the winning card changed between renders")
		}
	}
}

// In development a card template is read again on every use, and the preview
// lists the cards.
func TestDevelopment(t *testing.T) {
	dir := t.TempDir()
	for name, f := range templates {
		_ = os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755)
		_ = os.WriteFile(filepath.Join(dir, name), f.Data, 0o644)
	}
	disk := os.DirFS(dir)
	og := NewWith(Config{Templates: disk, Root: "t"})
	app, err := collage.New(&collage.Config{
		DevMode:  true,
		BaseURL:  "https://example.com",
		Template: collage.TemplateConfig{FS: disk, Root: "t"},
		Plugins:  []collage.Plugin{og},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.RegisterPage(collage.NewPage("p").WithLayouts(collage.NewFragment("layout", "layout.html").Build()).
		WithContent(collage.NewFragment("c", "page.html").WithDataHandler(collage.Load(func(_ context.Context, rc *collage.RenderContext) (string, error) {
			return "x", Set(rc, "og/post.html", Card{Title: "Live"})
		})).Build()).WithPath("en", "/p").Dynamic().Build()); err != nil {
		t.Fatal(err)
	}
	c := collagetest.New(t, app.Handler())
	before := cardURL(t, c.Get("/p").Body)
	_ = os.WriteFile(filepath.Join(dir, "t/og/post.html"), []byte(strings.Replace(testPostCard, "#0f172a", "#7c3aed", 1)), 0o644)
	after := cardURL(t, c.Get("/p").Body)
	if before == after {
		t.Error("an edited card template did not change the card")
	}
	c.Get(after).WantStatus(http.StatusOK)
	if preview := c.Get("/_og-preview/").WantStatus(http.StatusOK).Body; !strings.Contains(preview, "/p") {
		t.Errorf("the preview does not list the page:\n%s", preview)
	}
}

// A card's URL is absolute against BaseURL, except in development, where it is
// the request's own origin: a card opened from a page on localhost is drawn by
// that server, not looked for on the live site. Its hash is the same either way.
func TestDevelopmentCardsAreServedFromTheRequestsOrigin(t *testing.T) {
	hashes := map[bool]string{}
	for _, dev := range []bool{false, true} {
		og := NewWith(Config{Templates: templates, Root: "t", Default: "og/post.html"})
		app, err := collage.New(&collage.Config{
			DevMode:  dev,
			BaseURL:  "https://example.com",
			Template: collage.TemplateConfig{FS: templates, Root: "t"},
			Plugins:  []collage.Plugin{og},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := app.RegisterPage(collage.NewPage("p").WithLayouts(collage.NewFragment("layout", "layout.html").Build()).
			WithContent(collage.NewFragment("c", "page.html").Build()).WithPath("en", "/p").Dynamic().Build()); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "http://localhost:3000/p", nil)
		rec := httptest.NewRecorder()
		app.Handler().ServeHTTP(rec, req)
		card := cardURL(t, rec.Body.String())
		want := "https://example.com/_og/"
		if dev {
			want = "http://localhost:3000/_og/"
		}
		if !strings.HasPrefix(card, want) {
			t.Errorf("dev=%v: og:image = %q, want it under %s", dev, card, want)
		}
		if _, ok := og.Recorded("/p"); !ok {
			t.Errorf("dev=%v: the page's card was not recorded", dev)
		}
		hashes[dev] = hashOf(card)

		u, _ := url.Parse(card)
		rec = httptest.NewRecorder()
		app.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://localhost:3000"+u.Path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("dev=%v: GET %s = %d", dev, u.Path, rec.Code)
		}
	}
	if hashes[false] != hashes[true] {
		t.Errorf("the card's hash depends on the origin: %s and %s", hashes[false], hashes[true])
	}
}
