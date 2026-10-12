package ogimage

import (
	"bytes"
	"errors"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Elagoht/collage/pkg/collage"
)

// Draw validates what it is given: fonts listed with no FontFiles are an error,
// not a nil dereference.
func TestDrawRefusesFontsWithoutFiles(t *testing.T) {
	_, err := Draw(t.Context(), Options{Fonts: []Font{{Family: "A", File: "a.ttf"}}}, Input{HTML: `<div></div>`, Locale: "en"})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Draw = %v, want ErrInvalidConfig", err)
	}
}

// Draw keeps to RenderTimeout, its images' fetches included, as a served card
// does.
func TestDrawKeepsToRenderTimeout(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer slow.Close()
	start := time.Now()
	_, _ = Draw(t.Context(), Options{ImageOrigins: []string{slow.URL}, RenderTimeout: collage.Duration(100 * time.Millisecond)},
		Input{HTML: `<div style="display:flex"><img src="` + slow.URL + `/a.png" style="width:10px;height:10px"></div>`, Locale: "en"})
	if took := time.Since(start); took > time.Second {
		t.Errorf("Draw took %s with a 100ms renderTimeout", took)
	}
}

// Draw draws in the locale it is given: a Turkish card uppercases i as İ.
func TestDrawFollowsTheLocale(t *testing.T) {
	card := `<div style="display:flex; width:400px; height:120px; background:#fff; color:#000; font-family:Go"><p style="font-size:64px; text-transform:uppercase">iiii</p></div>`
	encode := func(locale string) []byte {
		img, err := Draw(t.Context(), Options{}, Input{HTML: card, Locale: locale})
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	if bytes.Equal(encode("en"), encode("tr")) {
		t.Error("a Turkish card is drawn as an English one")
	}
	if !bytes.Equal(encode("en"), encode("")) {
		t.Error("no locale draws differently from en")
	}
}
