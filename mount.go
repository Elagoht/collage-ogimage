package ogimage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"
)

// drawCall is one card being drawn, shared by every request for it.
type drawCall struct {
	done chan struct{}
	png  []byte
	err  error
}

// image returns a card's PNG: kept, or drawn now from its spec. A hash no
// render recorded is fs.ErrNotExist — nothing is drawn for it (DESIGN.md §9).
func (p *Plugin) image(ctx context.Context, hash string) ([]byte, error) {
	if !p.devMode {
		if b, ok := p.store.image(hash); ok {
			return b, nil
		}
	}
	spec, ok := p.store.spec(hash)
	if !ok {
		return nil, fs.ErrNotExist
	}

	p.mu.Lock()
	if call, ok := p.inflight[hash]; ok {
		p.mu.Unlock()
		select {
		case <-call.done:
			return call.png, call.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	call := &drawCall{done: make(chan struct{})}
	p.inflight[hash] = call
	p.mu.Unlock()

	call.png, call.err = p.drawBounded(ctx, spec)
	if call.err == nil && !p.devMode {
		p.store.put(hash, call.png)
	}
	p.mu.Lock()
	delete(p.inflight, hash)
	p.mu.Unlock()
	close(call.done)
	return call.png, call.err
}

// drawBounded draws a spec to PNG within the plugin's limits: a worker slot, and
// RenderTimeout for the whole of it.
func (p *Plugin) drawBounded(ctx context.Context, spec string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.cfg.RenderTimeout))
	defer cancel()
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("ogimage: no worker free within %s: %w", time.Duration(p.cfg.RenderTimeout), ctx.Err())
	}
	type result struct {
		png []byte
		err error
	}
	out := make(chan result, 1)
	go func() {
		defer func() { <-p.sem }()
		locale, html := decodeSpec(spec)
		img, err := p.renderer.draw(ctx, html, locale)
		if err != nil {
			out <- result{err: err}
			return
		}
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			out <- result{err: err}
			return
		}
		out <- result{png: b.Bytes()}
	}()
	select {
	case r := <-out:
		return r.png, r.err
	case <-ctx.Done():
		return nil, fmt.Errorf("ogimage: a card took longer than %s to draw: %w", time.Duration(p.cfg.RenderTimeout), ctx.Err())
	}
}

// cardFS is what Prefix serves: <hash>.png, drawn on first request.
type cardFS struct{ plugin *Plugin }

func (f *cardFS) Open(name string) (fs.File, error) {
	name = path.Clean(name)
	if name == "." {
		return &dirFile{}, nil
	}
	hash, ok := strings.CutSuffix(name, ".png")
	if !ok || !validHash.MatchString(hash) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	body, err := f.plugin.image(context.Background(), hash)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	if err != nil {
		f.plugin.log.Warn("ogimage: a card could not be drawn", "card", name, "err", err)
		// Not ErrNotExist: the card is one a page promised, and its failure is
		// the server's.
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}
	return &cardFile{Reader: bytes.NewReader(body), info: fileInfo{name: name, size: int64(len(body))}}, nil
}

type cardFile struct {
	*bytes.Reader
	info fileInfo
}

func (f *cardFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *cardFile) Close() error               { return nil }

var _ io.ReadSeeker = (*cardFile)(nil)

// dirFile is the mount's root: nothing is listed, since a card exists only once
// a page has recorded it.
type dirFile struct{}

func (d *dirFile) Stat() (fs.FileInfo, error) { return fileInfo{name: ".", dir: true}, nil }
func (d *dirFile) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: ".", Err: fs.ErrInvalid}
}
func (d *dirFile) Close() error                       { return nil }
func (d *dirFile) ReadDir(int) ([]fs.DirEntry, error) { return nil, nil }

type fileInfo struct {
	name string
	size int64
	dir  bool
}

func (i fileInfo) Name() string { return i.name }
func (i fileInfo) Size() int64  { return i.size }
func (i fileInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (i fileInfo) ModTime() time.Time { return time.Time{} }
func (i fileInfo) IsDir() bool        { return i.dir }
func (i fileInfo) Sys() any           { return nil } // any: fs.FileInfo's own signature
