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
func (p *Plugin) OnBuildFinished(ctx context.Context, ev *collage.BuildFinishedEvent) error {
	p.mu.Lock()
	hashes := make([]string, 0, len(p.built))
	for h := range p.built {
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
		body, err := p.image(ctx, h)
		if err != nil {
			ev.Error(p.cfg.Prefix+h+".png", "ogimage", "the card could not be drawn: "+err.Error())
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, h+".png"), body, 0o644); err != nil {
			return err
		}
	}
	return nil
}
