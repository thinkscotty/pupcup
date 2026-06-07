package photocache

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scottyturner/pupcup/internal/domain"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestDecodeAvatarCropsCenteredSquare verifies a non-square source is cropped to
// its centered square (the cropped-away margins must not survive) and resampled to
// size×size. The centered square is filled white and the margins red; after a
// correct centered crop the output is uniformly white.
func TestDecodeAvatarCropsCenteredSquare(t *testing.T) {
	cases := []struct {
		name string
		w, h int
	}{
		{"landscape", 200, 120},
		{"portrait", 120, 200},
		{"square", 150, 150},
	}
	const size = 64
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
			fill(src, color.RGBA{220, 0, 0, 255}) // red margins (to be cropped away)
			side := c.w
			if c.h < side {
				side = c.h
			}
			sx, sy := (c.w-side)/2, (c.h-side)/2
			fillRect(src, sx, sy, sx+side, sy+side, color.RGBA{255, 255, 255, 255}) // centered white square

			out, err := decodeAvatar(encodePNG(t, src), size)
			if err != nil {
				t.Fatal(err)
			}
			if out.Bounds().Dx() != size || out.Bounds().Dy() != size {
				t.Fatalf("output %v, want %dx%d", out.Bounds(), size, size)
			}
			// The whole output must be (near) white: the red margins were cropped.
			for y := 0; y < size; y++ {
				for x := 0; x < size; x++ {
					r := out.RGBAAt(x, y)
					if r.G < 240 || r.B < 240 {
						t.Fatalf("pixel (%d,%d)=%+v not white — crop kept the margins", x, y, r)
					}
				}
			}
		})
	}
}

func TestDecodeAvatarRejectsGarbage(t *testing.T) {
	if _, err := decodeAvatar(bytes.NewReader([]byte("not an image")), 64); err == nil {
		t.Fatal("expected a decode error for non-image bytes")
	}
}

// TestCachePhotoLifecycle drives the cache through the states the device hits:
// missing file, first decode, cache hit, on-disk replacement, no-photo dog, and a
// corrupt file.
func TestCachePhotoLifecycle(t *testing.T) {
	dir := t.TempDir()
	c := New(dir, 64, quiet())
	dog := domain.Dog{ID: 5, PhotoPath: "dog-5.png"}
	path := filepath.Join(dir, "dog-5.png")

	if got := c.Photo(dog); got != nil {
		t.Fatal("missing file should yield nil")
	}

	writePNG(t, path, solid(80, 80, color.RGBA{0, 200, 0, 255})) // green
	setMTime(t, path, time.Unix(1_000_000, 0))
	img := c.Photo(dog)
	if img == nil {
		t.Fatal("present photo should decode")
	}
	if img.Bounds().Dx() != 64 || img.Bounds().Dy() != 64 {
		t.Fatalf("decoded size %v, want 64x64", img.Bounds())
	}
	if g := img.RGBAAt(32, 32).G; g < 200 {
		t.Fatalf("center G=%d, want green", g)
	}

	if again := c.Photo(dog); again != img {
		t.Fatal("unchanged file should return the cached pointer")
	}

	// Replace the file's content and bump its mtime: the cache must re-decode.
	writePNG(t, path, solid(80, 80, color.RGBA{0, 0, 220, 255})) // blue
	setMTime(t, path, time.Unix(2_000_000, 0))
	img2 := c.Photo(dog)
	if img2 == img {
		t.Fatal("changed file should re-decode (new pointer)")
	}
	if b := img2.RGBAAt(32, 32).B; b < 200 {
		t.Fatalf("center B=%d, want blue after replacement", b)
	}

	if got := c.Photo(domain.Dog{ID: 6}); got != nil {
		t.Fatal("dog with empty PhotoPath should yield nil")
	}

	garbage := filepath.Join(dir, "dog-7.png")
	if err := os.WriteFile(garbage, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := c.Photo(domain.Dog{ID: 7, PhotoPath: "dog-7.png"}); got != nil {
		t.Fatal("undecodable file should yield nil, not panic")
	}
}

func TestCacheEmptyDirDisablesPhotos(t *testing.T) {
	c := New("", 64, quiet())
	if got := c.Photo(domain.Dog{ID: 1, PhotoPath: "dog-1.png"}); got != nil {
		t.Fatal("empty dir should disable photos")
	}
}

func TestCacheNilReceiver(t *testing.T) {
	var c *Cache
	if got := c.Photo(domain.Dog{ID: 1, PhotoPath: "dog-1.png"}); got != nil {
		t.Fatal("nil cache should return nil, not panic")
	}
}

// --- helpers ---

func fill(img *image.RGBA, col color.RGBA) {
	fillRect(img, img.Bounds().Min.X, img.Bounds().Min.Y, img.Bounds().Max.X, img.Bounds().Max.Y, col)
}

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, col color.RGBA) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			img.SetRGBA(x, y, col)
		}
	}
}

func solid(w, h int, col color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fill(img, col)
	return img
}

func encodePNG(t *testing.T, img image.Image) io.Reader {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return &buf
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func setMTime(t *testing.T, path string, mt time.Time) {
	t.Helper()
	if err := os.Chtimes(path, mt, mt); err != nil {
		t.Fatal(err)
	}
}
