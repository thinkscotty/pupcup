package display

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestRotateRGB565Geometry pins the pixel permutation for every rotation. Each
// source pixel is tagged with its logical (x,y) in its two bytes; after rotation
// the tag must appear at the panel position the documented forward map sends it
// to. (Direction labelling — that Rot90 is *clockwise* — is checked visually by
// TestRotateViewableGolden, which a self-consistent math test cannot prove.)
func TestRotateRGB565Geometry(t *testing.T) {
	const n = 5
	src := make([]byte, n*n*2)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			i := (y*n + x) * 2
			src[i], src[i+1] = byte(x), byte(y)
		}
	}
	cases := []struct {
		name string
		rot  Rotation
		fwd  func(lx, ly int) (px, py int)
	}{
		{"rot0", Rot0, func(lx, ly int) (int, int) { return lx, ly }},
		{"rot90", Rot90, func(lx, ly int) (int, int) { return n - 1 - ly, lx }},
		{"rot180", Rot180, func(lx, ly int) (int, int) { return n - 1 - lx, n - 1 - ly }},
		{"rot270", Rot270, func(lx, ly int) (int, int) { return ly, n - 1 - lx }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dst := make([]byte, n*n*2)
			gx0, gy0, gx1, gy1 := RotateRGB565(dst, src, n, 0, 0, n, n, c.rot)
			if gx0 != 0 || gy0 != 0 || gx1 != n || gy1 != n {
				t.Fatalf("full-frame panel rect = (%d,%d,%d,%d), want (0,0,%d,%d)", gx0, gy0, gx1, gy1, n, n)
			}
			for ly := 0; ly < n; ly++ {
				for lx := 0; lx < n; lx++ {
					px, py := c.fwd(lx, ly)
					di := (py*n + px) * 2
					if dst[di] != byte(lx) || dst[di+1] != byte(ly) {
						t.Fatalf("logical (%d,%d) -> panel (%d,%d): tag (%d,%d)", lx, ly, px, py, dst[di], dst[di+1])
					}
				}
			}
		})
	}
}

// TestRotate90FourTimesIdentity confirms four 90° turns return the original frame.
func TestRotate90FourTimesIdentity(t *testing.T) {
	const n = 7
	src := make([]byte, n*n*2)
	for i := range src {
		src[i] = byte(i*37 + 11) // deterministic, non-symmetric fill
	}
	a := make([]byte, n*n*2)
	b := make([]byte, n*n*2)
	RotateRGB565(a, src, n, 0, 0, n, n, Rot90)
	RotateRGB565(b, a, n, 0, 0, n, n, Rot90)
	RotateRGB565(a, b, n, 0, 0, n, n, Rot90)
	RotateRGB565(b, a, n, 0, 0, n, n, Rot90)
	if !bytes.Equal(b, src) {
		t.Fatal("Rot90 applied four times is not the identity")
	}
}

// TestRotatedFlusherMapsRect checks the decorator forwards the mapped panel rect
// to the inner flusher, and that Rot0 returns the inner flusher unwrapped.
func TestRotatedFlusherMapsRect(t *testing.T) {
	const n = 240
	buf := make([]byte, n*n*2)

	if f := NewFake(); NewRotatedFlusher(f, n, Rot0) != RectFlusher(f) {
		t.Fatal("Rot0 should return the inner flusher unwrapped")
	}

	cases := []struct {
		rot                Rotation
		x0, y0, x1, y1     int
		px0, py0, px1, py1 int
	}{
		{Rot90, 10, 20, 40, 60, n - 60, 10, n - 20, 40},
		{Rot180, 10, 20, 40, 60, n - 40, n - 60, n - 10, n - 20},
		{Rot270, 10, 20, 40, 60, 20, n - 40, 60, n - 10},
	}
	for _, c := range cases {
		f := NewFake()
		rf := NewRotatedFlusher(f, n, c.rot)
		if err := rf.FlushRect(buf, c.x0, c.y0, c.x1, c.y1); err != nil {
			t.Fatal(err)
		}
		x0, y0, x1, y1 := f.LastRect()
		if x0 != c.px0 || y0 != c.py0 || x1 != c.px1 || y1 != c.py1 {
			t.Errorf("rot=%d: panel rect (%d,%d,%d,%d), want (%d,%d,%d,%d)",
				c.rot, x0, y0, x1, y1, c.px0, c.py0, c.px1, c.py1)
		}
	}
}

