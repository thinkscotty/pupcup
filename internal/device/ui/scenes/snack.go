package scenes

import (
	"time"

	"github.com/fogleman/gg"

	"github.com/scottyturner/pupcup/internal/device/anim"
	"github.com/scottyturner/pupcup/internal/device/display"
	"github.com/scottyturner/pupcup/internal/device/ui"
)

const (
	snackAvatarR      = 58
	snackLabelY       = ui.CY - 66 // "SNACK" baseline
	snackCountdownY   = ui.CY + 84
	snackAvatarMargin = 22
)

var snackAvatarBox = anim.Rect{
	X0: ui.CX - (snackAvatarR + snackAvatarMargin),
	Y0: ui.CY - (snackAvatarR + snackAvatarMargin),
	X1: ui.CX + (snackAvatarR + snackAvatarMargin),
	Y1: ui.CY + (snackAvatarR + snackAvatarMargin),
}

// Snack is the snack-mode screen: a "SNACK" banner, the selected dog's avatar,
// and the idle-timeout countdown. Recording a snack plays a bounce + blue
// sparkle in place.
type Snack struct {
	base
	m SnackModel

	breath  *ui.IdleBreath
	bounce  *ui.Bounce
	sparkle *ui.Sparkle

	sparkActive bool
}

// NewSnack builds a Snack scene sharing th.
func NewSnack(th *ui.Theme) *Snack {
	return &Snack{
		base:    newBase(th),
		breath:  ui.NewIdleBreath(),
		bounce:  ui.NewBounce(),
		sparkle: ui.NewSparkle(th),
	}
}

func (s *Snack) SetModel(m any) {
	sm, ok := m.(SnackModel)
	if !ok {
		return
	}
	s.m = sm
	s.bake = true
}

// Celebrate plays the snack reaction (other kinds never reach snack mode).
func (s *Snack) Celebrate(ev display.CelebrationEvent) {
	s.bounce.Trigger(0.30)
	s.sparkle.Trigger(ui.CX, ui.CY, float64(snackAvatarR)+18, ui.ColSnack)
}

func (s *Snack) Update(dt time.Duration) []anim.Rect {
	s.breath.Update(dt)
	s.bounce.Update(dt)

	sparkDraw := s.sparkActive
	sparkNow := s.sparkle.Update(dt)
	s.sparkActive = sparkNow

	if s.bake {
		return fullPanel
	}
	s.dirty = s.dirty[:0]
	s.dirty = append(s.dirty, snackAvatarBox)
	if sparkNow || sparkDraw {
		s.dirty = append(s.dirty, homeSparkBox)
	}
	return s.dirty
}

func (s *Snack) Draw(ctx *gg.Context, clip []anim.Rect) {
	if s.bake {
		s.bakeBG()
		s.bake = false
	}
	s.restore(ctx, clip)

	s.th.SetTime(s.m.Now)
	r := int(float64(snackAvatarR) * s.breath.Scale())
	ui.DrawAvatar(ctx, s.th, s.m.Dog.Dog, s.m.Dog.Photo, ui.CX, ui.CY, r, s.bounce.Squash())
	s.sparkle.Draw(ctx)
}

func (s *Snack) bakeBG() {
	s.th.SetTime(s.m.Now)
	s.clearBG()
	s.th.Big.DrawCentered(s.bgg, "SNACK", ui.CX, snackLabelY, s.th.C(ui.ColSnack))
	if s.m.Remaining > 0 {
		s.th.Small.DrawCentered(s.bgg, ui.FormatDuration(s.m.Remaining)+" LEFT", ui.CX, snackCountdownY, s.th.C(ui.ColDim))
	}
}
