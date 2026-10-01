package fetch

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func pngOf(w, h int) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)))
	return b.Bytes()
}

func TestLocalFilesOnlyThroughTheirPrefix(t *testing.T) {
	files := fstest.MapFS{"logo.png": {Data: pngOf(4, 3)}, "img/a.png": {Data: pngOf(2, 2)}}
	l := &Loader{Files: map[string]fs.FS{"/static/": files}}
	img, err := l.Load(context.Background(), "/static/logo.png")
	if err != nil || img.Bounds().Dx() != 4 {
		t.Fatalf("Load = %v, %v", img, err)
	}
	if _, err := l.Load(context.Background(), "/static/img/a.png?v=2"); err != nil {
		t.Errorf("a query on a local path: %v", err)
	}
	for _, src := range []string{"/static/../secret.png", "/static//logo.png", "/other/logo.png", "logo.png", "file:///etc/passwd"} {
		if _, err := l.Load(context.Background(), src); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%s: %v, want ErrNotAllowed", src, err)
		}
	}
}

func TestRemoteOnlyFromAllowedOrigins(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(pngOf(1, 1)) }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.png":
			_, _ = w.Write(pngOf(5, 5))
		case "/away":
			http.Redirect(w, r, other.URL+"/b.png", http.StatusFound)
		case "/big":
			_, _ = w.Write(bytes.Repeat([]byte{0}, MaxBytes+10))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	l := &Loader{Origins: []string{srv.URL}}
	if img, err := l.Load(context.Background(), srv.URL+"/a.png"); err != nil || img.Bounds().Dx() != 5 {
		t.Fatalf("allowed: %v", err)
	}
	if _, err := l.Load(context.Background(), other.URL+"/b.png"); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("another origin: %v", err)
	}
	if _, err := l.Load(context.Background(), srv.URL+"/away"); err == nil {
		t.Error("a redirect to another origin was followed")
	}
	if _, err := l.Load(context.Background(), srv.URL+"/big"); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("a body past the cap: %v", err)
	}
	if _, err := l.Load(context.Background(), srv.URL+"/missing"); err == nil {
		t.Error("a 404 loaded")
	}
}

func TestDataURLs(t *testing.T) {
	l := &Loader{}
	src := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngOf(3, 7))
	if img, err := l.Load(context.Background(), src); err != nil || img.Bounds().Dy() != 7 {
		t.Fatalf("data: %v", err)
	}
	if _, err := l.Load(context.Background(), "data:text/html,<script>"); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("a data: URL of HTML: %v", err)
	}
}

// An image whose header claims more pixels than the budget is refused before it
// is decoded.
func TestPixelBudget(t *testing.T) {
	big := pngOf(1, 1)
	// Patch the IHDR width and height: 10000×10000 is past 40 megapixels.
	copy(big[16:24], []byte{0, 0, 0x27, 0x10, 0, 0, 0x27, 0x10})
	binary.BigEndian.PutUint32(big[29:33], crc32.ChecksumIEEE(big[12:29]))
	l := &Loader{}
	src := "data:image/png;base64," + base64.StdEncoding.EncodeToString(big)
	if _, err := l.Load(context.Background(), src); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Errorf("past the budget: %v", err)
	}
}
