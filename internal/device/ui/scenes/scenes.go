// Package scenes implements PupCup's animated round-display screens as
// anim.AnimatedScene values, composed from the ui component kit (avatar, feeding
// ring, rim pips, bowl, effects) over the design-system theme. One scene per
// screen — Home (the ambient "are the dogs fed?" status, covering both the idle
// selector and the locked "all done" state), Snack, AddIn, and Splash — each
// driven by the anim.Engine on its render goroutine.
//
// Rendering model (the key to staying within the SPI1 perf budget): every scene
// keeps a pre-rendered *static layer* (bg) holding everything that does not move
// frame-to-frame — background wash, feeding ring, name, rim pips, countdown. Each
// frame the scene restores bg under the dirty rectangles, then composites only the
// moving overlays (a breathing avatar, a confetti/sparkle burst) on top. When the
// static content itself changes (a new model snapshot, or a ring sweep settling),
// the scene re-bakes bg and asks for a one-frame full repaint. Truly idle screens
// (the locked summary, the add-in picker) return no dirty rectangles, so the
// engine skips drawing and flushing entirely and the panel holds its last frame.
//
// Everything here is pure Go (no build tag): scenes compile and golden-test on
// macOS against display.Fake exactly as they run on the Pi.
package scenes

import (
	"image"
	"time"

	"github.com/fogleman/gg"

	"github.com/scottyturner/pupcup/internal/device/anim"
	"github.com/scottyturner/pupcup/internal/device/ui"
	"github.com/scottyturner/pupcup/internal/domain"
)

// Compile-time proof every scene satisfies the engine's contract.
var (
	_ anim.AnimatedScene = (*Home)(nil)
	_ anim.AnimatedScene = (*Snack)(nil)
	_ anim.AnimatedScene = (*AddIn)(nil)
	_ anim.AnimatedScene = (*Splash)(nil)
)

// fullPanel is the whole-panel dirty rectangle, shared so a full repaint costs no
// allocation. Compared by value to detect "repaint everything" in Draw.
var fullPanel = []anim.Rect{{X0: 0, Y0: 0, X1: ui.W, Y1: ui.H}}

// isFull reports whether clip is a single rectangle covering the entire panel —
// the signal (from the engine on a scene swap, or from a scene on a static
// change) to copy the whole background rather than per-rect slivers.
func isFull(clip []anim.Rect) bool {
	return len(clip) == 1 && clip[0] == fullPanel[0]
}

// HomeMood selects the Home scene's two faces: the live idle selector vs. the
// calm "everyone's fed" locked summary.
type HomeMood uint8

const (
	// MoodIdle is the live ambient status: the selected dog's avatar breathes
	// inside a feeding ring, other dogs are rim pips, a meal press celebrates.
	MoodIdle HomeMood = iota
	// MoodAllDone is the locked summary: a full glowing ring, a filled bowl, the
	// per-dog outcome pips, and a countdown to unlock.
	MoodAllDone
)

// DogStat is the selected dog's identity for the centerpiece avatar: the domain
// dog (name + accent hex, which ui.DrawAvatar parses) and, when non-nil, the
// pre-cropped circular photo (nil falls back to the accent disc + initial).
type DogStat struct {
	Dog   domain.Dog
	Photo *image.RGBA
}

// HomeModel is the immutable snapshot the Home scene renders. The router builds
// it from a DogSelectorScene (MoodIdle) or a LockedSummaryScene (MoodAllDone).
type HomeModel struct {
	Mood      HomeMood
	Sel       DogStat  // the centerpiece dog (MoodIdle)
	Pips      []ui.Pip // per-dog status dots, in selection order
	Selected  int      // index into Pips to highlight; -1 in MoodAllDone
	Fed       int      // dogs fed this session — the lit ring count
	Total     int      // dogs total — the ring's segment count
	Countdown time.Duration
	Now       time.Time
}

// SnackModel is the Snack scene snapshot: the selected dog and the idle-timeout
// countdown.
type SnackModel struct {
	Dog       DogStat
	Remaining time.Duration
	Now       time.Time
}

// AddInModel is the add-in picker snapshot: the pending dog + score header and the
// ranked choices with the highlighted index.
type AddInModel struct {
	DogName string
	Score   string
	Choices []string
	Index   int
	Now     time.Time
}

// SplashModel is the boot/info splash: one centered line.
type SplashModel struct {
	Message string
	Now     time.Time
}

// base is the shared static-layer machinery every scene embeds. It owns the
// pre-rendered background image and its own gg context for re-baking, tracks
// whether that layer is stale, and provides the dirty-rect restore.
type base struct {
	th    *ui.Theme
	bg    *image.RGBA // pre-rendered static layer, full panel
	bgg   *gg.Context // gg context bound to bg, used only when re-baking
	bake  bool        // static layer changed → re-bake + request a full repaint
	dirty []anim.Rect // reused scratch returned from Update (no per-frame alloc)
}

func newBase(th *ui.Theme) base {
	bg := image.NewRGBA(image.Rect(0, 0, ui.W, ui.H))
	return base{
		th:    th,
		bg:    bg,
		bgg:   gg.NewContextForRGBA(bg),
		bake:  true, // first frame must bake before anything is shown
		dirty: make([]anim.Rect, 0, 3),
	}
}

// restore copies the static layer into the engine's frame buffer under clip:
// the whole panel when clip is full, otherwise just the dirty rows. This is the
// per-frame "erase the moving overlays back to the static background" step, done
// as a straight memory copy (no gg, no allocation).
func (b *base) restore(ctx *gg.Context, clip []anim.Rect) {
	dst := ctx.Image().(*image.RGBA)
	if isFull(clip) {
		copy(dst.Pix, b.bg.Pix)
		return
	}
	for _, r := range clip {
		blitRect(dst, b.bg, r)
	}
}

// blitRect copies the half-open rectangle r from src into dst (both full-panel
// RGBA of equal width), row by row. r is clamped to the panel first.
func blitRect(dst, src *image.RGBA, r anim.Rect) {
	r = r.Clamp(ui.W, ui.H)
	if r.Empty() {
		return
	}
	n := (r.X1 - r.X0) * 4
	for y := r.Y0; y < r.Y1; y++ {
		di := dst.PixOffset(r.X0, y)
		si := src.PixOffset(r.X0, y)
		copy(dst.Pix[di:di+n], src.Pix[si:si+n])
	}
}

// clearBG wipes the static layer to the time-washed background color, ready for a
// fresh bake. Callers set the theme time first.
func (b *base) clearBG() {
	b.bgg.SetColor(b.th.C(ui.ColBg))
	b.bgg.Clear()
}
