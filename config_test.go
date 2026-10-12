package ogimage

import (
	"encoding/json"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"
)

func valid() Options {
	return Options{Templates: fstest.MapFS{"templates/og/post.html": {Data: []byte(`<div></div>`)}}}.withDefaults()
}

func TestDefaults(t *testing.T) {
	c := valid()
	if c.Root != "templates" || c.CardDir != "og" || c.Prefix != "/_og/" {
		t.Errorf("paths = %q %q %q", c.Root, c.CardDir, c.Prefix)
	}
	if c.MaxEntries != 10000 || c.MaxBytes != 256<<20 || c.Workers < 1 {
		t.Errorf("caps = %d %d %d", c.MaxEntries, c.MaxBytes, c.Workers)
	}
	if time.Duration(c.RenderTimeout) != 5*time.Second {
		t.Errorf("renderTimeout = %s", time.Duration(c.RenderTimeout))
	}
	font := Options{Fonts: []Font{{Family: "Outfit", File: "a.ttf"}}}.withDefaults().Fonts[0]
	if font.Weight != 400 || font.Style != "normal" {
		t.Errorf("font defaults = %d %q", font.Weight, font.Style)
	}
	if err := c.validate(); err != nil {
		t.Errorf("a configuration of templates alone: %v", err)
	}
}

func TestValidateRefuses(t *testing.T) {
	for name, tweak := range map[string]func(*Options){
		"no templates":           func(c *Options) { c.Templates = nil },
		"prefix without slashes": func(c *Options) { c.Prefix = "_og" },
		"default outside og/":    func(c *Options) { c.Default = "pages/home.html" },
		"default not .html":      func(c *Options) { c.Default = "og/default.txt" },
		"default not clean":      func(c *Options) { c.Default = "og/../og/default.html" },
		"files prefix":           func(c *Options) { c.Files = map[string]fs.FS{"static": fstest.MapFS{}} },
		"origin with a path":     func(c *Options) { c.ImageOrigins = []string{"https://cms.example.com/api"} },
		"origin not http":        func(c *Options) { c.ImageOrigins = []string{"ftp://cms.example.com"} },
		"origin without a host":  func(c *Options) { c.ImageOrigins = []string{"https://"} },
		"fonts without files":    func(c *Options) { c.Fonts = []Font{{Family: "A", File: "a.ttf", Weight: 400, Style: "normal"}} },
		"font without a family": func(c *Options) {
			c.FontFiles = fstest.MapFS{}
			c.Fonts = []Font{{File: "a.ttf", Weight: 400, Style: "normal"}}
		},
		"font weight off the scale": func(c *Options) {
			c.FontFiles = fstest.MapFS{}
			c.Fonts = []Font{{Family: "A", File: "a.ttf", Weight: 450, Style: "normal"}}
		},
		"font style": func(c *Options) {
			c.FontFiles = fstest.MapFS{}
			c.Fonts = []Font{{Family: "A", File: "a.ttf", Weight: 400, Style: "oblique"}}
		},
		"negative workers":    func(c *Options) { c.Workers = -1 },
		"negative maxEntries": func(c *Options) { c.MaxEntries = -1 },
		"negative maxBytes":   func(c *Options) { c.MaxBytes = -1 },
		"negative timeout":    func(c *Options) { c.RenderTimeout = -1 },
	} {
		c := valid()
		tweak(&c)
		err := c.validate()
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if !errors.Is(err, ErrInvalidConfig) && !errors.Is(err, ErrNoTemplates) {
			t.Errorf("%s: %v is neither ErrInvalidConfig nor ErrNoTemplates", name, err)
		}
	}
}

func TestValidateAccepts(t *testing.T) {
	c := valid()
	c.Default = "og/default.html"
	c.ImageOrigins = []string{"https://cms.example.com", "http://localhost:8080/"}
	c.Files = map[string]fs.FS{"/static/": fstest.MapFS{}}
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
}

// The JSON configuration is decoded over New's Options; a slice it decodes must
// not be written into the caller's.
func TestNewCopies(t *testing.T) {
	origins := []string{"https://a.example.com"}
	p := New(Options{ImageOrigins: origins})
	if err := json.Unmarshal([]byte(`{"imageOrigins":["https://b.example.com"]}`), &p.cfg); err != nil {
		t.Fatal(err)
	}
	if origins[0] != "https://a.example.com" {
		t.Errorf("the caller's slice was written: %v", origins)
	}
}

func TestName(t *testing.T) {
	if New(Options{}).Name() != "elagoht/ogimage" {
		t.Error("name")
	}
}
