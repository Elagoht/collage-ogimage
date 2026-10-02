package ogimage

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/Elagoht/collage/pkg/collage"
)

// Name is the plugin's name, and the key of its plugin configuration.
const Name = "elagoht/ogimage"

// ErrNoBaseURL is returned by Init without the application's Config.BaseURL:
// networks follow only absolute og:image URLs.
var ErrNoBaseURL = errors.New("ogimage: the application's Config.BaseURL is required: og:image URLs are absolute")

// Plugin draws and serves share cards.
type Plugin struct {
	cfg Config

	devMode    bool
	baseURL    string
	site       SiteInfo
	log        *slog.Logger
	renderer   *renderer
	store      *store
	templates  map[string]*template.Template
	fontDigest []byte

	// Drawing: at most Workers at once, one draw per card however many ask.
	sem      chan struct{}
	mu       sync.Mutex
	inflight map[string]*drawCall

	// What renders recorded: the card of each page path, the cards a static
	// build's pages hold, and in development how many cards each path made.
	recorded map[string]RecordedCard
	built    map[string]bool
	perPage  map[string]map[string]bool
}

// New returns a plugin configured entirely from the application's plugin
// configuration. Templates has no JSON form, so a plugin built with New and no
// configuration is refused at startup with ErrNoTemplates: use NewWith.
func New() *Plugin { return &Plugin{} }

// NewWith returns a plugin with cfg as its starting point, which the
// application's own configuration is decoded over: a key the JSON has replaces
// the field entirely, and a key it lacks leaves cfg's value.
//
// The slices and maps are copied, because encoding/json decodes into the ones it
// finds, and would otherwise write the JSON's values into the caller's.
func NewWith(cfg Config) *Plugin {
	cfg.ImageOrigins = slices.Clone(cfg.ImageOrigins)
	cfg.Fonts = slices.Clone(cfg.Fonts)
	cfg.Files = maps.Clone(cfg.Files)
	return &Plugin{cfg: cfg}
}

// Name returns Name.
func (p *Plugin) Name() string { return Name }

// Version returns the plugin's release.
func (p *Plugin) Version() string { return "0.1.2" }

// Configure decodes the plugin configuration over the plugin's Config, fills its
// defaults and validates it.
func (p *Plugin) Configure(_ context.Context, host collage.ConfigHost) error {
	if err := host.Config(&p.cfg); err != nil {
		return err
	}
	p.cfg = p.cfg.withDefaults()
	return p.cfg.validate()
}

// Init parses and validates the card templates, loads the fonts, and mounts the
// cards — refusing to start on a template the renderer cannot draw.
func (p *Plugin) Init(_ context.Context, host collage.Host) error {
	if p.cfg.Prefix == "" {
		// Configure did not run: a Host that is not a ConfigHost.
		p.cfg = p.cfg.withDefaults()
		if err := p.cfg.validate(); err != nil {
			return err
		}
	}
	p.log = host.Logger()
	p.devMode = host.DevMode()
	p.baseURL = strings.TrimSuffix(host.BaseURL(), "/")
	u, err := url.Parse(p.baseURL)
	if p.baseURL == "" || err != nil || u.Host == "" {
		return fmt.Errorf("%w, got %q", ErrNoBaseURL, p.baseURL)
	}
	p.site = SiteInfo{Name: p.cfg.SiteName, URL: p.baseURL, Host: u.Host}
	if p.site.Name == "" {
		p.site.Name = u.Host
	}

	if p.renderer, err = newRenderer(p.cfg); err != nil {
		return err
	}
	p.renderer.warn = func(msg string) { p.log.Warn(msg) }
	p.fontDigest = p.renderer.digest
	if err := p.loadTemplates(); err != nil {
		return err
	}

	p.store = newStore(p.cfg.Dir, p.cfg.MaxEntries, p.cfg.MaxBytes)
	if p.cfg.Dir == "" && !p.devMode {
		p.log.Warn("ogimage: Dir is empty, so cards are kept in memory only; with a disk page cache, a page cached before a restart names a card the restarted process cannot draw")
	}
	p.sem = make(chan struct{}, p.cfg.Workers)
	p.inflight = map[string]*drawCall{}
	p.recorded = map[string]RecordedCard{}
	p.built = map[string]bool{}
	p.perPage = map[string]map[string]bool{}

	opts := []collage.MountOption{collage.WithoutBuildCopy()}
	if !p.devMode {
		opts = append(opts, collage.WithCacheControl("public, max-age=31536000, immutable"))
	} else {
		opts = append(opts, collage.WithCacheControl("no-store"))
	}
	if err := host.Mount(p.cfg.Prefix, &cardFS{plugin: p}, opts...); err != nil {
		return fmt.Errorf("ogimage: mount %s: %w", p.cfg.Prefix, err)
	}
	if p.devMode {
		if err := host.Handle(previewPrefix, p.preview()); err != nil {
			return fmt.Errorf("ogimage: preview: %w", err)
		}
	}
	return host.RegisterCommand(p.command())
}

