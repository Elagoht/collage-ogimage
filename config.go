package ogimage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"runtime"
	"strings"
	"time"
)

// Defaults for the fields Config leaves at zero.
const (
	DefaultRoot          = "templates"
	DefaultCardDir       = "og"
	DefaultPrefix        = "/_og/"
	DefaultMaxEntries    = 10000
	DefaultMaxBytes      = 256 << 20
	DefaultRenderTimeout = 5 * time.Second
)

// ErrNoTemplates is returned when Config.Templates is nil: card templates are
// files the application ships, and there is nowhere else to read them from.
var ErrNoTemplates = errors.New("ogimage: Config.Templates is required: the fs.FS card templates are read from")

// ErrInvalidConfig wraps every other problem with a configuration.
var ErrInvalidConfig = errors.New("ogimage: invalid configuration")

// Config configures the plugin. Every field but the fs.FS ones can also come from
// the application's plugin configuration, under "elagoht/ogimage", which is
// decoded over the Config the plugin was built with: a key the JSON has replaces
// the field, and a key it lacks leaves it.
type Config struct {
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
	// MaxEntries caps how many cards are kept. Zero means DefaultMaxEntries; a
	// negative value means unlimited.
	MaxEntries int `json:"maxEntries"`
	// MaxBytes caps the bytes of cards kept. Zero means DefaultMaxBytes; a
	// negative value means unlimited.
	MaxBytes int64 `json:"maxBytes"`
	// Workers is how many cards are drawn at once. Zero means half the CPUs, at
	// least one.
	Workers int `json:"workers"`
	// RenderTimeout is how long one card may take to draw, including fetching its
	// images. Zero means DefaultRenderTimeout.
	RenderTimeout Duration `json:"renderTimeout"`
}

// Font is one face cards can be drawn in.
type Font struct {
	// Family is the name font-family refers to it by: "Outfit".
	Family string `json:"family"`
	// Weight is 100 to 900. Zero means 400.
	Weight int `json:"weight"`
	// Style is "normal" or "italic". Empty means "normal".
	Style string `json:"style"`
	// File is the TrueType or OpenType file's path in Config.FontFiles.
	File string `json:"file"`
}

// Card is what a card template is drawn from. Every card has the same shape, so
// any card template can be Config.Default, and a page moves from the default to a
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
func (c Config) withDefaults() Config {
	if c.Root == "" {
		c.Root = DefaultRoot
	}
	if c.CardDir == "" {
		c.CardDir = DefaultCardDir
	}
	if c.Prefix == "" {
		c.Prefix = DefaultPrefix
	}
	if c.MaxEntries == 0 {
		c.MaxEntries = DefaultMaxEntries
	}
	if c.MaxBytes == 0 {
		c.MaxBytes = DefaultMaxBytes
	}
	if c.Workers == 0 {
		c.Workers = max(1, runtime.NumCPU()/2)
	}
	if c.RenderTimeout == 0 {
		c.RenderTimeout = Duration(DefaultRenderTimeout)
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

// validate reports the first problem with a configuration withDefaults filled.
func (c Config) validate() error {
	if c.Templates == nil {
		return ErrNoTemplates
	}
	if !strings.HasPrefix(c.Prefix, "/") || !strings.HasSuffix(c.Prefix, "/") {
		return fmt.Errorf("%w: prefix %q must begin and end with /", ErrInvalidConfig, c.Prefix)
	}
	if c.Default != "" && !c.isCard(c.Default) {
		return fmt.Errorf("%w: default %q is not a file under %s/", ErrInvalidConfig, c.Default, c.CardDir)
	}
	for prefix := range c.Files {
		if !strings.HasPrefix(prefix, "/") || !strings.HasSuffix(prefix, "/") {
			return fmt.Errorf("%w: files prefix %q must begin and end with /", ErrInvalidConfig, prefix)
		}
	}
	for _, origin := range c.ImageOrigins {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
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
func (c Config) isCard(name string) bool {
	clean := path.Clean(name)
	return clean == name && strings.HasPrefix(name, c.CardDir+"/") && path.Ext(name) == ".html"
}

// Duration is a time.Duration that reads "5s" from JSON, the way a person writes
// it, as well as a number of nanoseconds.
type Duration time.Duration

// UnmarshalJSON accepts "5s" and 5000000000 alike.
func (d *Duration) UnmarshalJSON(data []byte) error {
	var asNumber int64
	if err := json.Unmarshal(data, &asNumber); err == nil {
		*d = Duration(asNumber)
		return nil
	}
	var asText string
	if err := json.Unmarshal(data, &asText); err != nil {
		return fmt.Errorf("ogimage: a duration is a string such as \"5s\", or nanoseconds: %w", err)
	}
	parsed, err := time.ParseDuration(asText)
	if err != nil {
		return fmt.Errorf("ogimage: duration %q: %w", asText, err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalJSON writes the form a person reads.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}
