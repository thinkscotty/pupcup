// Command hwprobe-lcdperf measures the GC9A01's real-world animation throughput
// on this board before any engine code is built. The panel is on the Pi's
// auxiliary SPI1 (userspace spidev, no DMA), so the headline "90fps round LCD"
// numbers do not apply — this probe finds the actual ceiling three ways:
//
//	1 full-frame      a 240x240 frame redrawn + flushed every frame
//	2 dirty-rect sync only the moving element's bounding box packed + flushed
//	3 dirty-rect async a 60Hz render goroutine feeding a flush goroutine
//	                   (cap-1 latest-wins, triple-buffered) — drops stale frames
//
// All three animate a real anti-aliased gg circle (not a raw fill) so the
// numbers reflect true drawing cost. Run it at -spihz=40000000 and again at
// 25000000, with the kernel spidev.bufsiz raised to 65536, and report the fps +
// ~CPU per mode. Those numbers are the Phase-1 gate for the animated UI.
package main

import (
	"flag"
	"fmt"
	"image"
	"log/slog"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fogleman/gg"

	"github.com/scottyturner/pupcup/internal/config"
	"github.com/scottyturner/pupcup/internal/device/gc9a01"
	"github.com/scottyturner/pupcup/internal/device/hostinit"
)

const (
	W = gc9a01.Width
	H = gc9a01.Height

	orbitR = 30.0 // radius of the circle's orbit around screen center
	circR  = 52.0 // radius of the moving circle (~110px dirty rect ≈ the central
	//              avatar, the real sustained-animation worst case)
	pad = 3.0 // bbox padding to cover anti-aliased edges
)

// pos returns the circle center at elapsed seconds (0.6 revolutions/sec).
func pos(elapsed float64) (float64, float64) {
	a := elapsed * 2 * math.Pi * 0.6
	return float64(W)/2 + orbitR*math.Cos(a), float64(H)/2 + orbitR*math.Sin(a)
}

// boxOf returns the clamped, half-open bounding box of the circle at (x,y).
func boxOf(x, y float64) (x0, y0, x1, y1 int) {
	x0 = int(math.Floor(x - circR - pad))
	y0 = int(math.Floor(y - circR - pad))
	x1 = int(math.Ceil(x + circR + pad))
	y1 = int(math.Ceil(y + circR + pad))
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > W {
		x1 = W
	}
	if y1 > H {
		y1 = H
	}
	return
}

func unionBox(ax0, ay0, ax1, ay1, bx0, by0, bx1, by1 int) (int, int, int, int) {
	return min(ax0, bx0), min(ay0, by0), max(ax1, bx1), max(ay1, by1)
}

func clearBg(dc *gg.Context) {
	dc.SetRGB(0.04, 0.04, 0.09)
	dc.Clear()
}

func fillRectBg(dc *gg.Context, x0, y0, x1, y1 int) {
	dc.SetRGB(0.04, 0.04, 0.09)
	dc.DrawRectangle(float64(x0), float64(y0), float64(x1-x0), float64(y1-y0))
	dc.Fill()
}

func drawCircle(dc *gg.Context, cx, cy float64) {
	dc.SetRGB(0.13, 0.85, 0.80) // teal body
	dc.DrawCircle(cx, cy, circR)
	dc.Fill()
	dc.SetRGB(1, 1, 1) // small highlight, second AA fill
	dc.DrawCircle(cx-circR*0.3, cy-circR*0.3, circR*0.22)
	dc.Fill()
}

// packRect converts the sub-rectangle [x0,x1) x [y0,y1) of img into the
// full-frame RGB565 buffer dst (row stride = W) at matching offsets, using the
// same bit layout as the driver's rgb565.
func packRect(dst []byte, img *image.RGBA, x0, y0, x1, y1 int) {
	for y := y0; y < y1; y++ {
		si := img.PixOffset(x0, y)
		di := (y*W + x0) * 2
		for x := x0; x < x1; x++ {
			r, g, b := img.Pix[si], img.Pix[si+1], img.Pix[si+2]
			v := (uint16(r&0xF8) << 8) | (uint16(g&0xFC) << 3) | (uint16(b) >> 3)
			dst[di] = byte(v >> 8)
			dst[di+1] = byte(v)
			si += 4
			di += 2
		}
	}
}

