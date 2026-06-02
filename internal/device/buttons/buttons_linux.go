//go:build linux

package buttons

import (
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"

	"github.com/scottyturner/pupcup/internal/domain"
)

// New constructs a hardware-backed Driver. Pins are BCM GPIO numbers; each
// pin is configured input pull-up with falling-edge detection. Debounce is
// applied per-pin (events within `debounce` of the previous on the same pin
// are dropped).
func New(pins Pins, debounce time.Duration, log *slog.Logger) (Driver, error) {
	if log == nil {
		log = slog.Default()
	}
	log = log.With("component", "device.buttons")

	d := &linuxDriver{
		out:      make(chan ButtonEvent, 16),
		log:      log,
		debounce: debounce,
		stop:     make(chan struct{}),
	}

	for color, pin := range pins {
		p := gpioreg.ByName(strconv.Itoa(pin))
		if p == nil {
			// Try GPIO<num> name as a fallback.
			p = gpioreg.ByName("GPIO" + strconv.Itoa(pin))
		}
		if p == nil {
			d.Close()
			return nil, fmt.Errorf("buttons: gpio pin %d not found", pin)
		}
		if err := p.In(gpio.PullUp, gpio.FallingEdge); err != nil {
			d.Close()
			return nil, fmt.Errorf("buttons: configure %s (gpio %d): %w", color, pin, err)
		}
		d.wg.Add(1)
		go d.watch(color, p)
	}
	return d, nil
}

type linuxDriver struct {
	out      chan ButtonEvent
	log      *slog.Logger
	debounce time.Duration
	stop     chan struct{}
	wg       sync.WaitGroup
	once     sync.Once
}

func (d *linuxDriver) Events() <-chan ButtonEvent { return d.out }

func (d *linuxDriver) Close() error {
	d.once.Do(func() {
		close(d.stop)
		d.wg.Wait()
		close(d.out)
	})
	return nil
}

func (d *linuxDriver) watch(color domain.ButtonColor, p gpio.PinIO) {
	defer d.wg.Done()
	var last time.Time
	for {
		select {
		case <-d.stop:
			return
		default:
		}
		// 50ms timeout so we periodically check the stop signal.
		if !p.WaitForEdge(50 * time.Millisecond) {
			continue
		}
		now := time.Now()
		if !last.IsZero() && now.Sub(last) < d.debounce {
			continue
		}
		// Confirm still low (true press, not bounce noise).
		if p.Read() != gpio.Low {
			continue
		}
		last = now
		select {
		case d.out <- ButtonEvent{Color: color, TS: now}:
		case <-d.stop:
			return
		}
	}
}
