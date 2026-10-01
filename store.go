package ogimage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// validHash is a card's name without its extension: the hex of 16 bytes.
var validHash = regexp.MustCompile(`^[0-9a-f]{32}$`)

// store keeps what a card is made of — its spec, the executed HTML — and the PNG
// drawn from it, in memory and, with a directory, on disk (DESIGN.md §8.1). A spec
// on disk is what lets a restarted process draw a card a page cached before the
// restart still names.
type store struct {
	dir        string
	maxEntries int
	maxBytes   int64

	mu      sync.Mutex
	entries map[string]*entry
	bytes   int64
}

type entry struct {
	html string
	png  []byte
	// used is when the spec was last recorded or the image last served:
	// eviction drops the least recently used first.
	used time.Time
}

func newStore(dir string, maxEntries int, maxBytes int64) *store {
	s := &store{dir: dir, maxEntries: maxEntries, maxBytes: maxBytes, entries: map[string]*entry{}}
	if dir != "" {
		_ = os.MkdirAll(filepath.Join(dir, "specs"), 0o755)
		_ = os.MkdirAll(filepath.Join(dir, "png"), 0o755)
	}
	return s
}

func (s *store) specPath(hash string) string { return filepath.Join(s.dir, "specs", hash+".html") }
func (s *store) pngPath(hash string) string  { return filepath.Join(s.dir, "png", hash+".png") }

// record keeps a card's spec. Recording one already kept only marks it used.
func (s *store) record(hash, html string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[hash]; ok {
		e.used = time.Now()
		return
	}
	s.entries[hash] = &entry{html: html, used: time.Now()}
	s.bytes += int64(len(html))
	if s.dir != "" {
		if _, err := os.Stat(s.specPath(hash)); errors.Is(err, fs.ErrNotExist) {
			_ = writeAtomic(s.specPath(hash), []byte(html))
		}
	}
	s.evict()
}

// spec returns a card's HTML: from memory, or from disk after a restart.
func (s *store) spec(hash string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[hash]; ok {
		return e.html, true
	}
	if s.dir == "" {
		return "", false
	}
	b, err := os.ReadFile(s.specPath(hash))
	if err != nil {
		return "", false
	}
	s.entries[hash] = &entry{html: string(b), used: time.Now()}
	s.bytes += int64(len(b))
	s.evict()
	return string(b), true
}

// image returns a card's PNG, if it has been drawn: from memory, or from disk.
func (s *store) image(hash string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.entries[hash]; ok && e.png != nil {
		e.used = time.Now()
		return e.png, true
	}
	if s.dir == "" {
		return nil, false
	}
	b, err := os.ReadFile(s.pngPath(hash))
	if err != nil {
		return nil, false
	}
	if e, ok := s.entries[hash]; ok {
		e.png, e.used = b, time.Now()
		s.bytes += int64(len(b))
		s.evict()
	}
	return b, true
}

// put keeps a drawn card's PNG.
func (s *store) put(hash string, png []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[hash]
	if !ok {
		return
	}
	if e.png == nil {
		s.bytes += int64(len(png))
	}
	e.png, e.used = png, time.Now()
	if s.dir != "" {
		_ = writeAtomic(s.pngPath(hash), png)
	}
	s.evict()
}

// evict drops the least recently used cards while the store is past a cap. A
// dropped card's files go too: its spec can be recorded again by its page, and
// its image drawn again from that. Called with s.mu held.
func (s *store) evict() {
	over := func() bool {
		return (s.maxEntries > 0 && len(s.entries) > s.maxEntries) || (s.maxBytes > 0 && s.bytes > s.maxBytes)
	}
	if !over() {
		return
	}
	hashes := make([]string, 0, len(s.entries))
	for h := range s.entries {
		hashes = append(hashes, h)
	}
	sort.Slice(hashes, func(i, j int) bool { return s.entries[hashes[i]].used.Before(s.entries[hashes[j]].used) })
	for _, h := range hashes {
		if !over() {
			return
		}
		e := s.entries[h]
		s.bytes -= int64(len(e.html) + len(e.png))
		delete(s.entries, h)
		if s.dir != "" {
			_ = os.Remove(s.specPath(h))
			_ = os.Remove(s.pngPath(h))
		}
	}
}

// writeAtomic writes through a temporary file and a rename, so a reader never
// sees half a card.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
