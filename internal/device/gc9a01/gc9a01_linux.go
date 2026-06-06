//go:build linux

package gc9a01

import (
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/conn/v3/spi"
	"periph.io/x/conn/v3/spi/spireg"

	"github.com/scottyturner/pupcup/internal/device/anim"
	"github.com/scottyturner/pupcup/internal/device/display"
	"github.com/scottyturner/pupcup/internal/device/ui"
)

// linuxDev satisfies the scene-rendering contract, the raw bring-up surface, and
// the optional dirty-rect + celebration capabilities the animation engine uses.
var (
	_ display.Renderer    = (*linuxDev)(nil)
	_ Prober              = (*linuxDev)(nil)
	_ display.RectFlusher = (*linuxDev)(nil)
	_ display.Celebrator  = (*linuxDev)(nil)
)

// animFPS is the engine's render-loop rate. 30 Hz is a deliberate ambient-device
// default: smooth enough for the celebrations, kinder to the shared core than 60
// for the near-full-frame idle breath. Tune against on-device CPU in UAT.
const animFPS = 30

// defaultSpiHz is the SPI clock requested when Config.SpeedHz is 0. On the Pi's
// auxiliary SPI1 the real clock is core_freq/divisor (core_freq is pinned to
// 400 MHz in config.txt), so the request is advisory and snaps to an even
// divisor — 40 MHz ≈ 400/10. Drop to 25 MHz (400/16) if the panel tears or
// shows garbage. Override per-call via Config.SpeedHz (the lcdperf probe does
// this to compare clocks).
const defaultSpiHz = 40 * physic.MegaHertz

// New opens the SPI port (Mode0, 8-bit), acquires the DC + RST GPIO lines,
// pulses a hardware reset, and runs the GC9A01 init sequence. The host
// package must be initialized (host.Init) before calling.
func New(cfg Config, log *slog.Logger) (display.Renderer, error) {
	if log == nil {
		log = slog.Default()
	}
	log = log.With("component", "device.gc9a01")

	dc, err := outPin(cfg.DCPin)
	if err != nil {
		return nil, fmt.Errorf("gc9a01 DC: %w", err)
	}
	rst, err := outPin(cfg.RSTPin)
	if err != nil {
		return nil, fmt.Errorf("gc9a01 RST: %w", err)
	}

	port, err := spireg.Open(cfg.Device)
	if err != nil {
		return nil, fmt.Errorf("gc9a01: open spi %s: %w", cfg.Device, err)
	}
	hz := defaultSpiHz
	if cfg.SpeedHz > 0 {
		hz = physic.Frequency(cfg.SpeedHz) * physic.Hertz
	}
	conn, err := port.Connect(hz, spi.Mode0, 8)
	if err != nil {
		_ = port.Close()
		return nil, fmt.Errorf("gc9a01: connect: %w", err)
	}

	d := &linuxDev{
		port:  port,
		conn:  conn,
		dc:    dc,
		rst:   rst,
		fb:    make([]byte, Width*Height*2),
		maxTx: Width * Height * 2,
		log:   log,
	}
	// The kernel spidev driver rejects a single Tx larger than its bufsiz
	// (commonly 4096); flush() chunks the framebuffer to fit this limit.
	if l, ok := conn.(interface{ MaxTxSize() int }); ok {
		if m := l.MaxTxSize(); m > 0 && m < d.maxTx {
			d.maxTx = m
		}
	}

	if err := d.reset(); err != nil {
		_ = port.Close()
		return nil, fmt.Errorf("gc9a01: reset: %w", err)
	}
	if err := d.initSeq(); err != nil {
		_ = port.Close()
		return nil, fmt.Errorf("gc9a01: init: %w", err)
	}
	return d, nil
}

