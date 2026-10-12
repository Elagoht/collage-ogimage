package ogimage

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"runtime"
	"strings"
	"time"

	"github.com/Elagoht/collage/pkg/collage"
)

// Defaults for the fields Options leaves at zero.
const (
	defaultRoot          = "templates"
	defaultCardDir       = "og"
	defaultPrefix        = "/_og/"
	defaultMaxEntries    = 10000
	defaultMaxBytes      = 256 << 20
	defaultRenderTimeout = 5 * time.Second
)

// ErrNoTemplates is returned when Options.Templates is nil: card templates are
// files the application ships, and there is nowhere else to read them from.
var ErrNoTemplates = errors.New("ogimage: Options.Templates is required: the fs.FS card templates are read from")

// ErrInvalidConfig wraps every other problem with a configuration.
var ErrInvalidConfig = errors.New("ogimage: invalid configuration")

// Options configures the plugin. Every field but the fs.FS ones can also come
// from the application's plugin configuration, under "elagoht/ogimage", which is
// decoded over the Options the plugin was built with: a key the JSON has replaces
// the field, and a key it lacks leaves it.
type Options struct {
	// Templates is the filesystem card templates are read from, usually the one
	// the application parses its own templates from. Required.
	Templates fs.FS `json:"-"`
	// Root is the directory in Templates that template names are relative to.
	// Defaults to "templates".
	Root string `json:"root"`
	// CardDir is the directory under Root that holds card templates. Every .html
	// file in it is parsed at startup, and a name passed to Set is one of them:
	// "og/post.html". Defaults to "og".
	CardDir string `json:"cardDir"`
	// Default is the card template for every page that sets no card of its own,
	// drawn from the page's title and description. Empty: such a page gets no
	// card.
	Default string `json:"default"`
	// SiteName is what a template reads as .Site.Name. Defaults to the host of the
	// application's BaseURL.
	SiteName string `json:"siteName"`

	// Files maps a URL prefix to the filesystem local images under it are read
	// from: {"/static/": staticFS} makes <img src="/static/logo.png"> read
	// logo.png from staticFS, with no request made.
	Files map[string]fs.FS `json:"-"`
	// ImageOrigins are the origins remote images may be fetched from, such as
	// "https://cms.example.com". An image from any other origin is an error.
	ImageOrigins []string `json:"imageOrigins"`

	// Fonts are the fonts cards are drawn in, their files read from FontFiles.
	// With none, cards are drawn in the bundled Go fonts.
	Fonts []Font `json:"fonts"`
	// FontFiles is the filesystem the Fonts' files are read from.
	FontFiles fs.FS `json:"-"`

	// Dir is where cards and what they are made of are kept across restarts.
	// Empty keeps them in memory only, which is wrong with a disk page cache: a
	// page cached before a restart names a card the restarted process cannot
	// draw.
	Dir string `json:"dir"`
	// Prefix is the URL prefix cards are served under. Defaults to "/_og/".
	Prefix string `json:"prefix"`
	// MaxEntries caps how many cards are kept, and how many pages Recorded
	// remembers. Zero means 10000; a negative value is an error. For a store
	// with no practical limit, write a large number.
	MaxEntries int `json:"maxEntries"`
	// MaxBytes caps the bytes of cards kept. Zero means 256 MiB (268435456); a
	// negative value is an error. For no practical limit, write a large number.
	MaxBytes int64 `json:"maxBytes"`
	// Workers is how many cards are drawn at once. Zero means half the CPUs, at
	// least one.
	Workers int `json:"workers"`
	// RenderTimeout is how long one card may take to draw, including fetching its
	// images, written as a string: "5s". Zero means 5 seconds.
	RenderTimeout collage.Duration `json:"renderTimeout"`
}

// Font is one face cards can be drawn in.
type Font struct {
	// Family is the name font-family refers to it by: "Outfit".
	Family string `json:"family"`
	// Weight is 100 to 900. Zero means 400.
	Weight int `json:"weight"`
	// Style is "normal" or "italic". Empty means "normal".
	Style string `json:"style"`
	// File is the TrueType or OpenType file's path in Options.FontFiles.
	File string `json:"file"`
}

// Card is what a card template is drawn from. Every card has the same shape, so
// any card template can be Options.Default, and a page moves from the default to a
// card of its own by naming a template.
type Card struct {
	// Title is the headline.
	Title string `json:"title"`
	// Description is a sentence under it.
	Description string `json:"description"`
	// Label is a kicker: a category, a section, a tag.
	Label string `json:"label"`
	// Image is a picture the template may place: a cover, an avatar.
	Image string `json:"image"`
	// Fields holds anything else, by name: {{.Fields.date}}.
	Fields map[string]string `json:"fields"`
}

