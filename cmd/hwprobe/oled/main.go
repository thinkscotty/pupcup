// Command hwprobe-oled cycles the OLED through the four scene types so you
// can verify wiring and rendering on the device.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/scottyturner/pupcup/internal/config"
	"github.com/scottyturner/pupcup/internal/device/hostinit"
	"github.com/scottyturner/pupcup/internal/device/oled"
	"github.com/scottyturner/pupcup/internal/domain"
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
	r, err := oled.New(oled.Config{I2CBus: uint8(cfg.I2CBus), Addr: cfg.OLEDAddr}, log)
	if err != nil {
		fmt.Fprintln(os.Stderr, "oled:", err)
		os.Exit(1)
	}
	defer r.Close()

	now := time.Now()
	scenes := []oled.Scene{
		oled.SplashScene{Message: "PUPCUP", Now: now},
		oled.DogSelectorScene{Dog: domain.Dog{Name: "Cleo"}, Index: 0, Total: 3, Now: now},
		oled.LockedSummaryScene{
			Entries: []oled.SummaryEntry{
				{DogName: "Cleo", Score: domain.ScoreFull},
				{DogName: "Rio", Score: domain.ScorePartial, HasSnack: true},
				{DogName: "Pip", Score: domain.ScoreNone},
			},
			LockedUntil: now.Add(3*time.Hour + 42*time.Minute),
			Now:         now,
		},
		oled.SnackModeScene{Dog: domain.Dog{Name: "Otis"}, Remaining: 45 * time.Second},
	}
	for _, s := range scenes {
		fmt.Printf("rendering %T\n", s)
		if err := r.Render(s); err != nil {
			fmt.Fprintln(os.Stderr, "render:", err)
			os.Exit(1)
		}
		time.Sleep(2 * time.Second)
	}
}
