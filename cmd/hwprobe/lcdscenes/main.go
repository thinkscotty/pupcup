// Command hwprobe-lcdscenes walks the GC9A01 round LCD through the full animated
// UI on a timer — splash, the ambient HOME (idle + the three meal celebrations +
// the locked "all done" summary), snack mode, and the add-in picker — with NO
// buttons or rotary attached. It's the way to eyeball the Phase 4 scenes and read
// sustained CPU/fps on the real panel before the input hardware is wired.
//
// It drives a real anim.Engine through the driver's Prober.FlushRect (the same
// dirty-rect pipeline the daemon uses), so the motion, color, and cost are
// exactly what the app produces. The pupcup service owns SPI1, so stop it first:
//
//	sudo systemctl stop pupcup
//	/tmp/hwprobe-lcdscenes            # Ctrl-C to quit
//	sudo systemctl start pupcup
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
	"github.com/scottyturner/pupcup/internal/device/anim"
	"github.com/scottyturner/pupcup/internal/device/display"
	"github.com/scottyturner/pupcup/internal/device/gc9a01"
	"github.com/scottyturner/pupcup/internal/device/hostinit"
	"github.com/scottyturner/pupcup/internal/device/ui"
	"github.com/scottyturner/pupcup/internal/device/ui/scenes"
	"github.com/scottyturner/pupcup/internal/domain"
)

const (
	W = gc9a01.Width
	H = gc9a01.Height
)

// demoDog is a synthetic household member for the walkthrough.
type demoDog struct{ name, accent string }

var demoDogs = []demoDog{
	{"Cleo", "#7C5CFF"}, // violet
	{"Rio", "#FF8A3D"},  // orange
	{"Otis", "#2ED970"}, // green
}

func dogStat(d demoDog) scenes.DogStat {
	return scenes.DogStat{Dog: domain.Dog{Name: d.name, AccentColor: d.accent}}
}

// homeModel builds an idle HOME snapshot: the selected dog's avatar plus a pip +
// ring count derived from per-dog scores ("" = not yet fed).
func homeModel(sel int, scores []string) scenes.HomeModel {
	pips := make([]ui.Pip, len(demoDogs))
	fed := 0
	for i := range demoDogs {
		pips[i] = ui.Pip{Col: ui.ScoreColor(scores[i])}
		if scores[i] != "" {
			fed++
		}
	}
	return scenes.HomeModel{
		Mood:     scenes.MoodIdle,
		Sel:      dogStat(demoDogs[sel]),
		Pips:     pips,
		Selected: sel,
		Fed:      fed,
		Total:    len(demoDogs),
		Now:      time.Now(),
	}
}

// lockedModel builds the "all done" HOME snapshot: full glowing ring, outcome
// pips, and an unlock countdown.
func lockedModel(scores []string, countdown time.Duration) scenes.HomeModel {
	pips := make([]ui.Pip, len(demoDogs))
	for i := range demoDogs {
		pips[i] = ui.Pip{Col: ui.ScoreColor(scores[i])}
	}
	return scenes.HomeModel{
		Mood:      scenes.MoodAllDone,
		Pips:      pips,
		Selected:  -1,
		Fed:       len(demoDogs),
		Total:     len(demoDogs),
		Countdown: countdown,
		Now:       time.Now(),
	}
}

func meal(score domain.Score) display.CelebrationEvent {
	return display.CelebrationEvent{Kind: display.CelebrateMeal, Score: score}
}

