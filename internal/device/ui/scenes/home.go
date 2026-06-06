package scenes

import (
	"math"
	"time"

	"github.com/fogleman/gg"

	"github.com/scottyturner/pupcup/internal/device/anim"
	"github.com/scottyturner/pupcup/internal/device/display"
	"github.com/scottyturner/pupcup/internal/device/ui"
	"github.com/scottyturner/pupcup/internal/domain"
)

// Home geometry. The avatar sits in the center inside a feeding ring; rim pips
// run along the bottom arc. The locked variant swaps the avatar for a filled
// bowl and a countdown.
const (
	homeAvatarR      = 78
	homeRingR        = float64(ui.SafeRadius - 6) // 100
	homeRingTh       = 12.0
	homeRingOmega    = 12.0 // ring-fill spring stiffness (lively, no overshoot)
	homeDroopPx      = 12.0 // how far a poor-meal mood sinks the avatar
	homeBowlW        = 128.0
	homeBowlCY       = ui.CY - 4
	homeCountdownY   = ui.CY + 64
	homeAvatarMargin = 26 // dirty-box slack around the avatar for squash + droop + rim
	homeDroopHold    = 2.0
	// ringSettleEps: once the ring fill is within this many segments of its
	// target it is visually full, so we snap + stop re-baking (residual spring
	// velocity, which scales with the move amplitude, is imperceptible here).
	ringSettleEps = 0.01
	// Selection pop: the avatar springs up from homePopFrom to 1 when the
	// selected dog changes, so scrolling the rotary reads as a lively swap.
	homePopOmega = 18.0
	homePopFrom  = 0.82
	// homeConfettiDelay staggers the confetti a beat behind the bounce on a
	// full-meal celebration (bounce reacts, then the burst).
	homeConfettiDelay = 0.15
)

// homeAvatarBox is the fixed dirty rectangle the breathing/bouncing/drooping
// avatar is redrawn within each idle frame. It comfortably contains the avatar at
// its largest squash and lowest droop so the overlay never paints outside it.
var homeAvatarBox = anim.Rect{
	X0: ui.CX - (homeAvatarR + homeAvatarMargin),
	Y0: ui.CY - (homeAvatarR + homeAvatarMargin),
	X1: ui.CX + (homeAvatarR + homeAvatarMargin),
	Y1: ui.CY + (homeAvatarR + homeAvatarMargin),
}

// homeSparkBox bounds a sparkle burst (scattered around the ring).
var homeSparkBox = anim.Rect{X0: 6, Y0: 6, X1: ui.W - 6, Y1: ui.H - 6}

// Home is the ambient "are the dogs fed?" screen. MoodIdle shows the selected
// dog's avatar breathing inside a feeding ring with the household as rim pips;
// MoodAllDone shows the locked summary (full ring, filled bowl, countdown). A
// meal/snack Celebrate plays a bounce + confetti/sparkle in place.
type Home struct {
	base
	m HomeModel

	breath   *ui.IdleBreath
	bounce   *ui.Bounce
	droop    *ui.Droop
	confetti *ui.Confetti
	sparkle  *ui.Sparkle
	ring     anim.Spring // animated lit-segment count (sweeps when a dog is fed)
	pop      anim.Spring // selection pop: scales the avatar up on a dog change

	ringMoving   bool      // ring spring still settling → re-bake each frame
	droopT       float64   // seconds of poor-meal droop remaining
	confettiWait float64   // seconds until the staggered confetti fires (0 = none)
	confActive   bool      // confetti drew last frame (for the final clearing frame)
	sparkActive  bool      // sparkle drew last frame
	selKey       string    // identity of the selected dog, to detect a change
	lastSig      avatarSig // last flushed avatar signature (idle-breath throttle)
	haveSig      bool
}

// avatarSig is the quantized visual state of the avatar. While it is unchanged
// the breathing avatar needn't be re-flushed — the idle-breath throttle that
// keeps an at-rest HOME from streaming ~30 near-full frames a second forever.
type avatarSig struct {
	r, cy, squashBucket int
}

