package ogimage

import (
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"os"

	"github.com/Elagoht/collage/pkg/collage"
)

// command is "go run . ogimage <template> [card.json]": a card template drawn
// with the Card in card.json, written to standard output as a PNG — the loop for
// designing a card without a site (DESIGN.md §11).
func (p *Plugin) command() collage.Command {
	return collage.Command{
		Name:  "ogimage",
		Usage: "ogimage <template> [card.json] > card.png",
		Short: "Draw a card template to a PNG on standard output",
		Run: func(ctx context.Context, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return fmt.Errorf("usage: ogimage <template> [card.json] > card.png")
			}
			var card Card
			if len(args) == 2 {
				raw, err := os.ReadFile(args[1])
				if err != nil {
					return err
				}
				if err := json.Unmarshal(raw, &card); err != nil {
					return fmt.Errorf("ogimage: %s: %w", args[1], err)
				}
			}
			rec, err := p.record(args[0], card, "/", "en")
			if err != nil {
				return err
			}
			img, err := p.renderer.draw(ctx, rec.HTML, "en")
			if err != nil {
				return err
			}
			return png.Encode(os.Stdout, img)
		},
	}
}
