package ogimage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/Elagoht/collage/pkg/collage"
)

// command is "go run . ogimage [-locale tr] <template> [card.json]": a card
// template drawn with the Card in card.json, in the application's default locale
// or the one -locale names, written to standard output as a PNG — the loop for
// designing a card without a site (DESIGN.md §11). The draw keeps to
// RenderTimeout, as a served card's does.
func (p *Plugin) command() collage.Command {
	const usage = "usage: ogimage [-locale tr] <template> [card.json] > card.png"
	return collage.Command{
		Name:  "ogimage",
		Usage: "ogimage [-locale tr] <template> [card.json] > card.png",
		Short: "Draw a card template to a PNG on standard output",
		Run: func(ctx context.Context, args []string) error {
			locale := p.locale
			if len(args) >= 2 && args[0] == "-locale" {
				locale, args = args[1], args[2:]
			}
			if len(args) < 1 || len(args) > 2 {
				return errors.New(usage)
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
			rec, err := p.record(args[0], card, p.siteFor(p.baseURL), p.baseURL, "/", locale)
			if err != nil {
				return err
			}
			body, err := p.drawBounded(ctx, encodeSpec(locale, rec.HTML))
			if err != nil {
				return err
			}
			_, err = os.Stdout.Write(body)
			return err
		},
	}
}