// NewHome builds a Home scene sharing th (the engine warms its glyph caches once).
func NewHome(th *ui.Theme) *Home {
	return &Home{
		base:     newBase(th),
		breath:   ui.NewIdleBreath(),
		bounce:   ui.NewBounce(),
		droop:    ui.NewDroop(),
		confetti: ui.NewConfetti(th),
		sparkle:  ui.NewSparkle(th),
		ring:     anim.NewSpring(0, homeRingOmega),
		pop:      anim.NewSpring(1, homePopOmega),
	}
}

// SetModel swaps in a fresh snapshot, marks the static layer for a re-bake, and
// retargets the ring-fill spring (so feeding a dog sweeps the ring up by a
// segment; the locked summary glows fully).
func (s *Home) SetModel(m any) {
	hm, ok := m.(HomeModel)
	if !ok {
		return
	}
	s.m = hm
	s.bake = true
	target := float64(hm.Fed)
	if hm.Mood == MoodAllDone {
		target = float64(hm.Total)
	}
	s.ring.SetTarget(target)

	// Pop the avatar when the selected dog changes (rotary scroll). Skipped in
	// the locked summary, which has no avatar.
	key := hm.Sel.Dog.Name + "\x00" + hm.Sel.Dog.AccentColor
	if hm.Mood == MoodIdle && key != s.selKey {
		s.pop.Pos, s.pop.Vel = homePopFrom, 0
	}
	s.selKey = key
}

// Celebrate plays the in-place reaction for a recorded meal or snack: a squash
// bounce always, plus confetti on a full meal, a sparkle on a snack/partial, and
// a brief droop on a refused meal.
func (s *Home) Celebrate(ev display.CelebrationEvent) {
	switch ev.Kind {
	case display.CelebrateSnack:
		s.bounce.Trigger(0.28)
		s.sparkle.Trigger(ui.CX, ui.CY, homeRingR-10, ui.ColSnack)
	case display.CelebrateMeal:
		switch ev.Score {
		case domain.ScoreFull:
			s.bounce.Trigger(0.45)
			s.confettiWait = homeConfettiDelay // staggered: bounce now, burst a beat later
		case domain.ScorePartial:
			s.bounce.Trigger(0.30)
			s.sparkle.Trigger(ui.CX, ui.CY, homeRingR-10, ui.ColPartial)
		case domain.ScoreNone:
			s.bounce.Trigger(0.14)
			s.droopT = homeDroopHold
			s.droop.Set(true)
		default:
			s.bounce.Trigger(0.25)
		}
	}
}

// Update advances the ring sweep, droop, breath, bounce and overlay bursts, and
// returns the rectangles that changed: the whole panel when the static layer or a
// panel-wide burst (confetti) is in play, otherwise just the avatar/sparkle boxes
// (or nil when the locked screen is at rest).
func (s *Home) Update(dt time.Duration) []anim.Rect {
	secs := dt.Seconds()

	// Ring sweep: re-bake while moving, snap + final bake on settle. Gated on
	// position (not velocity) so a large sweep doesn't linger above the threshold.
	s.ring.Step(secs)
	if math.Abs(s.ring.Pos-s.ring.Target) > ringSettleEps {
		s.bake = true
		s.ringMoving = true
	} else if s.ringMoving {
		s.ring.Pos = s.ring.Target
		s.bake = true
		s.ringMoving = false
	}

	// Droop hold then release.
	if s.droopT > 0 {
		s.droopT -= secs
		if s.droopT <= 0 {
			s.droopT = 0
			s.droop.Set(false)
		}
	}
	s.droop.Update(dt)

	breathing := s.m.Mood == MoodIdle
	if breathing {
		s.breath.Update(dt)
		s.pop.Step(secs)
	}
	s.bounce.Update(dt)

	// Staggered confetti: fire a beat after the celebration bounce.
	if s.confettiWait > 0 {
		s.confettiWait -= secs
		if s.confettiWait <= 0 {
			s.confettiWait = 0
			s.confetti.Trigger(ui.CX, ui.CY)
		}
	}

	// Overlays. Include an effect's region for one extra frame after it stops so
	// the restore erases its last particles.
	confDraw := s.confActive
	sparkDraw := s.sparkActive
	confNow := s.confetti.Update(dt)
	sparkNow := s.sparkle.Update(dt)
	s.confActive, s.sparkActive = confNow, sparkNow
	confDirty := confNow || confDraw
	sparkDirty := sparkNow || sparkDraw

	var sig avatarSig
	if breathing {
		sig = s.avatarSignature()
	}

	if s.bake || confDirty {
		if breathing { // the full repaint draws the avatar; sync the throttle
			s.lastSig, s.haveSig = sig, true
		}
		return fullPanel
	}

	s.dirty = s.dirty[:0]
	// Idle-breath throttle: only re-flush the avatar when it visibly moved
	// (breath/pop/bounce/droop changed its rounded size, center, or squash).
	if breathing && (!s.haveSig || sig != s.lastSig) {
		s.lastSig, s.haveSig = sig, true
		s.dirty = append(s.dirty, homeAvatarBox)
	}
	if sparkDirty {
		s.dirty = append(s.dirty, homeSparkBox)
	}
	if len(s.dirty) == 0 {
		return nil // nothing visibly moved: hold the last frame
	}
	return s.dirty
}