// outPin resolves a BCM pin number (two-try, matching rotary's pin()) and
// configures it as an output driven high.
func outPin(num int) (gpio.PinIO, error) {
	p := gpioreg.ByName(strconv.Itoa(num))
	if p == nil {
		p = gpioreg.ByName("GPIO" + strconv.Itoa(num))
	}
	if p == nil {
		return nil, fmt.Errorf("gpio %d not found", num)
	}
	if err := p.Out(gpio.High); err != nil {
		return nil, err
	}
	return p, nil
}

type linuxDev struct {
	mu    sync.Mutex
	port  spi.PortCloser
	conn  spi.Conn
	dc    gpio.PinIO
	rst   gpio.PinIO
	fb    []byte // 240*240*2 RGB565, reused per flush (no per-frame alloc)
	maxTx int    // max bytes per Tx (spidev bufsiz)
	log   *slog.Logger

	// Animation engine. Lazily set up on the first Render so the raw Prober path
	// (hwprobe FillRGB/DrawTestPattern) never spins it up. All of these are touched
	// only by the state machine's single run goroutine (Render/Celebrate/Close),
	// never under mu — the engine owns SPI through FlushRect on its own goroutine.
	animOnce sync.Once
	engine   *anim.Engine
	theme    *ui.Theme
	scenes   map[sceneKind]anim.AnimatedScene
	curKind  sceneKind
}

// initCmd is one entry in the GC9A01 bring-up table: a command byte, its data
// bytes, and an optional post-command delay.
type initCmd struct {
	cmd   byte
	args  []byte
	delay time.Duration
}

// gc9a01Init is the canonical GC9A01 power-on sequence (the vendor register
// block used by the Waveshare/Adafruit reference drivers): inner-register
// unlock (0xFE/0xEF), the gamma/voltage/timing tuning block, COLMOD=RGB565,
// MADCTL orientation/order, display inversion on, sleep-out, display-on.
var gc9a01Init = []initCmd{
	{cmd: 0xEF},
	{cmd: 0xEB, args: []byte{0x14}},
	{cmd: 0xFE}, // inner register enable 1
	{cmd: 0xEF}, // inner register enable 2
	{cmd: 0xEB, args: []byte{0x14}},
	{cmd: 0x84, args: []byte{0x40}},
	{cmd: 0x85, args: []byte{0xFF}},
	{cmd: 0x86, args: []byte{0xFF}},
	{cmd: 0x87, args: []byte{0xFF}},
	{cmd: 0x88, args: []byte{0x0A}},
	{cmd: 0x89, args: []byte{0x21}},
	{cmd: 0x8A, args: []byte{0x00}},
	{cmd: 0x8B, args: []byte{0x80}},
	{cmd: 0x8C, args: []byte{0x01}},
	{cmd: 0x8D, args: []byte{0x01}},
	{cmd: 0x8E, args: []byte{0xFF}},
	{cmd: 0x8F, args: []byte{0xFF}},
	{cmd: 0xB6, args: []byte{0x00, 0x20}},
	{cmd: 0x36, args: []byte{0x08}}, // MADCTL: BGR order (set 0x00 if R/B swap)
	{cmd: 0x3A, args: []byte{0x05}}, // COLMOD: 16 bits/pixel (RGB565)
	{cmd: 0x90, args: []byte{0x08, 0x08, 0x08, 0x08}},
	{cmd: 0xBD, args: []byte{0x06}},
	{cmd: 0xBC, args: []byte{0x00}},
	{cmd: 0xFF, args: []byte{0x60, 0x01, 0x04}},
	{cmd: 0xC3, args: []byte{0x13}},
	{cmd: 0xC4, args: []byte{0x13}},
	{cmd: 0xC9, args: []byte{0x22}},
	{cmd: 0xBE, args: []byte{0x11}},
	{cmd: 0xE1, args: []byte{0x10, 0x0E}},
	{cmd: 0xDF, args: []byte{0x21, 0x0C, 0x02}},
	{cmd: 0xF0, args: []byte{0x45, 0x09, 0x08, 0x08, 0x26, 0x2A}}, // gamma
	{cmd: 0xF1, args: []byte{0x43, 0x70, 0x72, 0x36, 0x37, 0x6F}},
	{cmd: 0xF2, args: []byte{0x45, 0x09, 0x08, 0x08, 0x26, 0x2A}},
	{cmd: 0xF3, args: []byte{0x43, 0x70, 0x72, 0x36, 0x37, 0x6F}},
	{cmd: 0xED, args: []byte{0x1B, 0x0B}},
	{cmd: 0xAE, args: []byte{0x77}},
	{cmd: 0xCD, args: []byte{0x63}},
	{cmd: 0x70, args: []byte{0x07, 0x07, 0x04, 0x0E, 0x0F, 0x09, 0x07, 0x08, 0x03}},
	{cmd: 0xE8, args: []byte{0x34}}, // frame rate / dot inversion
	{cmd: 0x62, args: []byte{0x18, 0x0D, 0x71, 0xED, 0x70, 0x70, 0x18, 0x0F, 0x71, 0xEF, 0x70, 0x70}},
	{cmd: 0x63, args: []byte{0x18, 0x11, 0x71, 0xF1, 0x70, 0x70, 0x18, 0x13, 0x71, 0xF3, 0x70, 0x70}},
	{cmd: 0x64, args: []byte{0x28, 0x29, 0xF1, 0x01, 0xF1, 0x00, 0x07}},
	{cmd: 0x66, args: []byte{0x3C, 0x00, 0xCD, 0x67, 0x45, 0x45, 0x10, 0x00, 0x00, 0x00}},
	{cmd: 0x67, args: []byte{0x00, 0x3C, 0x00, 0x00, 0x00, 0x01, 0x54, 0x10, 0x32, 0x98}},
	{cmd: 0x74, args: []byte{0x10, 0x85, 0x80, 0x00, 0x00, 0x4E, 0x00}},
	{cmd: 0x98, args: []byte{0x3E, 0x07}},
	{cmd: 0x35},                                // TEON (tearing effect line; unwired, harmless)
	{cmd: 0x21},                                // INVON: round IPS panels need inversion
	{cmd: 0x11, delay: 120 * time.Millisecond}, // SLPOUT
	{cmd: 0x29, delay: 20 * time.Millisecond},  // DISPON
}

