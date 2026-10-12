package ogimage

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Elagoht/collage/pkg/collage"
)

// OnBuildFinished writes every card the build's pages carry to the output, under
// Prefix, and fails the build for one that cannot be drawn (DESIGN.md §8.3).
//
// Each card is drawn from the spec the build kept for it, not from the store: by
// now the store may have evicted the first pages' specs, past MaxEntries or
// MaxBytes, from memory and disk alike.
func (p *Plugin) OnBuildFinished(ctx context.Context, ev *collage.BuildFinishedEvent) error {
	p.mu.Lock()
	specs := make(map[string]string, len(p.built))
	hashes := make([]string, 0, len(p.built))
	for h, spec := range p.built {
		specs[h] = spec
		hashes = append(hashes, h)
	}
	p.mu.Unlock()
	sort.Strings(hashes)
	dir := filepath.Join(ev.OutDir, filepath.FromSlash(strings.Trim(p.cfg.Prefix, "/")))
	if len(hashes) > 0 {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	for _, h := range hashes {
		body, err := p.drawBounded(ctx, specs[h])
		if err != nil {
			ev.Error(p.cfg.Prefix+h+".png", "ogimage-draw", "the card could not be drawn: "+err.Error())
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, h+".png"), body, 0o644); err != nil {
			return err
		}
	}
	return nil
}
