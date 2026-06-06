// Package gc9a01 is a pure-Go SPI driver for the GC9A01 240x240 round RGB
// LCD. The panel is write-only 4-wire SPI: the DC GPIO line selects
// command vs data, the kernel SPI driver asserts CS, and pixels are 16-bit
// RGB565. periph.io ships no GC9A01 driver, so the controller bring-up and
// framebuffer streaming are hand-rolled here, mirroring the NeoPixel SPI
// pattern (open port, Connect Mode0/8-bit, Tx) and the rotary GPIO pattern
// (gpioreg.ByName with a "GPIO"+n fallback).
package gc9a01

import (
	"errors"
	"sync"

	"github.com/scottyturner/pupcup/internal/device/display"
)

// Width and Height of the GC9A01 GRAM. The panel is square; the visible area
// is the inscribed circle, so scene content should stay near the center.
const (
	Width  = 240
	Height = 240
)

// Config holds the GC9A01 wiring: the SPI device (SPI1 on this build) plus
// the two plain GPIO lines the driver toggles directly (DC and RST). CS is
// the kernel-asserted SPI chip-select and is not listed here.
type Config struct {
	Device  string // e.g. /dev/spidev1.0
	DCPin   int    // data/command select (BCM)
	RSTPin  int    // reset (BCM)
	SpeedHz int    // SPI clock in Hz; 0 uses the driver default (40 MHz)
}

// New returns a display.Renderer (see *_linux.go / *_stub.go), so the daemon
// drives the LCD through the same scene contract as the OLED. Prober is the raw
// bring-up surface — solid fills and the wiring/color test pattern — that the
// hwprobe recovers via a type assertion on the returned Renderer.
type Prober interface {
	// FillRGB paints the whole panel a single color.
	FillRGB(r, g, b uint8) error
	// DrawTestPattern paints colored quadrants + a center crosshair to verify
	// addressing, orientation, and color order.
	DrawTestPattern() error
	// FlushRect streams the sub-rectangle [x0,x1) x [y0,y1) of buf — a
	// full-frame Width*Height*2 RGB565 framebuffer (row stride = Width) — to the
	// panel via a partial address window. FlushRect(buf, 0, 0, Width, Height) is
	// a full-frame blit. This is the dirty-rectangle path the animation engine
	// and the lcdperf probe drive.
	FlushRect(buf []byte, x0, y0, x1, y1 int) error
}

// ErrUnavailable is returned by hardware constructors on platforms without SPI.
var ErrUnavailable = errors.New("gc9a01: hardware unavailable on this platform")

// Compile-time proof the Fake satisfies both surfaces. The real linuxDev is
// checked the same way in gc9a01_linux.go.
var (
	_ display.Renderer = (*Fake)(nil)
	_ Prober           = (*Fake)(nil)
)

// rgb565 packs an 8-bit RGB color into 16-bit RGB565, returned as the two
// bytes the GC9A01 expects per pixel (most-significant byte first).
//
// If colors come out swapped on hardware (e.g. red shows as blue), the panel
// is interpreting the data as BGR: either clear MADCTL bit 3 (0x08→0x00) in
// the init sequence, or swap r<->b here. This is the single line to change.
func rgb565(r, g, b uint8) (hi, lo byte) {
	v := (uint16(r&0xF8) << 8) | (uint16(g&0xFC) << 3) | (uint16(b) >> 3)
	return byte(v >> 8), byte(v)
}

// NewFake returns an in-memory Driver for non-Linux dev/builds and tests. It
// records the most recent fill color so callers can assert against it.
func NewFake() *Fake { return &Fake{} }

type Fake struct {
	mu        sync.Mutex
	lastFill  [3]uint8
	lastScene display.Scene
	fills     int
	patterns  int
	rects     int
	renders   int
	closed    bool
}

// Render records the most recent scene (display.Renderer).
func (f *Fake) Render(s display.Scene) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return errors.New("gc9a01: closed")
	}
	f.lastScene = s
	f.renders++
	return nil
}

// Last returns the most recently rendered scene (nil if none).
func (f *Fake) Last() display.Scene {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastScene
}

func (f *Fake) FillRGB(r, g, b uint8) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return errors.New("gc9a01: closed")
	}
	f.lastFill = [3]uint8{r, g, b}
	f.fills++
	return nil
}

func (f *Fake) DrawTestPattern() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return errors.New("gc9a01: closed")
	}
	f.patterns++
	return nil
}

// FlushRect records a partial-window flush (gc9a01.Prober). The Fake keeps no
// pixels; tests assert call counts via Rects().
func (f *Fake) FlushRect(buf []byte, x0, y0, x1, y1 int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return errors.New("gc9a01: closed")
	}
	f.rects++
	return nil
}

func (f *Fake) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// LastFill returns the most recent FillRGB color (zero value if none).
func (f *Fake) LastFill() (r, g, b uint8) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastFill[0], f.lastFill[1], f.lastFill[2]
}

// Rects returns the number of FlushRect calls.
func (f *Fake) Rects() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rects
}
