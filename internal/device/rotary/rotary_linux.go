//go:build linux

package rotary

import (
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
)

// New constructs a hardware-backed rotary driver. CLK + DT for quadrature,
// SW for the integrated push-button. All pins use internal pull-ups.
func New(cfg Config, log *slog.Logger) (Driver, error) {
	if log == nil {
		log = slog.Default()
	}
	log = log.With("component", "device.rotary")

	clk, err := pin(cfg.CLKPin, gpio.BothEdges)
	if err != nil {
		return nil, fmt.Errorf("rotary CLK: %w", err)
	}
	dt, err := pin(cfg.DTPin, gpio.NoEdge)
	if err != nil {
		return nil, fmt.Errorf("rotary DT: %w", err)
	}
	sw, err := pin(cfg.SWPin, gpio.BothEdges)
	if err != nil {
		return nil, fmt.Errorf("rotary SW: %w", err)
	}

	d := &linuxDriver{
		out:       make(chan Event, 16),
		stop:      make(chan struct{}),
		log:       log,
		debounce:  cfg.Debounce,
		longPress: cfg.LongPress,
		invert:    cfg.Invert,
		clk:       clk,
		dt:        dt,
		sw:        sw,
	}
	d.wg.Add(2)
	go d.watchRotation()
	go d.watchSwitch()
	return d, nil
}

func pin(num int, edge gpio.Edge) (gpio.PinIO, error) {
	p := gpioreg.ByName(strconv.Itoa(num))
	if p == nil {
		p = gpioreg.ByName("GPIO" + strconv.Itoa(num))
	}
	if p == nil {
		return nil, fmt.Errorf("gpio %d not found", num)
	}
	if err := p.In(gpio.PullUp, edge); err != nil {
		return nil, err
	}
	return p, nil
}

type linuxDriver struct {
	out               chan Event
	stop              chan struct{}
	log               *slog.Logger
	debounce, longPress time.Duration
	invert            bool
	clk, dt, sw       gpio.PinIO
	wg                sync.WaitGroup
	once              sync.Once
}

func (d *linuxDriver) Events() <-chan Event { return d.out }

func (d *linuxDriver) Close() error {
	d.once.Do(func() {
		close(d.stop)
		d.wg.Wait()
		close(d.out)
	})
	return nil
}

func (d *linuxDriver) watchRotation() {
	defer d.wg.Done()
	var last time.Time
	for {
		select {
		case <-d.stop:
			return
		default:
		}
		if !d.clk.WaitForEdge(50 * time.Millisecond) {
			continue
		}
		now := time.Now()
		if !last.IsZero() && now.Sub(last) < d.debounce {
			continue
		}
		// Detent occurs on falling edge of CLK on most KY-040 modules.
		if d.clk.Read() != gpio.Low {
			continue
		}
		last = now
		var k EventKind
		if d.dt.Read() == gpio.High {
			k = RotateCW
		} else {
			k = RotateCCW
		}
		if d.invert {
			if k == RotateCW {
				k = RotateCCW
			} else {
				k = RotateCW
			}
		}
		select {
		case d.out <- Event{Kind: k, TS: now}:
		case <-d.stop:
			return
		}
	}
}

func (d *linuxDriver) watchSwitch() {
	defer d.wg.Done()
	var pressedAt time.Time
	var longFired bool
	for {
		select {
		case <-d.stop:
			return
		default:
		}
		if !d.sw.WaitForEdge(50 * time.Millisecond) {
			// While held, check for long-press cross.
			if !pressedAt.IsZero() && !longFired && time.Since(pressedAt) >= d.longPress {
				now := time.Now()
				select {
				case d.out <- Event{Kind: PressLong, TS: now}:
				case <-d.stop:
					return
				}
				longFired = true
			}
			continue
		}
		state := d.sw.Read()
		if state == gpio.Low { // press
			pressedAt = time.Now()
			longFired = false
		} else { // release
			if pressedAt.IsZero() {
				continue
			}
			held := time.Since(pressedAt)
			pressedAt = time.Time{}
			if longFired {
				// Already emitted PressLong; release is silent.
				continue
			}
			if held < 25*time.Millisecond {
				continue // bounce
			}
			select {
			case d.out <- Event{Kind: PressShort, TS: time.Now()}:
			case <-d.stop:
				return
			}
		}
	}
}
