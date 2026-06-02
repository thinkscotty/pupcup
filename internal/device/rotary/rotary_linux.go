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
	dt, err := pin(cfg.DTPin, gpio.BothEdges)
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

// Buxton full-step quadrature decoder state machine. Tracks the Gray-code
// sequence across both CLK and DT, emitting exactly one event per detent and
// returning to R_START on any invalid (bounce) transition — so contact bounce
// and mid-detent reversals can't manufacture spurious counts the way the old
// single-edge DT sampler did. The encoder rests with both contacts open (both
// lines pulled high = pinstate 0b11), which is R_START's rest column.
//
// Adapted from Ben Buxton's rotary.cpp full-step table.
const (
	rStart    uint8 = 0x0
	rCWFinal  uint8 = 0x1
	rCWBegin  uint8 = 0x2
	rCWNext   uint8 = 0x3
	rCCWBegin uint8 = 0x4
	rCCWFinal uint8 = 0x5
	rCCWNext  uint8 = 0x6

	dirCW   uint8 = 0x10
	dirCCW  uint8 = 0x20
	dirMask uint8 = 0x30
)

// rotaryTable[state][pinstate] -> next state, with dir bits OR'd in on a
// completed detent. pinstate is (CLK<<1 | DT), columns 0b00..0b11.
var rotaryTable = [7][4]uint8{
	rStart:    {rStart, rCWBegin, rCCWBegin, rStart},
	rCWFinal:  {rCWNext, rStart, rCWFinal, rStart | dirCW},
	rCWBegin:  {rCWNext, rCWBegin, rStart, rStart},
	rCWNext:   {rCWNext, rCWBegin, rCWFinal, rStart},
	rCCWBegin: {rCCWNext, rStart, rCCWBegin, rStart},
	rCCWFinal: {rCCWNext, rCCWFinal, rStart, rStart | dirCCW},
	rCCWNext:  {rCCWNext, rCCWFinal, rCCWBegin, rStart},
}

func (d *linuxDriver) watchRotation() {
	defer d.wg.Done()

	// Two edge watchers feed a single decoder. We don't care which line moved
	// — every edge re-reads both pins and steps the state machine.
	ticks := make(chan struct{}, 8)
	var ww sync.WaitGroup
	ww.Add(2)
	go d.watchEdge(d.clk, ticks, &ww)
	go d.watchEdge(d.dt, ticks, &ww)
	defer ww.Wait()

	state := rStart
	for {
		select {
		case <-d.stop:
			return
		case <-ticks:
			state = rotaryTable[state&0x0f][d.pinstate()]
			switch state & dirMask {
			case dirCW:
				d.emitRotation(RotateCW)
			case dirCCW:
				d.emitRotation(RotateCCW)
			}
		}
	}
}

// watchEdge blocks on edges for a single pin and signals the decoder. The
// signal is level-less on purpose: the decoder samples both pins itself.
func (d *linuxDriver) watchEdge(p gpio.PinIO, ticks chan<- struct{}, ww *sync.WaitGroup) {
	defer ww.Done()
	for {
		select {
		case <-d.stop:
			return
		default:
		}
		if !p.WaitForEdge(50 * time.Millisecond) {
			continue
		}
		select {
		case ticks <- struct{}{}:
		case <-d.stop:
			return
		}
	}
}

// pinstate reads the live CLK/DT levels into the 2-bit (CLK<<1 | DT) index.
func (d *linuxDriver) pinstate() uint8 {
	var s uint8
	if d.clk.Read() == gpio.High {
		s |= 0b10
	}
	if d.dt.Read() == gpio.High {
		s |= 0b01
	}
	return s
}

func (d *linuxDriver) emitRotation(k EventKind) {
	if d.invert {
		if k == RotateCW {
			k = RotateCCW
		} else {
			k = RotateCW
		}
	}
	select {
	case d.out <- Event{Kind: k, TS: time.Now()}:
	case <-d.stop:
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
