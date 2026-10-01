// Package fetch loads the images a card draws — an <img src> or a background
// url() — within bounds (DESIGN.md §9): local files only through the fs.FS a URL
// prefix is mapped to, remote ones only from the allowed origins, with a timeout,
// a size cap and no redirect to another origin, and none decoded past a pixel
// budget.
package fetch

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // the formats a card may draw
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	_ "golang.org/x/image/webp"
)

// Limits on what is fetched and decoded.
const (
	MaxBytes  = 10 << 20
	MaxPixels = 40_000_000
	Timeout   = 5 * time.Second
)

// ErrNotAllowed is an image from somewhere the configuration does not name.
var ErrNotAllowed = errors.New("fetch: image not allowed")

// Loader loads images.
type Loader struct {
	// Files maps a URL path prefix ("/static/") to the filesystem under it.
	Files map[string]fs.FS
	// Origins are the scheme and host remote images may come from.
	Origins []string
	// Client fetches remote images; nil uses one with Timeout and no
	// redirects off the image's origin.
	Client *http.Client

	once     sync.Once
	prefixes []string
}

func (l *Loader) init() {
	l.once.Do(func() {
		for p := range l.Files {
			l.prefixes = append(l.prefixes, p)
		}
		// The longest prefix first, so "/static/img/" wins over "/static/".
		sort.Slice(l.prefixes, func(i, j int) bool { return len(l.prefixes[i]) > len(l.prefixes[j]) })
		if l.Client == nil {
			l.Client = &http.Client{
				Timeout: Timeout,
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					if len(via) >= 3 || req.URL.Host != via[0].URL.Host || req.URL.Scheme != via[0].URL.Scheme {
						return fmt.Errorf("%w: a redirect off %s", ErrNotAllowed, via[0].URL.Host)
					}
					return nil
				},
			}
		}
	})
}

// Load fetches and decodes src.
func (l *Loader) Load(ctx context.Context, src string) (image.Image, error) {
	l.init()
	body, err := l.read(ctx, src)
	if err != nil {
		return nil, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("fetch: %s: %w", short(src), err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return nil, fmt.Errorf("fetch: %s is %d×%d, past the %d-pixel budget", short(src), cfg.Width, cfg.Height, MaxPixels)
	}
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("fetch: %s: %w", short(src), err)
	}
	return img, nil
}

func (l *Loader) read(ctx context.Context, src string) ([]byte, error) {
	switch {
	case strings.HasPrefix(src, "data:"):
		return decodeData(src)
	case strings.HasPrefix(src, "/"):
		return l.readFile(src)
	case strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://"):
		return l.readRemote(ctx, src)
	}
	return nil, fmt.Errorf("%w: %q is neither a path, an http(s) URL nor a data: URL", ErrNotAllowed, short(src))
}

func (l *Loader) readFile(src string) ([]byte, error) {
	u, err := url.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("fetch: %q: %w", src, err)
	}
	p := u.Path
	for _, prefix := range l.prefixes {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		name := strings.TrimPrefix(p, prefix)
		// fs.ValidPath refuses "..", absolute paths and empty elements: an
		// fs.FS is the boundary, and nothing is joined to a directory.
		if !fs.ValidPath(name) || path.Clean(name) != name {
			return nil, fmt.Errorf("%w: %q is not a file under %s", ErrNotAllowed, src, prefix)
		}
		f, err := l.Files[prefix].Open(name)
		if err != nil {
			return nil, fmt.Errorf("fetch: %s: %w", src, err)
		}
		defer f.Close()
		return readCapped(f, src)
	}
	return nil, fmt.Errorf("%w: %q is under no prefix in Files", ErrNotAllowed, src)
}

func (l *Loader) readRemote(ctx context.Context, src string) ([]byte, error) {
	u, err := url.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("fetch: %q: %w", short(src), err)
	}
	origin := u.Scheme + "://" + u.Host
	allowed := false
	for _, o := range l.Origins {
		if strings.TrimSuffix(o, "/") == origin {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf("%w: %s is not in ImageOrigins", ErrNotAllowed, origin)
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, err
	}
	resp, err := l.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %s: %w", short(src), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch: %s answered %s", short(src), resp.Status)
	}
	return readCapped(resp.Body, src)
}

func readCapped(r io.Reader, src string) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("fetch: %s: %w", short(src), err)
	}
	if len(body) > MaxBytes {
		return nil, fmt.Errorf("fetch: %s is larger than %d bytes", short(src), MaxBytes)
	}
	return body, nil
}

// decodeData reads a data: URL of a base64 or percent-encoded image.
func decodeData(src string) ([]byte, error) {
	comma := strings.IndexByte(src, ',')
	if comma < 0 {
		return nil, fmt.Errorf("fetch: a data: URL has no comma")
	}
	meta, data := src[len("data:"):comma], src[comma+1:]
	if !strings.HasPrefix(meta, "image/") {
		return nil, fmt.Errorf("%w: a data: URL of %q, not an image", ErrNotAllowed, meta)
	}
	if strings.HasSuffix(meta, ";base64") {
		if base64.StdEncoding.DecodedLen(len(data)) > MaxBytes {
			return nil, fmt.Errorf("fetch: a data: URL larger than %d bytes", MaxBytes)
		}
		return base64.StdEncoding.DecodeString(data)
	}
	s, err := url.PathUnescape(data)
	return []byte(s), err
}

func short(src string) string {
	if len(src) > 80 {
		return src[:77] + "..."
	}
	return src
}
