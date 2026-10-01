package ogimage

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"sync"

	"github.com/Elagoht/collage-ogimage/internal/css"
	"github.com/Elagoht/collage-ogimage/internal/dom"
	"github.com/Elagoht/collage-ogimage/internal/fetch"
	"github.com/Elagoht/collage-ogimage/internal/layout"
	"github.com/Elagoht/collage-ogimage/internal/paint"
	"github.com/Elagoht/collage-ogimage/internal/text"
)

// renderer draws card HTML: the fonts and the image loader a configuration
// names, made once and shared by every draw.
type renderer struct {
	fonts  *text.Fonts
	loader *fetch.Loader
	// Warn reports what is drawn but not as written: a font family that is not
	// registered, an image that could not be loaded.
	warn func(msg string)
	// digest is a hash of every font's bytes, part of every card's hash.
	digest []byte
}

func newRenderer(cfg Config) (*renderer, error) {
	var faces []text.Face
	var all [][]byte
	for _, f := range cfg.Fonts {
		data, err := fs.ReadFile(cfg.FontFiles, f.File)
		if err != nil {
			return nil, fmt.Errorf("ogimage: font %q: %w", f.Family, err)
		}
		faces = append(faces, text.Face{Family: f.Family, Weight: f.Weight, Italic: f.Style == "italic", Data: data})
		all = append(all, data)
	}
	fonts, err := text.NewFonts(faces)
	if err != nil {
		return nil, fmt.Errorf("ogimage: %w", err)
	}
	for _, b := range text.Bundled {
		all = append(all, b.Data)
	}
	return &renderer{
		fonts:  fonts,
		loader: &fetch.Loader{Files: cfg.Files, Origins: cfg.ImageOrigins},
		warn:   func(string) {},
		digest: fontsDigest(all),
	}, nil
}

// Draw draws card HTML — one root element, in the subset of DESIGN.md §6 — with
// cfg's fonts and image sources, and returns the image. It is the renderer the
// plugin serves cards with, for golden tests and for designing a card without a
// site; cfg needs no Templates for it.
func Draw(cfg Config, html string) (image.Image, error) {
	r, err := newRenderer(cfg.withDefaults())
	if err != nil {
		return nil, err
	}
	return r.draw(context.Background(), html, "en")
}

// draw is the pipeline: parse, load the images the card names, lay out, paint.
func (r *renderer) draw(ctx context.Context, html, locale string) (*image.RGBA, error) {
	root, errs := dom.Parse(html)
	if len(errs) > 0 {
		out := make([]error, len(errs))
		for i, e := range errs {
			out[i] = &TemplateError{Template: "card", Line: e.Line, Col: e.Col, Msg: e.Msg}
		}
		return nil, errors.Join(out...)
	}
	if err := checkDynamic(root); err != nil {
		return nil, err
	}

	images := r.loadImages(ctx, root)
	m := &text.Measurer{
		Fonts:  r.fonts,
		Locale: locale,
		Images: func(n *dom.Node) (float64, float64, bool) {
			if img := images[n.Src]; img != nil {
				b := img.Bounds()
				return float64(b.Dx()), float64(b.Dy()), true
			}
			return 0, 0, false
		},
	}
	box, err := layout.Layout(root, m)
	if err != nil {
		return nil, err
	}
	return paint.Draw(box, m, func(src string) image.Image { return images[src] }), nil
}

// checkDynamic refuses a declaration still masked: card HTML is executed before
// it is drawn, so one here means a value that was never filled in.
func checkDynamic(n *dom.Node) error {
	for _, d := range n.Style {
		if d.Dynamic {
			return &TemplateError{Template: "card", Line: n.Line, Col: n.Col, Msg: fmt.Sprintf("<%s> style %s has no value", n.Tag, d.Property)}
		}
	}
	for _, c := range n.Children {
		if err := checkDynamic(c); err != nil {
			return err
		}
	}
	return nil
}

// loadImages loads every image the card names, at once, and returns them by
// URL; one that fails is left out, warned about, and drawn as nothing.
func (r *renderer) loadImages(ctx context.Context, root *dom.Node) map[string]image.Image {
	srcs := map[string]bool{}
	var walk func(*dom.Node)
	walk = func(n *dom.Node) {
		if n.Kind == dom.Image && n.Src != "" {
			srcs[n.Src] = true
		}
		for _, d := range n.Style {
			if bg, ok := d.Value.(css.Background); ok && bg.Image != "" {
				srcs[string(bg.Image)] = true
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)

	out := make(map[string]image.Image, len(srcs))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for src := range srcs {
		wg.Go(func() {
			img, err := r.loader.Load(ctx, src)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				r.warn(fmt.Sprintf("ogimage: an image is drawn as nothing: %v", err))
				return
			}
			out[src] = img
		})
	}
	wg.Wait()
	return out
}
