package ogimage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

// siteWith is a started application with og on the test templates, the edit
// applied to its configuration, and items registered — collagetest's Site with
// no home page.
func siteWith(t *testing.T, plugins []collage.Plugin, edit func(*collage.Config), items ...collage.Registrable) *collagetest.Site {
	t.Helper()
	return collagetest.NewSite(t, siteOptions(plugins, edit, items...)...)
}

func siteOptions(plugins []collage.Plugin, edit func(*collage.Config), items ...collage.Registrable) []collagetest.SiteOption {
	return []collagetest.SiteOption{
		collagetest.WithoutHome(),
		collagetest.WithPlugins(plugins...),
		collagetest.WithRegister(items...),
		collagetest.WithConfig(func(c *collage.Config) {
			c.Template.FS = templates
			if edit != nil {
				edit(c)
			}
		}),
	}
}

func onExample(c *collage.Config) { c.BaseURL = "https://example.com" }

func layoutFragment() *collage.Fragment { return collage.NewFragment("layout", "layout.html").Build() }

// newSite is an application with three pages: one that sets its card, one that
// sets none and gets the default, and one whose cover elagoht/meta declares.
func newSite(t *testing.T, dir string, extra ...collage.Plugin) *site {
	t.Helper()
	og := New(Options{Templates: templates, Root: "t", Default: "og/default.html", Dir: dir})
	sets := &atomic.Int32{}
	page := func(name, path string, data collage.Data) *collage.Page {
		return collage.NewPage(name).WithLayouts(layoutFragment()).
			WithContent(collage.NewFragment(name+"-content", "page.html").WithData(data).Build()).
			WithPath("en", path).Incremental(time.Hour).Build()
	}
	s := siteWith(t, append(extra, og), func(c *collage.Config) {
		onExample(c)
		c.Cache = collage.CacheConfig{Enabled: true, Type: "memory", DefaultTTL: time.Hour}
	},
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
	return &site{app: s.App, og: og, sets: sets}
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
	og := New(Options{Templates: templates, Root: "t", Default: "og/default.html"})
	about := collage.NewPage("about").WithLayouts(layoutFragment()).
		WithContent(collage.NewFragment("about-content", "page.html").WithData(
			collage.Load(func(_ context.Context, rc *collage.RenderContext) (string, error) {
				rc.HoistTitle("About me")
				return "about", nil
			})).Build()).
		WithPath("en", "/about").Incremental(time.Hour).Build()
	s := siteWith(t, []collage.Plugin{origins{}, og}, func(c *collage.Config) {
		c.Cache = collage.CacheConfig{Enabled: true, Type: "memory", DefaultTTL: time.Hour}
	}, about)
	return s.App.Handler()
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
	if _, err := collage.Build(context.Background(), s.app, collage.BuildOptions{OutDir: out}); err != nil {
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
	_, err := collagetest.TrySite(t, collagetest.WithoutHome(),
		collagetest.WithPlugins(New(Options{Templates: bad, Root: "t"})),
		collagetest.WithConfig(func(c *collage.Config) { onExample(c); c.Template.FS = bad }))
	if !errors.Is(err, ErrUnsupported) || !strings.Contains(err.Error(), "og/bad.html:2:") {
		t.Errorf("start = %v, want og/bad.html:2: ... ErrUnsupported", err)
	}
}

func TestTheApplicationNeedsABaseURL(t *testing.T) {
	_, err := collagetest.TrySite(t, siteOptions([]collage.Plugin{New(Options{Templates: templates, Root: "t"})}, nil)...)
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

// upperCard uppercases "iiii": a Turkish card draws "İİİİ", an English one "IIII".
const upperCard = `<div style="display:flex; width:400px; height:120px; background:#fff; color:#000; font-family:Go"><p style="font-size:64px; text-transform:uppercase">iiii</p></div>`

// localeSite is a site whose default locale is def and which supports en and tr,
// with the upper card among its templates.
func localeSite(t *testing.T, def string) *Plugin {
	t.Helper()
	fsys := fstest.MapFS{"t/og/upper.html": {Data: []byte(upperCard)}}
	for name, f := range templates {
		fsys[name] = f
	}
	og := New(Options{Templates: fsys, Root: "t"})
	siteWith(t, []collage.Plugin{og}, func(c *collage.Config) {
		onExample(c)
		c.Template.FS = fsys
		c.Locale = collage.LocaleConfig{Default: def, Supported: []string{"en", "tr"}}
	})
	return og
}

// runCommand runs the ogimage command with args and returns what it wrote to
// standard output.
func runCommand(t *testing.T, og *Plugin, args ...string) ([]byte, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := og.command().Run(context.Background(), args)
	w.Close()
	os.Stdout = old
	var b bytes.Buffer
	_, _ = b.ReadFrom(r)
	return b.Bytes(), runErr
}

// The command draws in the locale -locale names, in either spelling, and in the
// application's default locale without it.
func TestTheCommandTakesALocale(t *testing.T) {
	og := localeSite(t, "en")
	draw := func(args ...string) []byte {
		t.Helper()
		out, err := runCommand(t, og, args...)
		if err != nil {
			t.Fatalf("%q: %v", args, err)
		}
		return out
	}
	en, tr := draw("-locale", "en", "og/upper.html"), draw("-locale", "tr", "og/upper.html")
	if bytes.Equal(en, tr) {
		t.Error("-locale tr drew the card as en does")
	}
	if !bytes.Equal(draw("-locale=tr", "og/upper.html"), tr) {
		t.Error("-locale=tr drew differently from -locale tr")
	}
	if !bytes.Equal(draw("og/upper.html"), en) {
		t.Error("with no flag, an en site's card is not drawn in en")
	}
	if !bytes.Equal(draw("og/upper.html"), draw("-locale", "en", "og/upper.html")) {
		t.Error("drawing is not repeatable")
	}
	if got, _ := runCommand(t, localeSite(t, "tr"), "og/upper.html"); !bytes.Equal(got, tr) {
		t.Error("with no flag, a tr site's card is not drawn in tr")
	}
	for _, args := range [][]string{{"-locale"}, {"-locale", "tr"}, {"-locale=tr"}} {
		if _, err := runCommand(t, og, args...); err == nil {
			t.Errorf("%q: accepted with no template", args)
		}
	}
	if _, err := runCommand(t, og, "-locale", "xx", "og/upper.html"); err == nil || !strings.Contains(err.Error(), `"xx"`) {
		t.Errorf("an unsupported locale: %v", err)
	}
}

// Sibling fragments' handlers run concurrently, and both set a card: the page's
// card is the one its head ends up with, by collage's hoist rules, every time
// (DESIGN.md §15.3).
func TestSiblingCardsResolveByTheHead(t *testing.T) {
	og := New(Options{Templates: templates, Root: "t"})
	part := func(name, title string) *collage.Fragment {
		return collage.NewInlineFragment(name, `<i>{{.}}</i>`).WithData(collage.Load(func(_ context.Context, rc *collage.RenderContext) (string, error) {
			return name, Set(rc, "og/post.html", Card{Title: title})
		})).Build()
	}
	content := collage.NewInlineFragment("both", `<div>{{slot "a"}}{{slot "b"}}</div>`).
		WithSlotFragment("a", part("a", "First")).WithSlotFragment("b", part("b", "Second")).Build()
	h := siteWith(t, []collage.Plugin{og}, onExample, collage.NewPage("both").WithLayouts(layoutFragment()).
		WithContent(content).WithPath("en", "/both").Dynamic().Build()).App.Handler()
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
	og := New(Options{Templates: disk, Root: "t"})
	c := siteWith(t, []collage.Plugin{og}, func(c *collage.Config) {
		onExample(c)
		c.DevMode = true
		c.Template.FS = disk
	}, collage.NewPage("p").WithLayouts(layoutFragment()).
		WithContent(collage.NewFragment("c", "page.html").WithData(collage.Load(func(_ context.Context, rc *collage.RenderContext) (string, error) {
			return "x", Set(rc, "og/post.html", Card{Title: "Live"})
		})).Build()).WithPath("en", "/p").Dynamic().Build()).Client
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
		og := New(Options{Templates: templates, Root: "t", Default: "og/post.html"})
		app := siteWith(t, []collage.Plugin{og}, func(c *collage.Config) {
			onExample(c)
			c.DevMode = dev
		}, collage.NewPage("p").WithLayouts(layoutFragment()).
			WithContent(collage.NewFragment("c", "page.html").Build()).WithPath("en", "/p").Dynamic().Build()).App
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

// Recorded remembers at most MaxEntries pages: a crawler inventing URLs under a
// pattern page, each given the default card, cannot grow it without bound.
func TestRecordedIsCapped(t *testing.T) {
	og := New(Options{Templates: templates, Root: "t", Default: "og/default.html", MaxEntries: 2})
	site := siteWith(t, []collage.Plugin{og}, onExample, collage.NewPage("p").WithLayouts(layoutFragment()).
		WithContent(collage.NewFragment("c", "page.html").Build()).WithPath("en", "/p/{slug}").Dynamic().Build())
	for i := range 5 {
		site.Get(fmt.Sprintf("/p/%d", i)).WantStatus(http.StatusOK)
	}
	og.mu.Lock()
	n := len(og.recorded)
	og.mu.Unlock()
	if n != 2 {
		t.Errorf("%d paths recorded, want the cap, 2", n)
	}
	if _, ok := og.Recorded("/p/4"); !ok {
		t.Error("the newest path was forgotten")
	}
	if _, ok := og.Recorded("/p/0"); ok {
		t.Error("the oldest path was kept past the cap")
	}
}

// What plugins-config.json may not say: a duration as a number, a negative cap.
func TestConfigurationRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		raw  string
		want func(error) bool
	}{
		"number renderTimeout": {`{"renderTimeout": 5000000000}`, func(err error) bool { return strings.Contains(err.Error(), `"renderTimeout"`) }},
		"negative maxEntries": {`{"maxEntries": -1}`, func(err error) bool {
			return errors.Is(err, ErrInvalidConfig) && strings.Contains(err.Error(), "maxEntries")
		}},
		"negative maxBytes": {`{"maxBytes": -1}`, func(err error) bool {
			return errors.Is(err, ErrInvalidConfig) && strings.Contains(err.Error(), "maxBytes")
		}},
	} {
		opts := append(siteOptions([]collage.Plugin{New(Options{Templates: templates, Root: "t"})}, onExample),
			collagetest.WithPluginConfig(Name, c.raw))
		if _, err := collagetest.TrySite(t, opts...); err == nil || !c.want(err) {
			t.Errorf("%s: start = %v", name, err)
		}
	}
	_, err := collagetest.TrySite(t, append(siteOptions([]collage.Plugin{New(Options{Templates: templates, Root: "t"})}, onExample),
		collagetest.WithPluginConfig(Name, `{"renderTimeout": "2s", "maxEntries": 5}`))...)
	if err != nil {
		t.Errorf("a valid configuration: %v", err)
	}
}

// A card that cannot be drawn when a static build writes it is the build's
// ogimage-draw error, at the card's path. A card's HTML is checked when the page
// sets it, so what fails here is the draw itself: every worker is held, and the
// card cannot get one within RenderTimeout.
func TestTheExportReportsACardItCannotDraw(t *testing.T) {
	og := New(Options{Templates: templates, Root: "t", RenderTimeout: collage.Duration(50 * time.Millisecond)})
	s := siteWith(t, []collage.Plugin{og}, onExample, collage.NewPage("p").WithLayouts(layoutFragment()).
		WithContent(collage.NewFragment("c", "page.html").WithData(collage.Load(func(_ context.Context, rc *collage.RenderContext) (string, error) {
			return "x", Set(rc, "og/post.html", Card{Title: "Held"})
		})).Build()).WithPath("en", "/p").Static().Build())
	for range cap(og.sem) {
		og.sem <- struct{}{}
	}
	t.Cleanup(func() {
		for range cap(og.sem) {
			<-og.sem
		}
	})
	report, _ := collage.Build(context.Background(), s.App, collage.BuildOptions{OutDir: t.TempDir()})
	if report == nil {
		t.Fatal("Build returned no report")
	}
	found := false
	for _, f := range report.Findings {
		if f.Rule == "ogimage-draw" && f.Level == collage.FindingError && strings.HasPrefix(f.Path, "/_og/") && strings.HasSuffix(f.Path, ".png") {
			found = true
		}
	}
	if !found {
		t.Errorf("no ogimage-draw error at the card's path: %+v", report.Findings)
	}
}

// In development a card URL's scheme follows collage.IsHTTPS: X-Forwarded-Proto
// is believed from anyone when no TrustedProxies are listed, and only from one
// of them when they are.
func TestDevelopmentCardsFollowIsHTTPS(t *testing.T) {
	for _, c := range []struct {
		name    string
		trusted []string
		want    string
	}{
		{"no trusted proxies", nil, "https://localhost:3000/_og/"},
		{"the peer is not a trusted proxy", []string{"10.0.0.0/8"}, "http://localhost:3000/_og/"},
	} {
		og := New(Options{Templates: templates, Root: "t", Default: "og/post.html"})
		app := siteWith(t, []collage.Plugin{og}, func(cfg *collage.Config) {
			onExample(cfg)
			cfg.DevMode = true
			cfg.Server.TrustedProxies = c.trusted
		}, collage.NewPage("p").WithLayouts(layoutFragment()).
			WithContent(collage.NewFragment("c", "page.html").Build()).WithPath("en", "/p").Dynamic().Build()).App
		req := httptest.NewRequest(http.MethodGet, "http://localhost:3000/p", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		rec := httptest.NewRecorder()
		app.Handler().ServeHTTP(rec, req)
		if card := cardURL(t, rec.Body.String()); !strings.HasPrefix(card, c.want) {
			t.Errorf("%s: og:image = %q, want it under %s", c.name, card, c.want)
		}
	}
}
