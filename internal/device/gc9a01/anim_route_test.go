package gc9a01

import (
	"image"
	"testing"
	"time"

	"github.com/scottyturner/pupcup/internal/device/display"
	"github.com/scottyturner/pupcup/internal/device/ui/scenes"
	"github.com/scottyturner/pupcup/internal/domain"
)

func TestRouteKinds(t *testing.T) {
	now := time.Now()
	cases := []struct {
		s    display.Scene
		want sceneKind
	}{
		{display.DogSelectorScene{Dog: domain.Dog{Name: "Cleo"}, Now: now}, kindHome},
		{display.LockedSummaryScene{Now: now}, kindHome},
		{display.SnackModeScene{Dog: domain.Dog{Name: "Rio"}, Now: now}, kindSnack},
		{display.AddInSelectScene{Dog: domain.Dog{Name: "Pip"}, Now: now}, kindAddIn},
		{display.SplashScene{Message: "HI", Now: now}, kindSplash},
		{nil, kindNone},
	}
	for _, c := range cases {
		k, m := route(c.s)
		if k != c.want {
			t.Errorf("route(%T) kind = %d, want %d", c.s, k, c.want)
		}
		if c.want != kindNone && m == nil {
			t.Errorf("route(%T) model = nil", c.s)
		}
		if c.want == kindNone && m != nil {
			t.Errorf("route(nil) model = %v, want nil", m)
		}
	}
}

// TestRouteHomeFromSelector checks the idle HOME model: ring count = dogs fed,
// segments = roster size, the selected index, and the avatar dog.
func TestRouteHomeFromSelector(t *testing.T) {
	now := time.Now()
	sc := display.DogSelectorScene{
		Dog: domain.Dog{Name: "Cleo", AccentColor: "#7C5CFF"}, Index: 1, Total: 3, Now: now,
		Roster: []display.SummaryEntry{
			{DogName: "Cleo", Score: domain.ScoreFull},
			{DogName: "Rio"}, // not yet fed
			{DogName: "Pip", Score: domain.ScorePartial},
		},
	}
	k, m := route(sc)
	if k != kindHome {
		t.Fatalf("kind = %d, want kindHome", k)
	}
	hm := m.(scenes.HomeModel)
	if hm.Mood != scenes.MoodIdle {
		t.Errorf("mood = %d, want idle", hm.Mood)
	}
	if hm.Total != 3 || hm.Fed != 2 {
		t.Errorf("ring = %d/%d, want 2/3", hm.Fed, hm.Total)
	}
	if hm.Selected != 1 {
		t.Errorf("selected = %d, want 1", hm.Selected)
	}
	if len(hm.Pips) != 3 {
		t.Errorf("pips = %d, want 3", len(hm.Pips))
	}
	if hm.Sel.Dog.Name != "Cleo" {
		t.Errorf("avatar dog = %q, want Cleo", hm.Sel.Dog.Name)
	}
}

// TestRouteHomeFromSelectorNoRoster covers the legacy/no-roster fallback: still
// HOME, segments from Total, nothing lit, no pips.
func TestRouteHomeFromSelectorNoRoster(t *testing.T) {
	sc := display.DogSelectorScene{Dog: domain.Dog{Name: "Solo"}, Index: 0, Total: 2}
	_, m := route(sc)
	hm := m.(scenes.HomeModel)
	if hm.Total != 2 || hm.Fed != 0 || len(hm.Pips) != 0 {
		t.Fatalf("no-roster fallback = %d/%d pips=%d, want 0/2 pips=0", hm.Fed, hm.Total, len(hm.Pips))
	}
}

// TestRouteHomeFromLocked checks the locked HOME model: AllDone mood, no
// selection, per-dog pips, and an unlock countdown.
func TestRouteHomeFromLocked(t *testing.T) {
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	sc := display.LockedSummaryScene{
		Entries:     []display.SummaryEntry{{DogName: "Cleo", Score: domain.ScoreFull}, {DogName: "Rio", Score: domain.ScoreNone}},
		LockedUntil: now.Add(90 * time.Minute),
		Now:         now,
	}
	_, m := route(sc)
	hm := m.(scenes.HomeModel)
	if hm.Mood != scenes.MoodAllDone {
		t.Errorf("mood = %d, want allDone", hm.Mood)
	}
	if hm.Selected != -1 {
		t.Errorf("selected = %d, want -1 in locked", hm.Selected)
	}
	if hm.Countdown != 90*time.Minute {
		t.Errorf("countdown = %v, want 90m", hm.Countdown)
	}
	if len(hm.Pips) != 2 {
		t.Errorf("pips = %d, want 2", len(hm.Pips))
	}
}

// TestRouteAddIn flattens choices to labels.
func TestRouteAddIn(t *testing.T) {
	sc := display.AddInSelectScene{
		Dog:     domain.Dog{Name: "Pip"},
		Score:   domain.ScoreFull,
		Choices: []display.AddInChoice{{Label: "Chicken"}, {Label: "Other", IsOther: true}},
		Index:   1,
	}
	_, m := route(sc)
	am := m.(scenes.AddInModel)
	if am.DogName != "Pip" || am.Score != "full" || am.Index != 1 {
		t.Errorf("addin header = %q/%q/%d", am.DogName, am.Score, am.Index)
	}
	if len(am.Choices) != 2 || am.Choices[0] != "Chicken" || am.Choices[1] != "Other" {
		t.Errorf("choices = %v", am.Choices)
	}
}

// fakePhotos is a photoProvider that hands back a fixed image for any dog.
type fakePhotos struct{ img *image.RGBA }

func (f fakePhotos) Photo(domain.Dog) *image.RGBA { return f.img }

// TestWithPhotos checks the photo is injected only where the selected dog's
// avatar is drawn (idle HOME, snack), skipped for the locked summary (a bowl, no
// avatar), and that a nil provider is a no-op.
func TestWithPhotos(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	pp := fakePhotos{img: img}

	idle := scenes.HomeModel{Mood: scenes.MoodIdle, Sel: scenes.DogStat{Dog: domain.Dog{Name: "Rex"}}}
	if got := withPhotos(idle, pp).(scenes.HomeModel); got.Sel.Photo != img {
		t.Error("idle HOME should receive the photo")
	}

	locked := scenes.HomeModel{Mood: scenes.MoodAllDone, Sel: scenes.DogStat{Dog: domain.Dog{Name: "Rex"}}}
	if got := withPhotos(locked, pp).(scenes.HomeModel); got.Sel.Photo != nil {
		t.Error("locked summary has no avatar; photo should stay nil")
	}

	snack := scenes.SnackModel{Dog: scenes.DogStat{Dog: domain.Dog{Name: "Rex"}}}
	if got := withPhotos(snack, pp).(scenes.SnackModel); got.Dog.Photo != img {
		t.Error("snack should receive the photo")
	}

	if got := withPhotos(idle, nil).(scenes.HomeModel); got.Sel.Photo != nil {
		t.Error("nil provider should be a no-op")
	}
}