// reset pulses RST low to hardware-reset the controller, then waits for the
// panel to come up before the init sequence (datasheet: ~120 ms).
func (d *linuxDev) reset() error {
	if err := d.rst.Out(gpio.High); err != nil {
		return err
	}
	time.Sleep(10 * time.Millisecond)
	if err := d.rst.Out(gpio.Low); err != nil {
		return err
	}
	time.Sleep(10 * time.Millisecond)
	if err := d.rst.Out(gpio.High); err != nil {
		return err
	}
	time.Sleep(120 * time.Millisecond)
	return nil
}

func (d *linuxDev) initSeq() error {
	for _, c := range gc9a01Init {
		if err := d.cmd(c.cmd); err != nil {
			return err
		}
		if len(c.args) > 0 {
			if err := d.data(c.args...); err != nil {
				return err
			}
		}
		if c.delay > 0 {
			time.Sleep(c.delay)
		}
	}
	return nil
}

// cmd sends a command byte (DC low).
func (d *linuxDev) cmd(c byte) error {
	if err := d.dc.Out(gpio.Low); err != nil {
		return err
	}
	return d.conn.Tx([]byte{c}, nil)
}

// data sends data bytes (DC high).
func (d *linuxDev) data(b ...byte) error {
	if len(b) == 0 {
		return nil
	}
	if err := d.dc.Out(gpio.High); err != nil {
		return err
	}
	return d.conn.Tx(b, nil)
}