// step sleeps for d, returning false if the context is canceled first (Ctrl-C).
func step(ctx context.Context, label string, d time.Duration) bool {
	fmt.Printf("→ %s\n", label)
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

// runDemo plays one pass of the full scene walkthrough. Returns false if canceled.
func runDemo(ctx context.Context, e *anim.Engine, home, snack, addin, splash anim.AnimatedScene) bool {
	e.SetScene(splash)
	e.SetModel(scenes.SplashModel{Message: "PupCup", Now: time.Now()})
	if !step(ctx, "splash", 2*time.Second) {
		return false
	}

	e.SetScene(home)
	e.SetModel(homeModel(0, []string{"", "", ""}))
	if !step(ctx, "HOME idle — nobody fed yet (breathing avatar + empty ring)", 2500*time.Millisecond) {
		return false
	}

	e.SetModel(homeModel(1, []string{"full", "", ""}))
	e.Celebrate(meal(domain.ScoreFull))
	if !step(ctx, "fed Cleo FULL — confetti + bounce, ring sweeps to 1/3", 3*time.Second) {
		return false
	}

	e.SetModel(homeModel(2, []string{"full", "partial", ""}))
	e.Celebrate(meal(domain.ScorePartial))
	if !step(ctx, "fed Rio PARTIAL — amber sparkle, ring 2/3", 3*time.Second) {
		return false
	}

	e.SetModel(homeModel(2, []string{"full", "partial", "none"}))
	e.Celebrate(meal(domain.ScoreNone))
	if !step(ctx, "fed Otis NONE — bounce + droop, ring 3/3", 3*time.Second) {
		return false
	}

	e.SetModel(lockedModel([]string{"full", "partial", "none"}, 3*time.Hour+42*time.Minute))
	if !step(ctx, "LOCKED all-done — full ring + bowl + UNLOCK 3:42H + outcome pips", 4*time.Second) {
		return false
	}

	e.SetScene(snack)
	e.SetModel(scenes.SnackModel{Dog: dogStat(demoDogs[2]), Remaining: 45 * time.Second, Now: time.Now()})
	if !step(ctx, "SNACK mode — Otis", time.Second) {
		return false
	}
	e.Celebrate(display.CelebrationEvent{Kind: display.CelebrateSnack})
	if !step(ctx, "snack recorded — blue sparkle", 2500*time.Millisecond) {
		return false
	}

	e.SetScene(addin)
	choices := []string{"Chicken", "Pumpkin", "Egg", "Other"}
	for i := range choices {
		e.SetModel(scenes.AddInModel{DogName: "Pip", Score: "full", Choices: choices, Index: i, Now: time.Now()})
		if !step(ctx, fmt.Sprintf("ADD-IN picker — highlight %q", choices[i]), 900*time.Millisecond) {
			return false
		}
	}
	return true
}

// statsLoop prints render/flush fps and dropped-frame deltas every interval, so
// you can read throughput without external tooling (pair with `top` for CPU).
func statsLoop(ctx context.Context, e *anim.Engine, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	last := e.Stats()
	secs := interval.Seconds()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s := e.Stats()
			fmt.Printf("  [stats] render %5.1f fps  flush %5.1f fps  dropped %d\n",
				float64(s.Rendered-last.Rendered)/secs, float64(s.Flushed-last.Flushed)/secs, s.Dropped-last.Dropped)
			last = s
		}
	}
}

func main() {
	cfgPath := flag.String("config", "", "config.yaml path")
	fps := flag.Int("fps", 30, "engine render rate (matches the daemon's animFPS)")
	spihz := flag.Int("spihz", 40_000_000, "SPI clock in Hz")
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
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

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

	// Prober exposes FlushRect — the same dirty-rect path the daemon's engine
	// uses. Its method set is a superset of display.RectFlusher, so it satisfies
	// the engine's flusher directly (no adapter).
	prober, ok := r.(gc9a01.Prober)
	if !ok {
		fmt.Fprintln(os.Stderr, "gc9a01: driver does not expose Prober")
		os.Exit(1)
	}

	th, err := ui.NewTheme()
	if err != nil {
		fmt.Fprintln(os.Stderr, "theme:", err)
		os.Exit(1)
	}

	engine := anim.New(prober, W, H, log, anim.WithFPS(*fps))
	engine.Start()
	defer engine.Close() // LIFO: stops the flusher before r.Close blanks the panel

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fmt.Printf("lcdscenes  %dx%d  spi=%s  fps=%d  spihz=%d   (Ctrl-C to quit)\n", W, H, cfg.LCDSPIDevice, *fps, *spihz)
	go statsLoop(ctx, engine, 2*time.Second)

	home := scenes.NewHome(th)
	snack := scenes.NewSnack(th)
	addin := scenes.NewAddIn(th)
	splash := scenes.NewSplash(th)

	for runDemo(ctx, engine, home, snack, addin, splash) {
	}
	fmt.Println("\nshutting down")
}
