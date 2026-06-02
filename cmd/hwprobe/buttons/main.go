// Command hwprobe-buttons reads button events for 30 seconds and prints
// each press to stdout. Use during hardware bring-up.
//
//	GOOS=linux GOARCH=arm64 go build ./cmd/hwprobe/buttons
//	scp hwprobe-buttons pupcup@pupcup.local:/tmp/
//	ssh pupcup@pupcup.local sudo /tmp/hwprobe-buttons
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/scottyturner/pupcup/internal/config"
	"github.com/scottyturner/pupcup/internal/device/buttons"
	"github.com/scottyturner/pupcup/internal/device/hostinit"
	"github.com/scottyturner/pupcup/internal/domain"
)

func main() {
	cfgPath := flag.String("config", "", "config.yaml path")
	timeout := flag.Duration("timeout", 30*time.Second, "how long to listen")
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

	pins := buttons.Pins{
		domain.BtnGreen:  cfg.ButtonPins.Green,
		domain.BtnYellow: cfg.ButtonPins.Yellow,
		domain.BtnRed:    cfg.ButtonPins.Red,
		domain.BtnBlue:   cfg.ButtonPins.Blue,
	}
	d, err := buttons.New(pins, time.Duration(cfg.ButtonDebounceMS)*time.Millisecond, log)
	if err != nil {
		fmt.Fprintln(os.Stderr, "buttons:", err)
		os.Exit(1)
	}
	defer d.Close()

	fmt.Printf("listening for buttons for %s; ctrl-c to quit\n", *timeout)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	deadline := time.NewTimer(*timeout)
	defer deadline.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case ev, ok := <-d.Events():
			if !ok {
				return
			}
			fmt.Printf("[%s] %s %s\n", ev.TS.Format(time.RFC3339Nano), ev.Color, ev.Action)
		}
	}
}
