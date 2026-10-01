package ogimage

import (
	"bytes"
	"flag"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

var update = flag.Bool("update", false, "rewrite the golden PNGs from the renderer's output")

// testPhoto is a deterministic picture for the fixtures that draw images: four
// coloured quadrants and a diagonal, so a crop, a stretch or a misplacement
// shows.
func testPhoto() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 300, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 300; x++ {
			c := color.RGBA{R: 239, G: 68, B: 68, A: 255}
			switch {
			case x >= 150 && y < 100:
				c = color.RGBA{R: 34, G: 197, B: 94, A: 255}
			case x < 150 && y >= 100:
				c = color.RGBA{R: 59, G: 130, B: 246, A: 255}
			case x >= 150 && y >= 100:
				c = color.RGBA{R: 250, G: 204, B: 21, A: 255}
			}
			if d := x*2 - y*3; d > -6 && d < 6 {
				c = color.RGBA{A: 255}
			}
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func goldenConfig() Config {
	return Config{Files: map[string]fs.FS{"/img/": fstest.MapFS{"photo.png": {Data: testPhoto()}}}}
}

// Each card in testdata/golden draws the PNG beside it (DESIGN.md §12.3, phase
// 4). The PNGs are the renderer's own output, reviewed by eye when they change:
// go test -run Golden -update.
func TestGolden(t *testing.T) {
	cards, _ := filepath.Glob("testdata/golden/*.html")
	if len(cards) == 0 {
		t.Fatal("no golden cards")
	}
	for _, card := range cards {
		name := strings.TrimSuffix(filepath.Base(card), ".html")
		t.Run(name, func(t *testing.T) {
			src, _ := os.ReadFile(card)
			got, err := Draw(goldenConfig(), string(src))
			if err != nil {
				t.Fatal(err)
			}
			path := strings.TrimSuffix(card, ".html") + ".png"
			if *update {
				var b bytes.Buffer
				if err := png.Encode(&b, got); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("%v: run with -update and review the PNG", err)
			}
			defer f.Close()
			want, err := png.Decode(f)
			if err != nil {
				t.Fatal(err)
			}
			if got.Bounds() != want.Bounds() {
				t.Fatalf("size %v, golden %v", got.Bounds(), want.Bounds())
			}
			differ := 0
			b := got.Bounds()
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					if channelDiff(got.At(x, y), want.At(x, y)) > 2 {
						differ++
					}
				}
			}
			if differ > 0 {
				t.Errorf("%d pixels differ from the golden; if the change is intended, -update and review it", differ)
			}
		})
	}
}

func channelDiff(a, b color.Color) uint32 {
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	d := func(x, y uint32) uint32 {
		if x > y {
			return (x - y) >> 8
		}
		return (y - x) >> 8
	}
	return max(d(r1, r2), d(g1, g2), d(b1, b2), d(a1, a2))
}
