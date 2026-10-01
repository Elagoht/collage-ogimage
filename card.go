package ogimage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/Elagoht/collage-ogimage/internal/css"
	"github.com/Elagoht/collage-ogimage/internal/dom"
	"github.com/Elagoht/collage-ogimage/internal/layout"
	"github.com/Elagoht/collage/pkg/collage"
)

// engineVersion names the renderer in every card's hash: a release that draws
// the same HTML differently moves every card to a new URL.
const engineVersion = "ogimage/1"

// ErrUnknownTemplate is a name passed to Set that is no card template.
var ErrUnknownTemplate = errors.New("ogimage: no such card template")

// SiteInfo is what a card template reads as .Site.
type SiteInfo struct {
	// Name is Config.SiteName, or the host of the application's BaseURL.
	Name string
	// URL is the application's BaseURL; Host its host.
	URL, Host string
}

// PageInfo is what a card template reads as .Page.
type PageInfo struct {
	// Path is the request's path, and Locale the page's.
	Path, Locale string
}

// cardData is what a card template is executed with: the Card's fields promoted,
// .Site and .Page.
type cardData struct {
	Card
	Site SiteInfo
	Page PageInfo
}

// renderState is the plugin's, on a render: put there by OnBeforeRender with
// rc.Set, where Set and OnAfterRender find it (DESIGN.md §15.2).
type renderState struct {
	plugin *Plugin
	path   string
	locale string

	mu       sync.Mutex
	cards    map[string]RecordedCard // by hash, every card this render recorded
	explicit bool                    // a page called Set
}

// stateKey is the key the render state is kept under; prefixed with the plugin's
// name, as nothing else on the render uses.
const stateKey = Name + ":render"

// RecordedCard is a card a page recorded: what it was drawn from, and where.
type RecordedCard struct {
	// Template is the card template's name; Card the data it was drawn from.
	Template string
	Card     Card
	// HTML is the executed template, which is what is drawn and hashed.
	HTML string
	// URL is where the card is served, absolute.
	URL string
	// Width and Height are the card's size.
	Width, Height int
}

// Set declares the page's card: tmpl, a template under the card directory
// ("og/post.html"), drawn from card. Call it from a data handler, after
// meta.Set when both are used, since the later declaration of og:image wins.
//
// It returns an error for an unknown template, a template that fails to execute,
// or a value the renderer cannot draw. Without the plugin on the render — a test
// with no plugin registered — it does nothing and returns nil.
func Set(rc *collage.RenderContext, tmpl string, card Card) error {
	if rc == nil {
		return nil
	}
	st, ok := collage.Get[*renderState](rc, stateKey)
	if !ok || st == nil {
		return nil
	}
	rec, err := st.plugin.record(tmpl, card, st.path, st.locale)
	if err != nil {
		return err
	}
	st.mu.Lock()
	st.cards[hashOf(rec.URL)] = rec
	st.explicit = true
	st.mu.Unlock()
	hoistCard(rc, rec)
	return nil
}

// hoistCard declares a card's head tags: og:image, its size, type and text, and
// a large Twitter card.
func hoistCard(rc *collage.RenderContext, rec RecordedCard) {
	rc.HoistProperty("og:image", rec.URL)
	rc.HoistProperty("og:image:width", strconv.Itoa(rec.Width))
	rc.HoistProperty("og:image:height", strconv.Itoa(rec.Height))
	rc.HoistProperty("og:image:type", "image/png")
	rc.HoistMeta("twitter:card", "summary_large_image")
	if rec.Card.Title != "" {
		rc.HoistProperty("og:image:alt", rec.Card.Title)
		rc.HoistMeta("twitter:image:alt", rec.Card.Title)
	}
}

// cardTags is hoistCard's tags as HTML, for AfterRenderEvent.Hoist: key, then
// markup.
func cardTags(rec RecordedCard) [][2]string {
	prop := func(p, c string) [2]string {
		return [2]string{"property:" + p, `<meta property="` + template.HTMLEscapeString(p) + `" content="` + template.HTMLEscapeString(c) + `">`}
	}
	meta := func(n, c string) [2]string {
		return [2]string{"meta:" + n, `<meta name="` + template.HTMLEscapeString(n) + `" content="` + template.HTMLEscapeString(c) + `">`}
	}
	tags := [][2]string{
		prop("og:image", rec.URL),
		prop("og:image:width", strconv.Itoa(rec.Width)),
		prop("og:image:height", strconv.Itoa(rec.Height)),
		prop("og:image:type", "image/png"),
	}
	if rec.Card.Title != "" {
		tags = append(tags, prop("og:image:alt", rec.Card.Title), meta("twitter:image:alt", rec.Card.Title))
	}
	return tags
}