// setWindow programs the column/row address window and issues RAMWR, leaving
// the controller ready to receive pixel data.
func (d *linuxDev) setWindow(x0, y0, x1, y1 int) error {
	if err := d.cmd(0x2A); err != nil { // CASET
		return err
	}
	if err := d.data(byte(x0>>8), byte(x0), byte(x1>>8), byte(x1)); err != nil {
		return err
	}
	if err := d.cmd(0x2B); err != nil { // RASET
		return err
	}
	if err := d.data(byte(y0>>8), byte(y0), byte(y1>>8), byte(y1)); err != nil {
		return err
	}
	return d.cmd(0x2C) // RAMWR
}

// txChunked streams buf over SPI in maxTx-sized pieces. The kernel spidev
// driver rejects a single Tx larger than its bufsiz, and CS toggles between
// chunks, but the GC9A01 GRAM write pointer auto-increments across the toggle
// (and wraps at the active window's right edge to the next row), so pixels stay
// in order within the current address window. The caller must have programmed
// the window + RAMWR and driven DC high.
func (d *linuxDev) txChunked(buf []byte) error {
	for off := 0; off < len(buf); off += d.maxTx {
		end := off + d.maxTx
		if end > len(buf) {
			end = len(buf)
		}
		if err := d.conn.Tx(buf[off:end], nil); err != nil {
			return err
		}
	}
	return nil
}

// flush streams the whole framebuffer to GRAM after a full-screen window +
// RAMWR. Callers hold d.mu (flush does not lock).
func (d *linuxDev) flush() error {
	if err := d.setWindow(0, 0, Width-1, Height-1); err != nil {
		return err
	}
	if err := d.dc.Out(gpio.High); err != nil {
		return err
	}
	return d.txChunked(d.fb)
}

// flushRect streams the sub-rectangle [x0,x1) x [y0,y1) of buf (a full-frame
// Width*Height*2 RGB565 buffer, row stride = Width) to GRAM via a partial
// address window. When the rect spans the full width its rows are contiguous in
// buf and stream in one pass; otherwise each row streams separately and the
// controller's auto-wrap at the window edge keeps them in order. Coordinates are
// half-open and clamped to the panel; an empty rect is a no-op. Callers hold
// d.mu (flushRect does not lock).
func (d *linuxDev) flushRect(buf []byte, x0, y0, x1, y1 int) error {
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > Width {
		x1 = Width
	}
	if y1 > Height {
		y1 = Height
	}
	if x0 >= x1 || y0 >= y1 {
		return nil
	}
	if err := d.setWindow(x0, y0, x1-1, y1-1); err != nil {
		return err
	}
	if err := d.dc.Out(gpio.High); err != nil {
		return err
	}
	if x0 == 0 && x1 == Width {
		return d.txChunked(buf[y0*Width*2 : y1*Width*2])
	}
	for y := y0; y < y1; y++ {
		if err := d.txChunked(buf[(y*Width+x0)*2 : (y*Width+x1)*2]); err != nil {
			return err
		}
	}
	return nil
}

// FlushRect locks the panel and streams the sub-rectangle [x0,x1) x [y0,y1) of
// buf (a full-frame RGB565 framebuffer). FlushRect(buf, 0, 0, Width, Height) is
// a full-frame blit. It is the dirty-rectangle path the animation engine drives
// from its dedicated flush goroutine; nothing else may touch the panel
// concurrently while it runs.
func (d *linuxDev) FlushRect(buf []byte, x0, y0, x1, y1 int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.flushRect(buf, x0, y0, x1, y1)
}

// setPx writes one RGB565 pixel into the framebuffer (bounds-checked).
func (d *linuxDev) setPx(x, y int, hi, lo byte) {
	if x < 0 || x >= Width || y < 0 || y >= Height {
		return
	}
	i := (y*Width + x) * 2
	d.fb[i] = hi
	d.fb[i+1] = lo
}

