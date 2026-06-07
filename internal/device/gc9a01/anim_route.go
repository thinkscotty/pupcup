package gc9a01

import (
	"image"

	"github.com/scottyturner/pupcup/internal/device/anim"
	"github.com/scottyturner/pupcup/internal/device/display"
	"github.com/scottyturner/pupcup/internal/device/ui"
	"github.com/scottyturner/pupcup/internal/device/ui/scenes"
	"github.com/scottyturner/pupcup/internal/domain"
)

// sceneKind identifies which AnimatedScene a display.Scene maps to. Several
// display scenes collapse onto one animated scene — the idle selector and the
// locked summary are both faces of the ambient HOME — so the engine only swaps
// (and full-repaints) when the *kind* changes, animating model updates in place
// otherwise.
type sceneKind uint8

const (
	kindNone sceneKind = iota
	kindHome
	kindSnack
	kindAddIn
	kindSplash
)

// route maps a state-machine scene snapshot to its animated-scene kind and the
// immutable model that scene renders. It is pure (no device, no clock) so it is
// unit-tested on macOS; the linux driver feeds the result to the engine. A nil or
// unknown scene routes to kindNone with a nil model.
func route(s display.Scene) (sceneKind, any) {
	switch sc := s.(type) {
	case display.DogSelectorScene:
		return kindHome, homeFromSelector(sc)
	case display.LockedSummaryScene:
		return kindHome, homeFromLocked(sc)
	case display.SnackModeScene:
		return kindSnack, scenes.SnackModel{
			Dog:       scenes.DogStat{Dog: sc.Dog},
			Remaining: sc.Remaining,
			Now:       sc.Now,
		}
	case display.AddInSelectScene:
		return kindAddIn, addInModel(sc)
	case display.SplashScene:
		return kindSplash, scenes.SplashModel{Message: sc.Message, Now: sc.Now}
	default:
		return kindNone, nil
	}
}

// photoProvider supplies a dog's decoded, circle-ready avatar photo (nil when the
// dog has no usable photo). *photocache.Cache satisfies it; tests use a fake. It
// lives here so the pure injection step stays decoupled from the (linux-only)
// cache that does the file I/O.
type photoProvider interface {
	Photo(dog domain.Dog) *image.RGBA
}

// withPhotos fills in the avatar photo on a routed scene model from pp, leaving
// everything else untouched. It runs after route() and before the engine sees the
// model, so route() stays pure and photo loading stays out of the model mapping.
// Only scenes that draw the selected dog's avatar get a photo: idle HOME and
// snack. A nil provider (photos disabled) or any other model passes through.
func withPhotos(model any, pp photoProvider) any {
	if pp == nil {
		return model
	}
	switch m := model.(type) {
	case scenes.HomeModel:
		if m.Mood == scenes.MoodIdle { // MoodAllDone shows a bowl, no avatar
			m.Sel.Photo = pp.Photo(m.Sel.Dog)
		}
		return m
	case scenes.SnackModel:
		m.Dog.Photo = pp.Photo(m.Dog.Dog)
		return m
	default:
		return model
	}
}

// homeFromSelector builds the idle HOME model: the selected dog's avatar, the
// household as status pips, and the lit-segment count = dogs fed this session.
func homeFromSelector(sc display.DogSelectorScene) scenes.HomeModel {
	m := scenes.HomeModel{
		Mood:     scenes.MoodIdle,
		Sel:      scenes.DogStat{Dog: sc.Dog},
		Selected: sc.Index,
		Total:    sc.Total,
		Now:      sc.Now,
	}
	if len(sc.Roster) > 0 {
		m.Total = len(sc.Roster)
		m.Pips = make([]ui.Pip, len(sc.Roster))
		for i, e := range sc.Roster {
			m.Pips[i] = ui.Pip{Col: ui.ScoreColor(string(e.Score))}
			if e.Score != "" {
				m.Fed++
			}
		}
	}
	return m
}

// homeFromLocked builds the locked HOME model: a full glowing ring, the per-dog
// outcome pips, and the unlock countdown.
func homeFromLocked(sc display.LockedSummaryScene) scenes.HomeModel {
	m := scenes.HomeModel{
		Mood:     scenes.MoodAllDone,
		Selected: -1,
		Total:    len(sc.Entries),
		Fed:      len(sc.Entries),
		Now:      sc.Now,
	}
	if !sc.LockedUntil.IsZero() && !sc.Now.IsZero() && sc.LockedUntil.After(sc.Now) {
		m.Countdown = sc.LockedUntil.Sub(sc.Now)
	}
	m.Pips = make([]ui.Pip, len(sc.Entries))
	for i, e := range sc.Entries {
		m.Pips[i] = ui.Pip{Col: ui.ScoreColor(string(e.Score))}
	}
	return m
}

// addInModel flattens the picker choices to labels for the animated scene.
func addInModel(sc display.AddInSelectScene) scenes.AddInModel {
	labels := make([]string, len(sc.Choices))
	for i, c := range sc.Choices {
		labels[i] = c.Label
	}
	return scenes.AddInModel{
		DogName: sc.Dog.Name,
		Score:   string(sc.Score),
		Choices: labels,
		Index:   sc.Index,
		Now:     sc.Now,
	}
}

// newScenes constructs one instance of every animated scene, all sharing the
// theme (whose glyph caches are warmed once). The engine drives whichever is
// active; the rest sit idle holding their own pre-rendered background.
func newScenes(th *ui.Theme) map[sceneKind]anim.AnimatedScene {
	return map[sceneKind]anim.AnimatedScene{
		kindHome:   scenes.NewHome(th),
		kindSnack:  scenes.NewSnack(th),
		kindAddIn:  scenes.NewAddIn(th),
		kindSplash: scenes.NewSplash(th),
	}
}
