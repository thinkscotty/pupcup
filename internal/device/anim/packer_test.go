package anim

import (
	"image"
	"testing"
)

// TestPackRGBARectKnownColors pins the RGB565 bit layout to externally-verifiable
// constants (these are the canonical 565 encodings), so a regression in the
// packer or a divergence from the driver's rgb565 fails loudly.
func TestPackRGBARectKnownColors(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 5, 1))
	set := func(x int, r, g, b uint8) {
		i := src.PixOffset(x, 0)
		src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = r, g, b, 0xFF
	}
	set(0, 255, 0, 0)        // red          -> F800
	set(1, 0, 255, 0)        // green        -> 07E0
	set(2, 0, 0, 255)        // blue         -> 001F
	set(3, 255, 255, 255)    // white        -> FFFF
	set(4, 0x12, 0x34, 0x56) // bit-trunc    -> 11AA

	dst := make([]byte, 5*2)
	PackRGBARect(dst, src, Rect{0, 0, 5, 1})

	want := []byte{0xF8, 0x00, 0x07, 0xE0, 0x00, 0x1F, 0xFF, 0xFF, 0x11, 0xAA}
	for i := range want {
		if dst[i] != want[i] {
			t.Fatalf("dst = % X\nwant = % X\n(first mismatch at byte %d)", dst, want, i)
		}
	}
}

// TestPackRGBARectSubRect proves the packer writes only the requested rectangle,
// at the correct full-frame byte offsets (stride = src width * 2), leaving every
// other byte untouched — the property that makes a pooled buffer reusable.
func TestPackRGBARectSubRect(t *testing.T) {
	const w, h = 4, 4
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(src.Pix); i += 4 { // blue everywhere
		src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = 0, 0, 255, 0xFF
	}
	dst := make([]byte, w*h*2)
	for i := range dst {
		dst[i] = 0xEE // sentinel
	}

	PackRGBARect(dst, src, Rect{1, 1, 3, 3}) // interior 2x2

	for _, p := range [][2]int{{1, 1}, {2, 1}, {1, 2}, {2, 2}} {
		off := (p[1]*w + p[0]) * 2
		if dst[off] != 0x00 || dst[off+1] != 0x1F {
			t.Fatalf("pixel %v (off %d) = %02x%02x, want 001F", p, off, dst[off], dst[off+1])
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x >= 1 && x < 3 && y >= 1 && y < 3 {
				continue
			}
			off := (y*w + x) * 2
			if dst[off] != 0xEE || dst[off+1] != 0xEE {
				t.Fatalf("pixel (%d,%d) outside rect was modified: %02x%02x", x, y, dst[off], dst[off+1])
			}
		}
	}
}

func TestRectHelpers(t *testing.T) {
	if !(Rect{5, 5, 5, 9}).Empty() {
		t.Fatal("zero-width rect should be empty")
	}
	if (Rect{0, 0, 2, 2}).Empty() {
		t.Fatal("2x2 rect should not be empty")
	}

	a, b := Rect{0, 0, 10, 10}, Rect{5, 5, 20, 8}
	if got, want := a.Union(b), (Rect{0, 0, 20, 10}); got != want {
		t.Fatalf("Union = %v, want %v", got, want)
	}
	if got := (Rect{}).Union(b); got != b {
		t.Fatalf("empty.Union(b) = %v, want %v", got, b)
	}
	if got := a.Union(Rect{}); got != a {
		t.Fatalf("a.Union(empty) = %v, want %v", got, a)
	}
	if got, want := (Rect{-3, -3, 100, 50}).Clamp(64, 64), (Rect{0, 0, 64, 50}); got != want {
		t.Fatalf("Clamp = %v, want %v", got, want)
	}
	if got, want := unionAll([]Rect{{2, 2, 4, 4}, {10, 1, 12, 9}, {}}), (Rect{2, 1, 12, 9}); got != want {
		t.Fatalf("unionAll = %v, want %v", got, want)
	}
	if !unionAll(nil).Empty() {
		t.Fatal("unionAll(nil) should be empty")
	}
}

func TestPackRGBARectZeroAlloc(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 64, 64))
	dst := make([]byte, 64*64*2)
	r := Rect{8, 8, 56, 56}
	if allocs := testing.AllocsPerRun(1000, func() { PackRGBARect(dst, src, r) }); allocs != 0 {
		t.Fatalf("PackRGBARect allocated %.2f objs/op, want 0", allocs)
	}
}
