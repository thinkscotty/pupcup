//go:build linux

package neopixel

import (
	"fmt"
	"log/slog"
	"sync"

	"periph.io/x/conn/v3/physic"
	"periph.io/x/conn/v3/spi"
	"periph.io/x/conn/v3/spi/spireg"
)

// Config holds the SPI wiring options for a single strip.
type Config struct {
	Device string // /dev/spidev0.0
	N      int    // pixel count
}

// New opens the SPI port at 2.4 MHz Mode0 8-bit and returns a Strip backed
// by the bit-banged WS encoding. Host package must be initialized.
func New(cfg Config, log *slog.Logger) (Strip, error) {
	if cfg.N < 1 {
		return nil, fmt.Errorf("neopixel: pixel count must be >= 1")
	}
	if log == nil {
		log = slog.Default()
	}
	log = log.With("component", "device.neopixel")

	port, err := spireg.Open(cfg.Device)
	if err != nil {
		return nil, fmt.Errorf("neopixel: open spi %s: %w", cfg.Device, err)
	}
	conn, err := port.Connect(2400*physic.KiloHertz, spi.Mode0, 8)
	if err != nil {
		_ = port.Close()
		return nil, fmt.Errorf("neopixel: connect: %w", err)
	}
	return &linuxStrip{
		port: port,
		conn: conn,
		pix:  make([]Color, cfg.N),
		buf:  make([]byte, cfg.N*encodeBitsPerLED+resetBytes),
		log:  log,
	}, nil
}

type linuxStrip struct {
	mu   sync.Mutex
	port spi.PortCloser
	conn spi.Conn
	pix  []Color
	buf  []byte
	log  *slog.Logger
}

func (s *linuxStrip) N() int { return len(s.pix) }

func (s *linuxStrip) SetAll(c Color) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.pix {
		s.pix[i] = c
	}
	return nil
}

func (s *linuxStrip) SetPixel(i int, c Color) error {
	if i < 0 || i >= len(s.pix) {
		return fmt.Errorf("neopixel: pixel %d out of range [0,%d)", i, len(s.pix))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pix[i] = c
	return nil
}

func (s *linuxStrip) Show() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.buf {
		s.buf[i] = 0
	}
	encodeFrame(s.buf, s.pix)
	return s.conn.Tx(s.buf, nil)
}

func (s *linuxStrip) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.buf {
		s.buf[i] = 0
	}
	encodeFrame(s.buf, make([]Color, len(s.pix))) // turn LEDs off
	_ = s.conn.Tx(s.buf, nil)
	return s.port.Close()
}