// Draw re-bakes the static layer if needed, restores it under the dirty clip, then
// composites the moving overlays.
func (s *Home) Draw(ctx *gg.Context, clip []anim.Rect) {
	if s.bake {
		s.bakeBG()
		s.bake = false
	}
	s.restore(ctx, clip)

	if s.m.Mood == MoodIdle {
		s.drawAvatar(ctx)
	}
	s.confetti.Draw(ctx)
	s.sparkle.Draw(ctx)
}

// drawAvatar paints the breathing/bouncing/drooping/popping centerpiece on top of
// the restored background. It re-applies the time wash each frame so the avatar
// tracks time-of-day even between static re-bakes.
func (s *Home) drawAvatar(ctx *gg.Context) {
	s.th.SetTime(s.m.Now)
	r, cy, squash := s.avatarParams()
	ui.DrawAvatar(ctx, s.th, s.m.Sel.Dog, s.m.Sel.Photo, ui.CX, cy, r, squash)
}

// avatarParams is the avatar's current draw geometry: radius (breath × selection
// pop), vertical center (droop offset), and squash (bounce). drawAvatar renders
// from it and avatarSignature quantizes it, so the throttle and the draw agree.
func (s *Home) avatarParams() (r, cy int, squash float64) {
	scale := s.breath.Scale() * s.pop.Pos
	r = int(float64(homeAvatarR) * scale)
	cy = ui.CY + int(s.droop.Amount()*homeDroopPx)
	squash = s.bounce.Squash()
	return
}

// avatarSignature quantizes avatarParams so sub-pixel breath jitter doesn't force
// a flush — only an integer size/center change or a perceptible squash step does.
func (s *Home) avatarSignature() avatarSig {
	r, cy, squash := s.avatarParams()
	return avatarSig{r: r, cy: cy, squashBucket: int(math.Round(squash / 0.02))}
}

// bakeBG re-renders the static layer for the current model and mood.
func (s *Home) bakeBG() {
	s.th.SetTime(s.m.Now)
	s.clearBG()

	switch s.m.Mood {
	case MoodAllDone:
		ui.DrawFeedingRing(s.bgg, s.th, ui.CX, ui.CY, homeRingR, homeRingTh, s.m.Total, s.ring.Pos, ui.ColFull)
		ui.DrawRimPips(s.bgg, s.th, s.m.Pips, -1)
		ui.DrawBowl(s.bgg, s.th, ui.CX, homeBowlCY, homeBowlW, 1.0, ui.ColFull)
		if s.m.Countdown > 0 {
			s.th.Small.DrawCentered(s.bgg, "UNLOCK "+ui.FormatDuration(s.m.Countdown), ui.CX, homeCountdownY, s.th.C(ui.ColDim))
		}
	default: // MoodIdle — the avatar is an overlay, so the static layer is ring + pips
		ui.DrawFeedingRing(s.bgg, s.th, ui.CX, ui.CY, homeRingR, homeRingTh, s.m.Total, s.ring.Pos, ui.ColFull)
		ui.DrawRimPips(s.bgg, s.th, s.m.Pips, s.m.Selected)
	}
}
