// Package photocache turns a dog's uploaded photo into a circle-ready square
// RGBA for the round-LCD avatar, decoding each file once and re-decoding only
// when it changes on disk. The web uploader and the device run in the same
// process, so a freshly uploaded photo is immediately loadable here.
package photocache

import (
	"image"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/scottyturner/pupcup/internal/domain"
)

// Cache loads and resamples dog photos for the avatar.
//
// It is designed for use from a SINGLE goroutine — the device's render/run loop,
// via the GC9A01 driver's Render — so it holds no lock. Decoded images are never
// mutated after creation, so handing a returned *image.RGBA to the animation
// engine's goroutine (through the scene model) is race-free.
type Cache struct {
	dir  string
	size int
	log  *slog.Logger
	m    map[int64]entry
}

// entry is a decoded photo plus the on-disk identity it came from, so a replaced
// file (same name, new content) is detected by a changed mtime/size and reloaded.
// img is nil when the dog has no usable photo (absent or undecodable); that nil
// is cached too, so a bad file is not re-decoded every frame.
type entry struct {
	path  string
	mtime time.Time
	size  int64
	img   *image.RGBA
}

// New returns a cache that loads photos from dir and produces size×size avatars.
// An empty dir disables photos (Photo always returns nil). A nil logger defaults
// to slog.Default.
func New(dir string, size int, log *slog.Logger) *Cache {
	if log == nil {
		log = slog.Default()
	}
	return &Cache{
		dir:  dir,
		size: size,
		log:  log.With("subcomponent", "photocache"),
		m:    make(map[int64]entry),
	}
}

// Photo returns the dog's circle-ready avatar image, or nil when the dog has no
// photo (the caller then falls back to the accent disc + initial). It re-decodes
// only when the file's mtime/size changes. A missing file returns nil without
// caching, so a just-uploaded photo appears on the next render; an undecodable
// file caches nil so it is not retried until it changes. Safe on a nil receiver.
func (c *Cache) Photo(dog domain.Dog) *image.RGBA {
	if c == nil || c.dir == "" || dog.PhotoPath == "" {
		return nil
	}
	path := c.resolve(dog.PhotoPath)
	st, err := os.Stat(path)
	if err != nil {
		return nil // not present (yet); cheap to retry next render
	}
	if e, ok := c.m[dog.ID]; ok && e.path == path && e.mtime == st.ModTime() && e.size == st.Size() {
		return e.img
	}
	img, err := c.load(path)
	if err != nil {
		c.log.Warn("decode dog photo", "path", path, "err", err)
		img = nil // cache the failure (keyed to this mtime) so we don't re-decode each frame
	}
	c.m[dog.ID] = entry{path: path, mtime: st.ModTime(), size: st.Size(), img: img}
	return img
}

func (c *Cache) load(path string) (*image.RGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return decodeAvatar(f, c.size)
}

// resolve joins the relative PhotoPath under dir. Forcing it absolute then
// cleaning strips any ".." before re-rooting under dir, so a crafted path can't
// escape (mirrors web/photos.go safeJoin). The value comes from our own DB
// ("dog-<id>.<ext>"), so this is purely defensive.
func (c *Cache) resolve(rel string) string {
	return filepath.Join(c.dir, filepath.Clean("/"+rel))
}
