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
// pin is configured input pull-up with both-edge detection. Debounce is
// applied per-pin (edges within `debounce` of the previous on the same pin
// are dropped). Active-low: a confirmed Low after a falling edge is a press, a
// confirmed High after a rising edge is a release.
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
		if err := p.In(gpio.PullUp, gpio.BothEdges); err != nil {
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
	var (
		last    time.Time
		lastLvl = gpio.High // released at rest (active-low, internal pull-up)
	)
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
		// Re-read to settle bounce and classify the edge. Active-low: Low is a
		// press, High a release. Drop edges that didn't change the level.
		lvl := p.Read()
		if lvl == lastLvl {
			continue
		}
		lastLvl = lvl
		last = now
		action := ActionRelease
		if lvl == gpio.Low {
			action = ActionPress
		}
		select {
		case d.out <- ButtonEvent{Color: color, Action: action, TS: now}:
		case <-d.stop:
			return
		}
	}
}
