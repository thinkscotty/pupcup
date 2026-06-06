package state

import (
	"testing"

	"github.com/scottyturner/pupcup/internal/device/display"
	"github.com/scottyturner/pupcup/internal/device/rotary"
	"github.com/scottyturner/pupcup/internal/domain"
)

// TestCelebrate_MealCommitFires proves a committed meal fires exactly one
// CelebrateMeal carrying the dog + score, through the Celebrator type-assert (the
// Fake records it). Covers the run-loop → animator handoff without a real panel.
func TestCelebrate_MealCommitFires(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio") // 2 dogs → one meal does not complete the session
	h.fireBtn(t, domain.BtnGreen)     // Cleo full

	celebs := h.oled.Celebrations()
	if len(celebs) != 1 {
		t.Fatalf("expected 1 celebration after a meal, got %d (%+v)", len(celebs), celebs)
	}
	if celebs[0].Kind != display.CelebrateMeal || celebs[0].Score != domain.ScoreFull {
		t.Fatalf("celebration = %+v, want meal/full", celebs[0])
	}
	if celebs[0].DogID != h.m.dogs[0].ID {
		t.Fatalf("celebration dog = %d, want Cleo %d", celebs[0].DogID, h.m.dogs[0].ID)
	}
}

// TestCelebrate_SnackFires proves recording a snack fires one CelebrateSnack.
func TestCelebrate_SnackFires(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio")
	h.fireBtn(t, domain.BtnBlue) // enter snack mode (no record yet)
	h.fireBtn(t, domain.BtnBlue) // record a snack for Cleo

	celebs := h.oled.Celebrations()
	if len(celebs) != 1 {
		t.Fatalf("expected 1 snack celebration, got %d (%+v)", len(celebs), celebs)
	}
	if celebs[0].Kind != display.CelebrateSnack || celebs[0].DogID != h.m.dogs[0].ID {
		t.Fatalf("celebration = %+v, want snack for Cleo", celebs[0])
	}
}

// TestCelebrate_AddInCommitFires proves committing via the add-in picker also
// celebrates (it routes through commitPending like a plain meal).
func TestCelebrate_AddInCommitFires(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio")
	h.pressBtn(t, domain.BtnGreen)  // hold meal
	h.pressBtn(t, domain.BtnBlue)   // chord → picker
	h.fireRot(t, rotary.PressShort) // select first add-in → commitPending

	celebs := h.oled.Celebrations()
	if len(celebs) != 1 || celebs[0].Kind != display.CelebrateMeal {
		t.Fatalf("expected 1 meal celebration from add-in commit, got %+v", celebs)
	}
}
