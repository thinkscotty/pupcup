//go:build linux

package oled

import (
	"fmt"
	"image"
	"log/slog"
	"sync"

	"periph.io/x/conn/v3/i2c/i2creg"
	"periph.io/x/devices/v3/ssd1306"
)

// Config holds the SSD1306 wiring options.
type Config struct {
	I2CBus uint8  // 1 = /dev/i2c-1
	Addr   uint16 // typically 0x3C
}

// New opens the I2C bus, initializes the SSD1306, and returns a Renderer.
// The host package must be initialized (host.Init) before calling.
func New(cfg Config, log *slog.Logger) (Renderer, error) {
	if log == nil {
		log = slog.Default()
	}
	log = log.With("component", "device.oled")

	busName := fmt.Sprintf("%d", cfg.I2CBus)
	bus, err := i2creg.Open(busName)
	if err != nil {
		return nil, fmt.Errorf("oled: open i2c %s: %w", busName, err)
	}

	// Note: ssd1306.NewI2C hardcodes the I2C address to 0x3C; cfg.Addr is
	// validated upstream but not consumed here.
	_ = cfg.Addr
	opts := ssd1306.DefaultOpts
	opts.W = Width
	opts.H = Height
	dev, err := ssd1306.NewI2C(bus, &opts)
	if err != nil {
		_ = bus.Close()
		return nil, fmt.Errorf("oled: ssd1306 init: %w", err)
	}

	return &linuxRenderer{
		dev: dev,
		bus: bus,
		log: log,
	}, nil
}

type linuxRenderer struct {
	mu  sync.Mutex
	dev *ssd1306.Dev
	bus interface{ Close() error }
	log *slog.Logger
}

func (r *linuxRenderer) Render(s Scene) error {
	img := frame(s)
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dev.Draw(image.Rect(0, 0, Width, Height), img, image.Point{})
}

func (r *linuxRenderer) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.dev.Halt(); err != nil {
		r.log.Warn("ssd1306 halt", "err", err)
	}
	return r.bus.Close()
}