// Render hands the scene to the animation engine: it swaps the active animated
// scene (a full-frame repaint) when the scene *kind* changes, otherwise updates
// the running scene's model in place. The handoff is channel sends only — no SPI,
// no framebuffer packing on the caller's run loop. If the engine could not be set
// up (font load failure), or the scene has no animated mapping, it falls back to
// the static colorFrame path so the panel always shows something.
func (d *linuxDev) Render(s display.Scene) error {
	d.animOnce.Do(d.initAnim)
	if d.engine == nil {
		return d.renderStatic(s)
	}
	kind, model := route(s)
	if kind == kindNone {
		return d.renderStatic(s)
	}
	if kind != d.curKind {
		d.engine.SetScene(d.scenes[kind])
		d.curKind = kind
	}
	d.engine.SetModel(model)
	return nil
}

// renderStatic composes the scene into a color canvas and streams it as a full
// frame — the original pre-animation path, kept as the fallback.
func (d *linuxDev) renderStatic(s display.Scene) error {
	c := colorFrame(s)
	d.mu.Lock()
	defer d.mu.Unlock()
	copy(d.fb, c.pix)
	return d.flush()
}

// initAnim builds the theme, the animated scenes, and the engine, then starts it.
// Run once via animOnce on the first Render. On any setup failure it logs and
// leaves engine nil, so Render uses the static fallback. Must not hold d.mu — the
// engine's Start spawns the flush goroutine, which locks d.mu via FlushRect.
func (d *linuxDev) initAnim() {
	th, err := ui.NewTheme()
	if err != nil {
		d.log.Warn("gc9a01 animation disabled; using static scenes", "err", err)
		return
	}
	d.theme = th
	d.scenes = newScenes(th)
	d.engine = anim.New(d, Width, Height, d.log, anim.WithFPS(animFPS))
	d.engine.Start()
	d.log.Info("gc9a01 animation engine started", "fps", animFPS)
}

// Celebrate forwards a one-shot reaction to the active scene via the engine (a
// non-blocking channel send). No-op until the engine is set up. Implements
// display.Celebrator.
func (d *linuxDev) Celebrate(ev display.CelebrationEvent) {
	if d.engine != nil {
		d.engine.Celebrate(ev)
	}
}

func (d *linuxDev) FillRGB(r, g, b uint8) error {
	hi, lo := rgb565(r, g, b)
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := 0; i < len(d.fb); i += 2 {
		d.fb[i] = hi
		d.fb[i+1] = lo
	}
	return d.flush()
}

func (d *linuxDev) DrawTestPattern() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	// Quadrants reveal orientation; distinct primaries reveal color order.
	for y := 0; y < Height; y++ {
		for x := 0; x < Width; x++ {
			var r, g, b uint8
			switch {
			case x < Width/2 && y < Height/2:
				r = 255 // top-left: red
			case x >= Width/2 && y < Height/2:
				g = 255 // top-right: green
			case x < Width/2 && y >= Height/2:
				b = 255 // bottom-left: blue
			default:
				r, g = 255, 255 // bottom-right: yellow
			}
			hi, lo := rgb565(r, g, b)
			d.setPx(x, y, hi, lo)
		}
	}
	// White crosshair through the center (2px wide for visibility).
	hi, lo := rgb565(255, 255, 255)
	for x := 0; x < Width; x++ {
		d.setPx(x, Height/2-1, hi, lo)
		d.setPx(x, Height/2, hi, lo)
	}
	for y := 0; y < Height; y++ {
		d.setPx(Width/2-1, y, hi, lo)
		d.setPx(Width/2, y, hi, lo)
	}
	return d.flush()
}

func (d *linuxDev) Close() error {
	// Stop the engine first so its flush goroutine releases the panel before we
	// take d.mu to blank it (Close is idempotent; no-op if never started).
	if d.engine != nil {
		_ = d.engine.Close()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.fb {
		d.fb[i] = 0
	}
	if err := d.flush(); err != nil { // blank the panel
		d.log.Warn("gc9a01 blank on close", "err", err)
	}
	return d.port.Close()
}
