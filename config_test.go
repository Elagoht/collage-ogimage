package ogimage

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func valid() Config {
	return Config{Templates: fstest.MapFS{"templates/og/post.html": {Data: []byte(`<div></div>`)}}}.withDefaults()
}

func TestDefaults(t *testing.T) {
	c := valid()
	if c.Root != "templates" || c.CardDir != "og" || c.Prefix != "/_og/" {
		t.Errorf("paths = %q %q %q", c.Root, c.CardDir, c.Prefix)
	}
	if c.MaxEntries != DefaultMaxEntries || c.MaxBytes != DefaultMaxBytes || c.Workers < 1 {
		t.Errorf("caps = %d %d %d", c.MaxEntries, c.MaxBytes, c.Workers)
	}
	if time.Duration(c.RenderTimeout) != DefaultRenderTimeout {
		t.Errorf("renderTimeout = %s", time.Duration(c.RenderTimeout))
	}
	font := Config{Fonts: []Font{{Family: "Outfit", File: "a.ttf"}}}.withDefaults().Fonts[0]
	if font.Weight != 400 || font.Style != "normal" {
		t.Errorf("font defaults = %d %q", font.Weight, font.Style)
	}
	if err := c.validate(); err != nil {
		t.Errorf("a configuration of templates alone: %v", err)
	}
}

func TestValidateRefuses(t *testing.T) {
	for name, tweak := range map[string]func(*Config){
		"no templates":           func(c *Config) { c.Templates = nil },
		"prefix without slashes": func(c *Config) { c.Prefix = "_og" },
		"default outside og/":    func(c *Config) { c.Default = "pages/home.html" },
		"default not .html":      func(c *Config) { c.Default = "og/default.txt" },
		"default not clean":      func(c *Config) { c.Default = "og/../og/default.html" },
		"files prefix":           func(c *Config) { c.Files = map[string]fs.FS{"static": fstest.MapFS{}} },
		"origin with a path":     func(c *Config) { c.ImageOrigins = []string{"https://cms.example.com/api"} },
		"origin not http":        func(c *Config) { c.ImageOrigins = []string{"ftp://cms.example.com"} },
		"origin without a host":  func(c *Config) { c.ImageOrigins = []string{"https://"} },
		"fonts without files":    func(c *Config) { c.Fonts = []Font{{Family: "A", File: "a.ttf", Weight: 400, Style: "normal"}} },
		"font without a family": func(c *Config) {
			c.FontFiles = fstest.MapFS{}
			c.Fonts = []Font{{File: "a.ttf", Weight: 400, Style: "normal"}}
		},
		"font weight off the scale": func(c *Config) {
			c.FontFiles = fstest.MapFS{}
			c.Fonts = []Font{{Family: "A", File: "a.ttf", Weight: 450, Style: "normal"}}
		},
		"font style": func(c *Config) {
			c.FontFiles = fstest.MapFS{}
			c.Fonts = []Font{{Family: "A", File: "a.ttf", Weight: 400, Style: "oblique"}}
		},
		"negative workers": func(c *Config) { c.Workers = -1 },
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

func TestDurationReadsWhatAPersonWrites(t *testing.T) {
	var c struct {
		A, B Duration
	}
	if err := json.Unmarshal([]byte(`{"A":"2.5s","B":1000000000}`), &c); err != nil {
		t.Fatal(err)
	}
	if time.Duration(c.A) != 2500*time.Millisecond || time.Duration(c.B) != time.Second {
		t.Errorf("read %s and %s", time.Duration(c.A), time.Duration(c.B))
	}
	out, _ := json.Marshal(Duration(3 * time.Second))
	if string(out) != `"3s"` {
		t.Errorf("wrote %s", out)
	}
	if err := json.Unmarshal([]byte(`"soon"`), &c.A); err == nil || !strings.Contains(err.Error(), "soon") {
		t.Errorf("a bad duration: %v", err)
	}
}

// The JSON configuration is decoded over NewWith's; a slice it decodes must not be
// written into the caller's.
func TestNewWithCopies(t *testing.T) {
	origins := []string{"https://a.example.com"}
	p := NewWith(Config{ImageOrigins: origins})
	if err := json.Unmarshal([]byte(`{"imageOrigins":["https://b.example.com"]}`), &p.cfg); err != nil {
		t.Fatal(err)
	}
	if origins[0] != "https://a.example.com" {
		t.Errorf("the caller's slice was written: %v", origins)
	}
}

func TestInitRefusesUntilReleased(t *testing.T) {
	if err := NewWith(valid()).Init(context.Background(), nil); !errors.Is(err, ErrNotReleased) {
		t.Errorf("Init = %v, want ErrNotReleased", err)
	}
	if New().Name() != "elagoht/ogimage" {
		t.Error("name")
	}
}
