// Command pupcup is the single-binary daemon: hardware drivers, web server,
// SQLite store, mDNS, all running as goroutines coordinated through an
// event bus. See pupcup_build_plan.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/scottyturner/pupcup/internal/clock"
	"github.com/scottyturner/pupcup/internal/config"
	"github.com/scottyturner/pupcup/internal/eventbus"
	"github.com/scottyturner/pupcup/internal/store"
)

// version is set at build time via -ldflags "-X main.version=<sha>".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "pupcup:", err)
		os.Exit(1)
	}
}

func run() error {
	cfgPath := flag.String("config", "", "path to config.yaml (empty = defaults + env)")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)
	log = log.With("version", version)
	log.Info("pupcup starting", "config", *cfgPath)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer st.Close()

	bus := eventbus.New(64, log)
	defer bus.Close()

	clk := clock.Real{}

	// Prove the dependencies link & run; future milestones wire the real
	// hardware loop, web server, mDNS here.
	_ = clk
	_ = st
	_ = bus
	_ = cfg

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	<-ctx.Done()
	log.Info("pupcup shutting down")
	return nil
}