// withDefaults fills every zero field with its default.
func (c Options) withDefaults() Options {
	if c.Root == "" {
		c.Root = defaultRoot
	}
	if c.CardDir == "" {
		c.CardDir = defaultCardDir
	}
	if c.Prefix == "" {
		c.Prefix = defaultPrefix
	}
	if c.MaxEntries == 0 {
		c.MaxEntries = defaultMaxEntries
	}
	if c.MaxBytes == 0 {
		c.MaxBytes = defaultMaxBytes
	}
	if c.Workers == 0 {
		c.Workers = max(1, runtime.NumCPU()/2)
	}
	if c.RenderTimeout == 0 {
		c.RenderTimeout = collage.Duration(defaultRenderTimeout)
	}
	for i := range c.Fonts {
		if c.Fonts[i].Weight == 0 {
			c.Fonts[i].Weight = 400
		}
		if c.Fonts[i].Style == "" {
			c.Fonts[i].Style = "normal"
		}
	}
	return c
}

// validate reports the first problem with an Options withDefaults filled, for
// the plugin: what drawing needs, and the templates, prefix and default card
// serving needs.
func (c Options) validate() error {
	if c.Templates == nil {
		return ErrNoTemplates
	}
	if !strings.HasPrefix(c.Prefix, "/") || !strings.HasSuffix(c.Prefix, "/") {
		return fmt.Errorf("%w: prefix %q must begin and end with /", ErrInvalidConfig, c.Prefix)
	}
	if c.Default != "" && !c.isCard(c.Default) {
		return fmt.Errorf("%w: default %q is not a file under %s/", ErrInvalidConfig, c.Default, c.CardDir)
	}
	if c.MaxEntries < 0 {
		return fmt.Errorf("%w: maxEntries %d is negative", ErrInvalidConfig, c.MaxEntries)
	}
	if c.MaxBytes < 0 {
		return fmt.Errorf("%w: maxBytes %d is negative", ErrInvalidConfig, c.MaxBytes)
	}
	return c.validateDrawing()
}

// validateDrawing reports the first problem with what drawing a card reads:
// the image sources, the fonts, the workers and the timeout. Draw checks this
// much; it needs no templates.
func (c Options) validateDrawing() error {
	for prefix := range c.Files {
		if !strings.HasPrefix(prefix, "/") || !strings.HasSuffix(prefix, "/") {
			return fmt.Errorf("%w: files prefix %q must begin and end with /", ErrInvalidConfig, prefix)
		}
	}
	for _, origin := range c.ImageOrigins {
		if _, err := collage.ParseOrigin(origin); err != nil {
			return fmt.Errorf("%w: image origin %q must be a scheme and host, such as https://cms.example.com", ErrInvalidConfig, origin)
		}
	}
	if len(c.Fonts) > 0 && c.FontFiles == nil {
		return fmt.Errorf("%w: fonts are listed but FontFiles is nil", ErrInvalidConfig)
	}
	for _, f := range c.Fonts {
		switch {
		case f.Family == "":
			return fmt.Errorf("%w: a font has no family", ErrInvalidConfig)
		case f.File == "":
			return fmt.Errorf("%w: font %q has no file", ErrInvalidConfig, f.Family)
		case f.Weight < 100 || f.Weight > 900 || f.Weight%100 != 0:
			return fmt.Errorf("%w: font %q weight %d is not 100 to 900 in hundreds", ErrInvalidConfig, f.Family, f.Weight)
		case f.Style != "normal" && f.Style != "italic":
			return fmt.Errorf("%w: font %q style %q is not normal or italic", ErrInvalidConfig, f.Family, f.Style)
		}
	}
	if c.Workers < 0 {
		return fmt.Errorf("%w: workers %d is negative", ErrInvalidConfig, c.Workers)
	}
	if c.RenderTimeout < 0 {
		return fmt.Errorf("%w: renderTimeout %s is negative", ErrInvalidConfig, time.Duration(c.RenderTimeout))
	}
	return nil
}

// isCard reports whether name is a template path under CardDir: "og/post.html".
func (c Options) isCard(name string) bool {
	clean := path.Clean(name)
	return clean == name && strings.HasPrefix(name, c.CardDir+"/") && path.Ext(name) == ".html"
}