// cpuTicks returns this process's utime+stime in clock ticks (Linux only; false
// elsewhere). Field parsing skips the comm field (which may contain spaces) by
// splitting after its closing paren.
func cpuTicks() (uint64, bool) {
	b, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, false
	}
	s := string(b)
	i := strings.LastIndex(s, ")")
	if i < 0 {
		return 0, false
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 13 { // need up to stime (index 12 after the comm field)
		return 0, false
	}
	utime, err1 := strconv.ParseUint(f[11], 10, 64)
	stime, err2 := strconv.ParseUint(f[12], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return utime + stime, true
}

func report(name string, frames int, wall time.Duration, c0, c1 uint64, cpuOK bool) {
	line := fmt.Sprintf("  %-18s %6.1f fps   (%d frames / %.1fs)",
		name, float64(frames)/wall.Seconds(), frames, wall.Seconds())
	if cpuOK {
		line += fmt.Sprintf("   ~%3.0f%% of 1 core", float64(c1-c0)/100.0/wall.Seconds()*100.0)
	}
	fmt.Println(line)
}

func runFull(prober gc9a01.Prober, dur time.Duration) {
	rgba := image.NewRGBA(image.Rect(0, 0, W, H))
	dc := gg.NewContextForRGBA(rgba)
	buf := make([]byte, W*H*2)
	var frames int
	start := time.Now()
	c0, cpuOK := cpuTicks()
	for time.Since(start) < dur {
		cx, cy := pos(time.Since(start).Seconds())
		clearBg(dc)
		drawCircle(dc, cx, cy)
		packRect(buf, rgba, 0, 0, W, H)
		if err := prober.FlushRect(buf, 0, 0, W, H); err != nil {
			fmt.Fprintln(os.Stderr, "full flush:", err)
			os.Exit(1)
		}
		frames++
	}
	c1, _ := cpuTicks()
	report("1 full-frame", frames, time.Since(start), c0, c1, cpuOK)
}

func runDirtySync(prober gc9a01.Prober, dur time.Duration) {
	rgba := image.NewRGBA(image.Rect(0, 0, W, H))
	dc := gg.NewContextForRGBA(rgba)
	buf := make([]byte, W*H*2)
	clearBg(dc)
	packRect(buf, rgba, 0, 0, W, H)
	_ = prober.FlushRect(buf, 0, 0, W, H) // prime the panel with the background
	var frames int
	prevX, prevY := pos(0)
	start := time.Now()
	c0, cpuOK := cpuTicks()
	for time.Since(start) < dur {
		cx, cy := pos(time.Since(start).Seconds())
		px0, py0, px1, py1 := boxOf(prevX, prevY)
		cx0, cy0, cx1, cy1 := boxOf(cx, cy)
		ux0, uy0, ux1, uy1 := unionBox(px0, py0, px1, py1, cx0, cy0, cx1, cy1)
		fillRectBg(dc, ux0, uy0, ux1, uy1)
		drawCircle(dc, cx, cy)
		packRect(buf, rgba, ux0, uy0, ux1, uy1)
		if err := prober.FlushRect(buf, ux0, uy0, ux1, uy1); err != nil {
			fmt.Fprintln(os.Stderr, "dirty flush:", err)
			os.Exit(1)
		}
		frames++
		prevX, prevY = cx, cy
	}
	c1, _ := cpuTicks()
	report("2 dirty-rect sync", frames, time.Since(start), c0, c1, cpuOK)
}

func runAsync(prober gc9a01.Prober, dur time.Duration) {
	rgba := image.NewRGBA(image.Rect(0, 0, W, H))
	dc := gg.NewContextForRGBA(rgba)
	clearBg(dc)
	prime := make([]byte, W*H*2)
	packRect(prime, rgba, 0, 0, W, H)
	_ = prober.FlushRect(prime, 0, 0, W, H)

	const nbuf = 3 // triple-buffered: at most 1 queued + 1 in-flight, so >=1 free
	free := make(chan []byte, nbuf)
	for i := 0; i < nbuf; i++ {
		free <- make([]byte, W*H*2)
	}
	type job struct {
		buf            []byte
		x0, y0, x1, y1 int
	}
	ready := make(chan job, 1)
	var wg sync.WaitGroup
	var flushed int
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := range ready { // sole SPI owner
			if err := prober.FlushRect(j.buf, j.x0, j.y0, j.x1, j.y1); err != nil {
				fmt.Fprintln(os.Stderr, "async flush:", err)
			}
			flushed++
			free <- j.buf
		}
	}()

	var rendered, dropped int
	prevX, prevY := pos(0)
	tick := time.NewTicker(time.Second / 60)
	start := time.Now()
	c0, cpuOK := cpuTicks()
	for time.Since(start) < dur {
		<-tick.C
		cx, cy := pos(time.Since(start).Seconds())
		px0, py0, px1, py1 := boxOf(prevX, prevY)
		bx0, by0, bx1, by1 := boxOf(cx, cy)
		ux0, uy0, ux1, uy1 := unionBox(px0, py0, px1, py1, bx0, by0, bx1, by1)
		fillRectBg(dc, ux0, uy0, ux1, uy1)
		drawCircle(dc, cx, cy)
		select {
		case buf := <-free:
			packRect(buf, rgba, ux0, uy0, ux1, uy1)
			select { // latest-wins: reclaim any stale queued frame
			case old := <-ready:
				free <- old.buf
				dropped++
			default:
			}
			ready <- job{buf, ux0, uy0, ux1, uy1}
			rendered++
		default:
			dropped++ // flusher busy and all buffers in flight
		}
		prevX, prevY = cx, cy
	}
	tick.Stop()
	c1, _ := cpuTicks()
	close(ready)
	wg.Wait()
	wall := time.Since(start)
	line := fmt.Sprintf("  %-18s render %5.1f fps  flush %5.1f fps  dropped %d",
		"3 dirty-rect async", float64(rendered)/wall.Seconds(), float64(flushed)/wall.Seconds(), dropped)
	if cpuOK {
		line += fmt.Sprintf("   ~%3.0f%% of 1 core", float64(c1-c0)/100.0/wall.Seconds()*100.0)
	}
	fmt.Println(line)
}