// Shutdown has nothing to release.
func (p *Plugin) Shutdown(context.Context) error { return nil }

// OnBeforeRender puts the plugin's state on the render, where Set and
// OnAfterRender find it, and — when a default card is configured — declares a
// large Twitter card at the depth elagoht/meta declares its summary, after it.
func (p *Plugin) OnBeforeRender(_ context.Context, ev *collage.BeforeRenderEvent) error {
	rc := ev.Context
	if rc == nil || p.store == nil {
		return nil
	}
	st := &renderState{plugin: p, locale: rc.Locale, origin: p.baseURL, cards: map[string]RecordedCard{}}
	if rc.Request != nil {
		st.path = rc.Request.URL.Path
		st.origin = p.origin(rc.Request)
	}
	rc.Set(stateKey, st)
	if p.cfg.Default != "" {
		rc.HoistMeta("twitter:card", "summary_large_image")
	}
	return nil
}

// OnAfterRender adds the default card to a page that has no og:image, and notes
// which card each page carries.
func (p *Plugin) OnAfterRender(_ context.Context, ev *collage.AfterRenderEvent) error {
	st, _ := ev.Data[stateKey].(*renderState)
	if st == nil {
		return nil
	}
	head := readHead(ev.HTML)
	if p.cfg.Default != "" && head.image == "" {
		rec, err := p.record(p.cfg.Default, Card{Title: head.title, Description: head.description}, st.origin, st.path, st.locale)
		if err != nil {
			p.log.Warn("ogimage: the default card could not be made", "path", st.path, "err", err)
		} else {
			for _, tag := range cardTags(rec) {
				ev.Hoist("head", tag[0], template.HTML(tag[1])) // markup assembled from escaped values
			}
			st.mu.Lock()
			st.cards[hashOf(rec.URL)] = rec
			st.mu.Unlock()
			head.image = rec.URL
		}
	}
	// The page's card is the og:image its head ended up with: collage's hoist
	// rules decided it, not the order Set calls arrived in (DESIGN.md §15.3).
	prefix := st.origin + p.cfg.Prefix
	if !strings.HasPrefix(head.image, prefix) {
		return nil
	}
	hash := hashOf(head.image)
	st.mu.Lock()
	rec, ok := st.cards[hash]
	st.mu.Unlock()
	if !ok {
		return nil
	}
	p.mu.Lock()
	p.recorded[st.path] = rec
	if ev.Static {
		p.built[hash] = true
	}
	p.mu.Unlock()
	return nil
}

// origin is what a request's card URLs are absolute against: the application's
// BaseURL, except in development, where it is the request's own scheme and host,
// so a card opened from a page served on localhost is drawn by that server rather
// than looked for on the live site. The card's hash does not change with it.
func (p *Plugin) origin(r *http.Request) string {
	if !p.devMode || r.Host == "" {
		return p.baseURL
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// Recorded returns the card the page at path carried on its last render: its
// template, its Card, its HTML and its URL. It is for a test that a page sets
// the card it should.
func (p *Plugin) Recorded(path string) (RecordedCard, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, ok := p.recorded[path]
	return rec, ok
}

// noteCard counts, in development, the distinct cards a page records, and warns
// when a page records more than 100: its card holds something that varies per
// request (DESIGN.md §9).
func (p *Plugin) noteCard(path, hash string) {
	if !p.devMode {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	set := p.perPage[path]
	if set == nil {
		set = map[string]bool{}
		p.perPage[path] = set
	}
	if set[hash] {
		return
	}
	set[hash] = true
	if len(set) == 101 {
		p.log.Warn("ogimage: one page has recorded more than 100 cards; a card made from request input — a search term — makes one per request", "path", path)
	}
}

// fontsDigest is a hash of every font's bytes, in order: a changed font changes
// every card.
func fontsDigest(data [][]byte) []byte {
	h := sha256.New()
	for _, d := range data {
		h.Write(d)
		h.Write([]byte{0})
	}
	return h.Sum(nil)
}
