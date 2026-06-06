package scenes

import (
	"strings"
	"time"

	"github.com/fogleman/gg"

	"github.com/scottyturner/pupcup/internal/device/anim"
	"github.com/scottyturner/pupcup/internal/device/display"
	"github.com/scottyturner/pupcup/internal/device/ui"
)

// splashMaxW is the widest a splash line may be before dropping to the small face.
const splashMaxW = 210

// Splash is the boot/info screen: one centered line, re-baked on change and
// otherwise idle. (A boot motion pass is deferred to the easing phase.)
type Splash struct {
	base
	m SplashModel
}

// NewSplash builds a Splash scene sharing th.
func NewSplash(th *ui.Theme) *Splash { return &Splash{base: newBase(th)} }

func (s *Splash) SetModel(m any) {
	sm, ok := m.(SplashModel)
	if !ok {
		return
	}
	s.m = sm
	s.bake = true
}

// Celebrate is a no-op on the splash.
func (s *Splash) Celebrate(display.CelebrationEvent) {}

func (s *Splash) Update(time.Duration) []anim.Rect {
	if s.bake {
		return fullPanel
	}
	return nil
}

func (s *Splash) Draw(ctx *gg.Context, clip []anim.Rect) {
	if s.bake {
		s.bakeBG()
		s.bake = false
	}
	s.restore(ctx, clip)
}

func (s *Splash) bakeBG() {
	s.th.SetTime(s.m.Now)
	s.clearBG()
	msg := strings.ToUpper(s.m.Message)
	face := s.th.Big
	if face.Measure(msg) > splashMaxW {
		face = s.th.Small
	}
	face.DrawCenteredBoth(s.bgg, msg, ui.CX, ui.CY, s.th.C(ui.ColFg))
}
