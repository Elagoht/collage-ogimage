package ogimage

import (
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/Elagoht/collage/pkg/collage"
)

// Name is the plugin's name, and the key of its plugin configuration.
const Name = "elagoht/ogimage"

// ErrNotReleased is returned by Init until v0.1.0: the plugin is designed but not
// yet drawing, and an application that registers it should fail at startup rather
// than run with no cards.
var ErrNotReleased = errors.New("ogimage: in development, not yet released; see DESIGN.md")

// Plugin draws and serves share cards.
type Plugin struct {
	cfg Config
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
func (p *Plugin) Version() string { return "0.0.0" }

// Configure decodes the plugin configuration over the plugin's Config, fills its
// defaults and validates it.
func (p *Plugin) Configure(_ context.Context, host collage.ConfigHost) error {
	if err := host.Config(&p.cfg); err != nil {
		return err
	}
	p.cfg = p.cfg.withDefaults()
	return p.cfg.validate()
}

// Init refuses to start until v0.1.0 is released.
func (p *Plugin) Init(context.Context, collage.Host) error { return ErrNotReleased }

// Shutdown has nothing to release.
func (p *Plugin) Shutdown(context.Context) error { return nil }