// TestRotateViewableGolden renders an unambiguous "F" (no rotational or mirror
// symmetry) plus a red corner marker, rotates it every way, and compares against
// committed PNGs. Run with UI_GOLDEN_UPDATE=1 to (re)generate the goldens, then
// eyeball them: rotate_rot90cw.png must show the F turned clockwise. This is the
// only check that validates the *direction* of each Rotation, not just the math.
func TestRotateViewableGolden(t *testing.T) {
	const n = 120
	src := encode565(drawF(n), n)
	cases := []struct {
		name string
		rot  Rotation
	}{
		{"rot0", Rot0},
		{"rot90cw", Rot90},
		{"rot180", Rot180},
		{"rot270ccw", Rot270},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dst := make([]byte, n*n*2)
			RotateRGB565(dst, src, n, 0, 0, n, n, c.rot)
			got := decode565(dst, n)
			path := filepath.Join("testdata", "rotate_"+c.name+".png")
			if os.Getenv("UI_GOLDEN_UPDATE") != "" {
				writePNG(t, path, got)
				return
			}
			want := readPNG(t, path)
			if !imagesEqual(got, want) {
				t.Errorf("%s differs from golden %s (UI_GOLDEN_UPDATE=1 to regen)", c.name, path)
			}
		})
	}
}

// drawF paints a thick white block "F" on a dark background with a red square in
// the top-left corner, in an n×n RGBA. The glyph is chosen for having no symmetry,
// so any rotation is visually unambiguous.
func drawF(n int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	bg := color.RGBA{24, 24, 32, 255}
	fg := color.RGBA{240, 240, 240, 255}
	mark := color.RGBA{220, 40, 40, 255}
	fillRect := func(x0, y0, x1, y1 int, col color.RGBA) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				img.SetRGBA(x, y, col)
			}
		}
	}
	fillRect(0, 0, n, n, bg)
	// F: vertical spine + top bar + middle bar (coords scaled for n=120).
	fillRect(36, 24, 52, 100, fg) // spine
	fillRect(36, 24, 92, 40, fg)  // top bar
	fillRect(36, 56, 80, 70, fg)  // middle bar
	fillRect(6, 6, 22, 22, mark)  // top-left corner marker
	return img
}

func encode565(img *image.RGBA, n int) []byte {
	b := make([]byte, n*n*2)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			o := img.PixOffset(x, y)
			r, g, bl := img.Pix[o], img.Pix[o+1], img.Pix[o+2]
			v := (uint16(r&0xF8) << 8) | (uint16(g&0xFC) << 3) | (uint16(bl) >> 3)
			i := (y*n + x) * 2
			b[i], b[i+1] = byte(v>>8), byte(v)
		}
	}
	return b
}

func decode565(buf []byte, n int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			i := (y*n + x) * 2
			v := uint16(buf[i])<<8 | uint16(buf[i+1])
			r := byte((v >> 11) << 3)
			g := byte((v >> 5 & 0x3F) << 2)
			bl := byte((v & 0x1F) << 3)
			img.SetRGBA(x, y, color.RGBA{r, g, bl, 255})
		}
	}
	return img
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func readPNG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open golden %s: %v (UI_GOLDEN_UPDATE=1 to generate)", path, err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func imagesEqual(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	bnds := a.Bounds()
	for y := bnds.Min.Y; y < bnds.Max.Y; y++ {
		for x := bnds.Min.X; x < bnds.Max.X; x++ {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab != bb || aa != ba {
				return false
			}
		}
	}
	return true
}
