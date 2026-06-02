// Command pupcup is the single-binary daemon: hardware drivers, web server,
// SQLite store, mDNS, all running as goroutines coordinated through an
// event bus. See pupcup_build_plan.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/scottyturner/pupcup/internal/clock"
	"github.com/scottyturner/pupcup/internal/config"
	"github.com/scottyturner/pupcup/internal/device/buttons"
	"github.com/scottyturner/pupcup/internal/device/hostinit"
	"github.com/scottyturner/pupcup/internal/device/neopixel"
	"github.com/scottyturner/pupcup/internal/device/oled"
	"github.com/scottyturner/pupcup/internal/device/rotary"
	"github.com/scottyturner/pupcup/internal/device/state"
	"github.com/scottyturner/pupcup/internal/domain"
	"github.com/scottyturner/pupcup/internal/eventbus"
	"github.com/scottyturner/pupcup/internal/mdns"
	"github.com/scottyturner/pupcup/internal/seed"
	"github.com/scottyturner/pupcup/internal/store"
	"github.com/scottyturner/pupcup/internal/systemd"
	"github.com/scottyturner/pupcup/internal/web"
)

// version is set at build time via -ldflags "-X main.version=<sha>".
var version = "dev"

// splashDuration is how long the boot splash shows before the state machine
// takes over the OLED.
const splashDuration = 1500 * time.Millisecond

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

	// Signals cancel ctx; the device loop and the watchdog both unwind from it.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// First-boot seed: give a fresh device its dogs without a manual web step.
	if err := seedDogs(ctx, st, log); err != nil {
		return fmt.Errorf("seed dogs: %w", err)
	}

	// Bring up the hardware. host.Init must run before opening any device. On
	// non-Linux the driver constructors return Fakes (build-tag split), so this
	// exact wiring also runs during laptop development.
	if err := hostinit.Init(); err != nil {
		return fmt.Errorf("host init: %w", err)
	}

	btn, err := buttons.New(buttons.Pins{
		domain.BtnGreen:  cfg.ButtonPins.Green,
		domain.BtnYellow: cfg.ButtonPins.Yellow,
		domain.BtnRed:    cfg.ButtonPins.Red,
		domain.BtnBlue:   cfg.ButtonPins.Blue,
	}, cfg.ButtonDebounce(), log)
	if err != nil {
		return fmt.Errorf("buttons: %w", err)
	}
	defer btn.Close()

	rot, err := rotary.New(rotary.Config{
		CLKPin:    cfg.RotaryPins.CLK,
		DTPin:     cfg.RotaryPins.DT,
		SWPin:     cfg.RotaryPins.SW,
		Debounce:  cfg.RotaryDebounce(),
		LongPress: cfg.LongPress(),
	}, log)
	if err != nil {
		return fmt.Errorf("rotary: %w", err)
	}
	defer rot.Close()

	ol, err := oled.New(oled.Config{I2CBus: uint8(cfg.I2CBus), Addr: cfg.OLEDAddr}, log)
	if err != nil {
		return fmt.Errorf("oled: %w", err)
	}
	defer ol.Close()

	leds, err := neopixel.New(neopixel.Config{Device: cfg.SPIDevice, N: cfg.NeopixelCount}, log)
	if err != nil {
		return fmt.Errorf("neopixel: %w", err)
	}
	defer leds.Close()

	// Boot splash (milestone 2): a brief "PupCup" before the state machine's
	// bootstrap renders the live scene. Interruptible so a signal during the
	// splash still shuts down promptly (drivers close via the defers above).
	_ = ol.Render(oled.SplashScene{Message: "PupCup", Now: clk.Now()})
	select {
	case <-time.After(splashDuration):
	case <-ctx.Done():
		return nil
	}

	machine, err := state.New(state.Deps{
		Cfg:     &cfg,
		Log:     log,
		Clk:     clk,
		Bus:     bus,
		Store:   st,
		Buttons: btn,
		Rotary:  rot,
		OLED:    ol,
		LEDs:    leds,
	})
	if err != nil {
		return fmt.Errorf("state machine: %w", err)
	}

	// Web app + mDNS advertising run alongside the device loop, all unwinding
	// from the same ctx. A web/mDNS failure is logged but does not take down the
	// device — feedings keep recording even if the network layer is unhappy.
	// Dog photos are uploaded and served from PhotoDir; ensure it exists. A
	// failure here is non-fatal — uploads will report a clear error and the rest
	// of the app keeps serving (web trouble never takes down the device loop).
	if err := os.MkdirAll(cfg.PhotoDir, 0o755); err != nil {
		log.Warn("photo dir", "path", cfg.PhotoDir, "err", err)
	}
	websrv, err := web.New(web.Deps{
		Store:      st,
		Log:        log,
		Clk:        clk,
		Version:    version,
		Loc:        cfg.Location,
		Host:       cfg.MDNSHostname + ".local",
		PhotoDir:   cfg.PhotoDir,
		PhotoMaxKB: cfg.PhotoMaxKB,
		PhotoMaxPx: cfg.PhotoMaxPx,
	})
	if err != nil {
		return fmt.Errorf("web: %w", err)
	}
	port, err := mdns.PortFromListen(cfg.Listen)
	if err != nil {
		return fmt.Errorf("mdns port: %w", err)
	}
	adv := mdns.New(cfg.MDNSHostname, port, version, log)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := websrv.Serve(ctx, cfg.Listen); err != nil {
			log.Error("web server", "err", err)
		}
	}()
	go func() {
		defer wg.Done()
		_ = adv.Run(ctx)
	}()

	// Watchdog heartbeat (no-op off systemd), then signal readiness now that the
	// store, drivers, state machine, web server, and mDNS are all up.
	go systemd.RunWatchdog(ctx)
	if err := systemd.Ready(); err != nil {
		log.Warn("sd_notify ready", "err", err)
	}
	log.Info("device ready", "listen", cfg.Listen)

	// Run the device loop until a signal cancels ctx, then let the web server
	// and mDNS advertiser unwind before returning.
	runErr := machine.Run(ctx)
	_ = systemd.Stopping()
	log.Info("pupcup shutting down")
	wg.Wait()
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		return runErr
	}
	return nil
}

// seedDogs inserts the embedded seed dogs the first time the daemon runs
// against an empty database, so a freshly-deployed device has its dogs without
// a manual web step. Idempotent: a no-op once any dog exists.
func seedDogs(ctx context.Context, st *store.Store, log *slog.Logger) error {
	existing, err := st.ListDogs(ctx)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	dogs, err := seed.Dogs()
	if err != nil {
		return err
	}
	for _, d := range dogs {
		if _, err := st.CreateDog(ctx, d); err != nil {
			return fmt.Errorf("create seed dog %q: %w", d.Name, err)
		}
	}
	log.Info("seeded dogs", "count", len(dogs))
	return nil
}
