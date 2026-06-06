package scenes

import (
	"strings"
	"time"

	"github.com/fogleman/gg"

	"github.com/scottyturner/pupcup/internal/device/anim"
	"github.com/scottyturner/pupcup/internal/device/display"
	"github.com/scottyturner/pupcup/internal/device/ui"
)

const (
	addInHeaderY = ui.CY - 84
	addInTopY    = ui.CY - 44
	addInRowH    = 30
	addInMaxRows = 4
	addInMaxLen  = 12
)

// AddIn is the add-in picker: a dog/score header over a scrollable list of ranked
// choices with the highlighted row called out. It is a discrete, rotary-driven
// screen, so it re-bakes and full-repaints on each selection change and otherwise
// holds still (no per-frame animation this milestone).
type AddIn struct {
	base
	m AddInModel
}

// NewAddIn builds an AddIn scene sharing th.
func NewAddIn(th *ui.Theme) *AddIn { return &AddIn{base: newBase(th)} }

func (s *AddIn) SetModel(m any) {
	am, ok := m.(AddInModel)
	if !ok {
		return
	}
	s.m = am
	s.bake = true
}

// Celebrate is a no-op: the picker plays no reactions.
func (s *AddIn) Celebrate(display.CelebrationEvent) {}

// Update repaints the whole panel on a change and is otherwise idle.
func (s *AddIn) Update(time.Duration) []anim.Rect {
	if s.bake {
		return fullPanel
	}
	return nil
}

func (s *AddIn) Draw(ctx *gg.Context, clip []anim.Rect) {
	if s.bake {
		s.bakeBG()
		s.bake = false
	}
	s.restore(ctx, clip)
}

func (s *AddIn) bakeBG() {
	s.th.SetTime(s.m.Now)
	s.clearBG()

	s.th.Small.DrawCentered(s.bgg, strings.ToUpper(s.m.DogName), ui.CX, addInHeaderY, s.th.C(ui.ScoreColor(s.m.Score)))

	if len(s.m.Choices) == 0 {
		s.th.Small.DrawCentered(s.bgg, "NO ADD-INS", ui.CX, ui.CY, s.th.C(ui.ColDim))
		return
	}

	// Scroll the window so the selected row stays visible.
	start := 0
	if s.m.Index >= addInMaxRows {
		start = s.m.Index - addInMaxRows + 1
	}
	for row := 0; row < addInMaxRows; row++ {
		i := start + row
		if i >= len(s.m.Choices) {
			break
		}
		y := addInTopY + row*addInRowH
		label := strings.ToUpper(s.m.Choices[i])
		if len(label) > addInMaxLen {
			label = label[:addInMaxLen]
		}
		if i == s.m.Index {
			mid := y + addInRowH/2
			half := ui.ChordHalfWidth(mid)
			s.bgg.SetColor(s.th.C(ui.ColHilite))
			s.bgg.DrawRoundedRectangle(float64(ui.CX-half+8), float64(y-2), float64(2*half-16), float64(addInRowH-4), 8)
			s.bgg.Fill()
			s.th.Small.DrawString(s.bgg, ">", ui.CX-half+16, mid+7, s.th.C(ui.ColFg))
		}
		col := ui.ColFg
		if i != s.m.Index {
			col = ui.ColDim
		}
		s.th.Small.DrawCentered(s.bgg, label, ui.CX, y+addInRowH/2+7, s.th.C(col))
	}
}