func main() {
	cfgPath := flag.String("config", "", "config.yaml path")
	secs := flag.Int("secs", 5, "seconds per benchmark loop")
	spihz := flag.Int("spihz", 40_000_000, "SPI clock in Hz (run at 40000000, then 25000000)")
	flag.Parse()

	if err := hostinit.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "host init:", err)
		os.Exit(1)
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	r, err := gc9a01.New(gc9a01.Config{
		Device:  cfg.LCDSPIDevice,
		DCPin:   cfg.LCDDCPin,
		RSTPin:  cfg.LCDRSTPin,
		SpeedHz: *spihz,
	}, log)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gc9a01:", err)
		os.Exit(1)
	}
	defer r.Close()

	prober, ok := r.(gc9a01.Prober)
	if !ok {
		fmt.Fprintln(os.Stderr, "gc9a01: driver does not expose Prober")
		os.Exit(1)
	}

	dur := time.Duration(*secs) * time.Second
	fmt.Printf("lcdperf  %dx%d RGB565  spi=%s  requested=%d Hz  %s/loop\n", W, H, cfg.LCDSPIDevice, *spihz, dur)
	fmt.Println("verify kernel bufsiz:  cat /sys/module/spidev/parameters/bufsiz   (want 65536)")
	fmt.Println("CPU%% assumes CLK_TCK=100 (standard on Raspberry Pi OS)")
	fmt.Println()
	runFull(prober, dur)
	runDirtySync(prober, dur)
	runAsync(prober, dur)
	fmt.Println()
	fmt.Println("re-run with -spihz=25000000 to compare clocks")
}
