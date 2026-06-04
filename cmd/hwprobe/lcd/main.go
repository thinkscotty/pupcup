// Command hwprobe-lcd cycles the GC9A01 round LCD through solid-color fills
// and a test pattern so you can verify wiring (SPI1 + DC/RST), the reset/init
// sequence, address windowing, and RGB565 color order before any scene code
// exists. If red shows as blue, flip the MADCTL BGR bit in the gc9a01 init
// sequence (see gc9a01.go rgb565 / gc9a01_linux.go gc9a01Init).
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/scottyturner/pupcup/internal/config"
	"github.com/scottyturner/pupcup/internal/device/gc9a01"
	"github.com/scottyturner/pupcup/internal/device/hostinit"
)

func main() {
	cfgPath := flag.String("config", "", "config.yaml path")
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
		Device: cfg.LCDSPIDevice,
		DCPin:  cfg.LCDDCPin,
		RSTPin: cfg.LCDRSTPin,
	}, log)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gc9a01:", err)
		os.Exit(1)
	}
	defer r.Close()

	// New returns a display.Renderer; the raw fill/test-pattern bring-up surface
	// is recovered via the Prober assertion (both the real driver and the Fake
	// implement it).
	prober, ok := r.(gc9a01.Prober)
	if !ok {
		fmt.Fprintln(os.Stderr, "gc9a01: driver does not expose Prober")
		os.Exit(1)
	}

	fills := []struct {
		name    string
		r, g, b uint8
	}{
		{"red", 255, 0, 0},
		{"green", 0, 255, 0},
		{"blue", 0, 0, 255},
		{"white", 255, 255, 255},
		{"black", 0, 0, 0},
	}
	for _, f := range fills {
		fmt.Printf("fill %s\n", f.name)
		if err := prober.FillRGB(f.r, f.g, f.b); err != nil {
			fmt.Fprintln(os.Stderr, "fill:", err)
			os.Exit(1)
		}
		time.Sleep(time.Second)
	}

	fmt.Println("test pattern (TL=red TR=green BL=blue BR=yellow + white crosshair)")
	if err := prober.DrawTestPattern(); err != nil {
		fmt.Fprintln(os.Stderr, "test pattern:", err)
		os.Exit(1)
	}
	time.Sleep(3 * time.Second)
}
