// Package neopixel is a pure-Go SPI driver for WS2812 / SK6812 LEDs. Each
// WS data bit is encoded as 3 SPI bits at 2.4 MHz, giving canonical 800 kHz
// (1.25 µs) WS timing.
//
//	1 → 0b110   (HIGH ~833 ns, LOW ~417 ns)
//	0 → 0b100   (HIGH ~417 ns, LOW ~833 ns)
//
// Per-frame buffer = nLEDs × 9 bytes + reset gap. Allocated once in
// NewStrip(n) so Show() does not allocate.
package neopixel

import (
	"errors"
	"sync"
)

// Color is 24-bit RGB. White-channel handling for SK6812 RGBW is left to a
// future revision; for the v1 LED bar (status colors) RGB suffices.
type Color struct{ R, G, B uint8 }

var (
	ColorOff   = Color{}
	ColorWhite = Color{255, 255, 255}
)

// Strip is the public driver interface.
type Strip interface {
	SetAll(c Color) error
	SetPixel(i int, c Color) error
	Show() error
	Close() error
	N() int
}

var ErrUnavailable = errors.New("neopixel: hardware unavailable on this platform")

// encodeBitsPerLED is the number of SPI bytes consumed per WS LED:
// 24 data bits × 3 SPI bits = 72 bits = 9 bytes.
const encodeBitsPerLED = 9

// resetBytes pads the end of the frame to ensure ≥ 50 µs of low signal so the
// strip latches. At 2.4 MHz a single byte takes 8 / 2.4e6 ≈ 3.33 µs, so 30
// bytes ≈ 100 µs.
const resetBytes = 30

// encodeFrame writes the SPI byte stream for the given pixel buffer to dst,
// which must be exactly nLEDs*encodeBitsPerLED + resetBytes bytes. WS2812
// receives bits in G-R-B order, MSB first.
func encodeFrame(dst []byte, pixels []Color) {
	pos := 0
	for _, c := range pixels {
		// Encode each byte of the GRB triplet.
		for _, b := range [3]uint8{c.G, c.R, c.B} {
			for bit := 7; bit >= 0; bit-- {
				// Each WS bit becomes 3 SPI bits.
				var triplet uint8
				if (b>>uint(bit))&1 == 1 {
					triplet = 0b110
				} else {
					triplet = 0b100
				}
				// Write 3 bits into dst at bit-offset pos*3 from MSB-first start.
				bitOff := pos * 3
				bytePos := bitOff / 8
				bitPos := bitOff % 8
				// Shift the triplet into the right place, possibly spanning bytes.
				v := uint16(triplet) << uint(13-bitPos) // 16-bit slot, top-aligned
				dst[bytePos] |= byte(v >> 8)
				if bytePos+1 < len(dst) {
					dst[bytePos+1] |= byte(v & 0xFF)
				}
				pos++
			}
		}
	}
	// Reset gap is already zero-initialized by callers (they reset the slice).
}

// NewFake returns an in-memory strip useful for tests.
func NewFake(n int) *Fake {
	return &Fake{pix: make([]Color, n)}
}

type Fake struct {
	mu      sync.Mutex
	pix     []Color
	frames  [][]Color
	closed  bool
}

func (f *Fake) N() int { return len(f.pix) }

func (f *Fake) SetAll(c Color) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return errors.New("neopixel: closed")
	}
	for i := range f.pix {
		f.pix[i] = c
	}
	return nil
}

func (f *Fake) SetPixel(i int, c Color) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return errors.New("neopixel: closed")
	}
	if i < 0 || i >= len(f.pix) {
		return errors.New("neopixel: pixel index out of range")
	}
	f.pix[i] = c
	return nil
}

func (f *Fake) Show() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return errors.New("neopixel: closed")
	}
	cp := make([]Color, len(f.pix))
	copy(cp, f.pix)
	f.frames = append(f.frames, cp)
	return nil
}

func (f *Fake) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// LastFrame returns the most recently rendered frame, or nil if none.
func (f *Fake) LastFrame() []Color {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.frames) == 0 {
		return nil
	}
	return f.frames[len(f.frames)-1]
}

// FrameCount returns the number of Show calls.
func (f *Fake) FrameCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.frames)
}
