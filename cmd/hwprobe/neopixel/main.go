// Command hwprobe-neopixel exercises the LED bar: solid colors, walking
// pixel, and a smooth fade. Verifies SPI 3-bit encoding and level shifter.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/scottyturner/pupcup/internal/config"
	"github.com/scottyturner/pupcup/internal/device/hostinit"
	"github.com/scottyturner/pupcup/internal/device/neopixel"
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
	s, err := neopixel.New(neopixel.Config{Device: cfg.SPIDevice, N: cfg.NeopixelCount}, log)
	if err != nil {
		fmt.Fprintln(os.Stderr, "neopixel:", err)
		os.Exit(1)
	}
	defer s.Close()

	// Solid colors.
	for _, c := range []neopixel.Color{
		{R: 32}, {G: 32}, {B: 32}, {R: 32, G: 32, B: 32},
	} {
		fmt.Printf("solid %+v\n", c)
		_ = s.SetAll(c)
		_ = s.Show()
		time.Sleep(time.Second)
	}

	// Walking pixel.
	fmt.Println("walking pixel")
	for round := 0; round < 3; round++ {
		for i := 0; i < s.N(); i++ {
			_ = s.SetAll(neopixel.Color{})
			_ = s.SetPixel(i, neopixel.Color{R: 32, G: 16})
			_ = s.Show()
			time.Sleep(80 * time.Millisecond)
		}
	}

	// Fade-in/fade-out green.
	fmt.Println("fade")
	for v := 0; v <= 64; v += 2 {
		_ = s.SetAll(neopixel.Color{G: uint8(v)})
		_ = s.Show()
		time.Sleep(15 * time.Millisecond)
	}
	for v := 64; v >= 0; v -= 2 {
		_ = s.SetAll(neopixel.Color{G: uint8(v)})
		_ = s.Show()
		time.Sleep(15 * time.Millisecond)
	}
	_ = s.SetAll(neopixel.Color{})
	_ = s.Show()
}