// record executes a card template, validates what it produced, and records it.
func (p *Plugin) record(tmpl string, card Card, pagePath, locale string) (RecordedCard, error) {
	t, err := p.template(tmpl)
	if err != nil {
		return RecordedCard{}, err
	}
	var b bytes.Buffer
	data := cardData{Card: card, Site: p.site, Page: PageInfo{Path: pagePath, Locale: locale}}
	if err := t.Execute(&b, data); err != nil {
		return RecordedCard{}, fmt.Errorf("ogimage: %s: %w", tmpl, err)
	}
	html := b.String()
	root, errs := dom.Parse(html)
	if len(errs) > 0 {
		out := make([]error, len(errs))
		for i, e := range errs {
			out[i] = &TemplateError{Template: tmpl + " (executed)", Line: e.Line, Col: e.Col, Msg: e.Msg}
		}
		return RecordedCard{}, errors.Join(out...)
	}
	w, h := cardSize(root)
	spec := encodeSpec(locale, html)
	hash := p.hash(spec)
	p.store.record(hash, spec)
	p.noteCard(pagePath, hash)
	return RecordedCard{
		Template: tmpl, Card: card, HTML: html, Width: w, Height: h,
		URL: p.baseURL + p.cfg.Prefix + hash + ".png",
	}, nil
}

// encodeSpec is what is stored and hashed for a card: the locale its text is
// transformed in, on a line of its own, then its HTML. The locale is part of
// what is drawn: "i" uppercases to "İ" in a Turkish card.
func encodeSpec(locale, html string) string { return locale + "\n" + html }

func decodeSpec(spec string) (locale, html string) {
	locale, html, _ = strings.Cut(spec, "\n")
	return locale, html
}

// hash is a card's name: the hex of the first 16 bytes of SHA-256 over the
// engine, the fonts and the spec (DESIGN.md §7). The size is in the HTML.
func (p *Plugin) hash(html string) string {
	h := sha256.New()
	h.Write([]byte(engineVersion))
	h.Write([]byte{0})
	h.Write(p.fontDigest)
	h.Write([]byte{0})
	h.Write([]byte(html))
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// hashOf is the hash in a card's URL.
func hashOf(url string) string {
	return strings.TrimSuffix(path.Base(url), ".png")
}

// cardSize is the root's declared size in pixels, or the default card size.
func cardSize(root *dom.Node) (int, int) {
	w, h := layout.DefaultWidth, layout.DefaultHeight
	if d, ok := root.Last("width"); ok && !d.Dynamic {
		if l, ok := d.Value.(css.Length); ok && l.Unit == css.Px {
			w = int(l.N)
		}
	}
	if d, ok := root.Last("height"); ok && !d.Dynamic {
		if l, ok := d.Value.(css.Length); ok && l.Unit == css.Px {
			h = int(l.N)
		}
	}
	return w, h
}

// template returns a parsed card template: from the set parsed at startup, or in
// development parsed again from the filesystem so an edit shows on the next use.
func (p *Plugin) template(name string) (*template.Template, error) {
	if !p.cfg.isCard(name) {
		return nil, fmt.Errorf("%w: %q is not a file under %s/", ErrUnknownTemplate, name, p.cfg.CardDir)
	}
	if p.devMode {
		src, err := fs.ReadFile(p.cfg.Templates, path.Join(p.cfg.Root, name))
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrUnknownTemplate, name)
		}
		if err := checkTemplate(name, string(src)); err != nil {
			return nil, err
		}
		return template.New(name).Parse(string(src))
	}
	t, ok := p.templates[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownTemplate, name)
	}
	return t, nil
}

// loadTemplates parses and validates every card template under the card
// directory, and reports every problem in every one of them together.
func (p *Plugin) loadTemplates() error {
	dir := path.Join(p.cfg.Root, p.cfg.CardDir)
	p.templates = map[string]*template.Template{}
	var problems []error
	err := fs.WalkDir(p.cfg.Templates, dir, func(file string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && file == dir {
				return fs.SkipAll
			}
			return err
		}
		if d.IsDir() || path.Ext(file) != ".html" {
			return nil
		}
		name := strings.TrimPrefix(file, p.cfg.Root+"/")
		if p.cfg.Root == "." || p.cfg.Root == "" {
			name = file
		}
		src, err := fs.ReadFile(p.cfg.Templates, file)
		if err != nil {
			return err
		}
		if err := checkTemplate(name, string(src)); err != nil {
			problems = append(problems, err)
			return nil
		}
		t, err := template.New(name).Parse(string(src))
		if err != nil {
			problems = append(problems, fmt.Errorf("ogimage: %w", err))
			return nil
		}
		p.templates[name] = t
		p.warnFonts(name, string(src))
		return nil
	})
	if err != nil {
		return fmt.Errorf("ogimage: card templates under %s: %w", dir, err)
	}
	if len(problems) > 0 {
		return errors.Join(problems...)
	}
	if p.cfg.Default != "" {
		if _, ok := p.templates[p.cfg.Default]; !ok {
			return fmt.Errorf("%w: Default %q is not among the card templates under %s", ErrUnknownTemplate, p.cfg.Default, dir)
		}
	}
	return nil
}

// warnFonts logs, once per template, a font family it names that no font is
// registered for: the card is drawn in the fallback.
func (p *Plugin) warnFonts(name, src string) {
	root, _ := dom.Parse(mask(src))
	if root == nil {
		return
	}
	seen := map[string]bool{}
	var walk func(*dom.Node)
	walk = func(n *dom.Node) {
		if d, ok := n.Last("font-family"); ok && !d.Dynamic {
			for _, fam := range d.Value.(css.FontFamily) {
				if !seen[fam] && !p.renderer.fonts.Has(fam) {
					seen[fam] = true
					p.log.Warn("ogimage: a card names a font family no font is registered for; it is drawn in the fallback",
						"template", name, "family", fam)
				}
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
}
