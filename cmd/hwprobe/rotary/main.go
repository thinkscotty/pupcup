// Command hwprobe-rotary reads rotary encoder + button events to stdout.
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
	"github.com/scottyturner/pupcup/internal/device/hostinit"
	"github.com/scottyturner/pupcup/internal/device/rotary"
)

func main() {
	cfgPath := flag.String("config", "", "config.yaml path")
	timeout := flag.Duration("timeout", 60*time.Second, "how long to listen")
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

	d, err := rotary.New(rotary.Config{
		CLKPin:    cfg.RotaryPins.CLK,
		DTPin:     cfg.RotaryPins.DT,
		SWPin:     cfg.RotaryPins.SW,
		Debounce:  time.Duration(cfg.RotaryDebounceMS) * time.Millisecond,
		LongPress: cfg.LongPress(),
	}, log)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rotary:", err)
		os.Exit(1)
	}
	defer d.Close()

	fmt.Printf("listening for rotary for %s; ctrl-c to quit\n", *timeout)
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
			fmt.Printf("[%s] %s\n", ev.TS.Format(time.RFC3339Nano), ev.Kind)
		}
	}
}
