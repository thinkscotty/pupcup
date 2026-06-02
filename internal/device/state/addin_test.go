package state

import (
	"testing"
	"time"

	"github.com/scottyturner/pupcup/internal/device/oled"
	"github.com/scottyturner/pupcup/internal/device/rotary"
	"github.com/scottyturner/pupcup/internal/domain"
	"github.com/scottyturner/pupcup/internal/store"
)

// feedingsFor is a small helper to read all (non-deleted) feedings.
func (h *harness) allFeedings(t *testing.T) []domain.Feeding {
	t.Helper()
	got, err := h.store.ListFeedings(h.ctx, store.FeedingFilter{})
	if err != nil {
		t.Fatalf("list feedings: %v", err)
	}
	return got
}

func TestDeferredCommit_ReleaseCommitsPlainMeal(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio", "Pip")
	h.pressBtn(t, domain.BtnGreen)
	// Nothing written on press — the commit is deferred to release.
	if n := len(h.allFeedings(t)); n != 0 {
		t.Fatalf("press should not write a feeding yet, got %d", n)
	}
	if h.m.SelectedDog().Name != "Cleo" {
		t.Fatalf("selection should not advance until release, got %s", h.m.SelectedDog().Name)
	}
	h.releaseBtn(t, domain.BtnGreen)
	feeds := h.allFeedings(t)
	if len(feeds) != 1 || feeds[0].Score != domain.ScoreFull || len(feeds[0].Tags) != 0 {
		t.Fatalf("release should commit one plain full meal, got %+v", feeds)
	}
	if h.m.SelectedDog().Name != "Rio" {
		t.Fatalf("should advance to Rio after commit, got %s", h.m.SelectedDog().Name)
	}
}

func TestLastWins_OverlappingMealButtons(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio", "Pip")
	h.pressBtn(t, domain.BtnGreen)  // pending = full
	h.pressBtn(t, domain.BtnYellow) // last-wins → partial
	h.releaseBtn(t, domain.BtnGreen)
	if n := len(h.allFeedings(t)); n != 0 {
		t.Fatalf("should not commit while a meal button is still held, got %d", n)
	}
	h.releaseBtn(t, domain.BtnYellow)
	feeds := h.allFeedings(t)
	if len(feeds) != 1 || feeds[0].Score != domain.ScorePartial {
		t.Fatalf("last-wins should commit a partial meal, got %+v", feeds)
	}
}

func TestForwardChord_HoldMealTapBlue_OpensPicker(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio", "Pip")
	h.pressBtn(t, domain.BtnGreen) // hold meal
	h.pressBtn(t, domain.BtnBlue)  // tap Blue → chord
	if h.m.Mode() != ModeAddInSelect {
		t.Fatalf("forward chord should open the picker, mode = %s", h.m.Mode())
	}
	sc, ok := h.oled.Last().(oled.AddInSelectScene)
	if !ok {
		t.Fatalf("scene = %T, want AddInSelectScene", h.oled.Last())
	}
	if sc.Dog.Name != "Cleo" || sc.Score != domain.ScoreFull {
		t.Fatalf("picker header = %s/%s, want Cleo/full", sc.Dog.Name, sc.Score)
	}
	// A real tag selected → committed with exactly that tag, advance to Rio.
	h.fireRot(t, rotary.PressShort)
	feeds := h.allFeedings(t)
	if len(feeds) != 1 || len(feeds[0].Tags) != 1 || feeds[0].Tags[0].IsUnspecified {
		t.Fatalf("selecting a tag should commit with one real add-in, got %+v", feeds)
	}
	if h.m.Mode() != ModeIdle || h.m.SelectedDog().Name != "Rio" {
		t.Fatalf("after select: mode=%s sel=%s, want idle/Rio", h.m.Mode(), h.m.SelectedDog().Name)
	}
	h.releaseBtn(t, domain.BtnBlue)
	h.releaseBtn(t, domain.BtnGreen)
}

func TestReverseChord_HoldBlueTapMeal_OpensPicker(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio")
	h.pressBtn(t, domain.BtnBlue)   // hold Blue
	h.pressBtn(t, domain.BtnYellow) // tap meal → chord
	if h.m.Mode() != ModeAddInSelect {
		t.Fatalf("reverse chord should open the picker, mode = %s", h.m.Mode())
	}
	sc := h.oled.Last().(oled.AddInSelectScene)
	if sc.Score != domain.ScorePartial {
		t.Fatalf("picker score = %s, want partial", sc.Score)
	}
	h.releaseBtn(t, domain.BtnYellow)
	h.releaseBtn(t, domain.BtnBlue)
	// Blue release must NOT record a snack (the hold was a chord).
	if snacks, _ := h.store.ListSnacks(h.ctx, store.SnackFilter{}); len(snacks) != 0 {
		t.Fatalf("reverse chord should not record a snack, got %d", len(snacks))
	}
}

func TestAddIn_OtherAttachesUnspecified(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio")
	h.pressBtn(t, domain.BtnGreen)
	h.pressBtn(t, domain.BtnBlue) // chord
	// The trailing "Other (name later)" row is last; one CCW from index 0 wraps to it.
	h.fireRot(t, rotary.RotateCCW)
	sc := h.oled.Last().(oled.AddInSelectScene)
	if !sc.Choices[sc.Index].IsOther {
		t.Fatalf("CCW from top should land on the Other row, got %+v", sc.Choices[sc.Index])
	}
	h.fireRot(t, rotary.PressShort)
	feeds := h.allFeedings(t)
	if len(feeds) != 1 || len(feeds[0].Tags) != 1 || !feeds[0].Tags[0].IsUnspecified {
		t.Fatalf("Other should attach the Unspecified sentinel, got %+v", feeds)
	}
	if feeds[0].Tags[0].ID != store.UnspecifiedTagID {
		t.Errorf("tag id = %d, want UnspecifiedTagID %d", feeds[0].Tags[0].ID, store.UnspecifiedTagID)
	}
}

func TestAddIn_IdleTimeoutCommitsUntagged(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio")
	h.pressBtn(t, domain.BtnGreen)
	h.pressBtn(t, domain.BtnBlue) // chord → picker
	if h.m.Mode() != ModeAddInSelect {
		t.Fatal("setup: expected picker")
	}
	h.clk.Advance(h.m.d.Cfg.AddInIdle())
	h.m.onTick(h.ctx)
	feeds := h.allFeedings(t)
	if len(feeds) != 1 || len(feeds[0].Tags) != 0 {
		t.Fatalf("walk-away should commit one untagged meal, got %+v", feeds)
	}
	if h.m.Mode() != ModeIdle {
		t.Fatalf("after timeout commit, mode = %s, want idle", h.m.Mode())
	}
}

func TestAddIn_AllFedRecheckAfterCommit(t *testing.T) {
	h := newHarness(t, "Solo") // single dog → committing the chord completes the meal
	h.pressBtn(t, domain.BtnGreen)
	h.pressBtn(t, domain.BtnBlue) // chord
	h.fireRot(t, rotary.PressShort)
	if h.m.Mode() != ModeLockedSummary {
		t.Fatalf("an add-in commit that completes the meal should lock, mode = %s", h.m.Mode())
	}
}

func TestBlueAlone_StillRoutesToSnack(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio")
	// Blue pressed and released with no meal button held → snack mode.
	h.pressBtn(t, domain.BtnBlue)
	if h.m.Mode() != ModeIdle {
		t.Fatalf("Blue press alone should not change mode yet, got %s", h.m.Mode())
	}
	h.releaseBtn(t, domain.BtnBlue)
	if h.m.Mode() != ModeSnackMode {
		t.Fatalf("Blue alone should enter snack mode, got %s", h.m.Mode())
	}
}

func TestBlueLongPress_InLockedSummary_EntersSnack(t *testing.T) {
	h := newHarness(t, "Cleo") // 1 dog → first meal locks immediately
	h.fireBtn(t, domain.BtnGreen)
	if h.m.Mode() != ModeLockedSummary {
		t.Fatalf("setup: expected locked, got %s", h.m.Mode())
	}
	// A plain Blue tap in LockedSummary must NOT enter snack mode.
	h.fireBtn(t, domain.BtnBlue)
	if h.m.Mode() != ModeLockedSummary {
		t.Fatalf("a Blue tap in locked should be ignored, got %s", h.m.Mode())
	}
	// Hold Blue past the long-press threshold; the tick detects it.
	h.pressBtn(t, domain.BtnBlue)
	h.clk.Advance(h.m.d.Cfg.LongPress() + time.Second)
	h.m.onTick(h.ctx)
	if h.m.Mode() != ModeSnackMode {
		t.Fatalf("Blue long-press in locked should enter snack, got %s", h.m.Mode())
	}
	// The trailing Blue release is consumed silently (no snack recorded).
	h.releaseBtn(t, domain.BtnBlue)
	if snacks, _ := h.store.ListSnacks(h.ctx, store.SnackFilter{}); len(snacks) != 0 {
		t.Fatalf("the long-press release should not record a snack, got %d", len(snacks))
	}
}

func TestRotaryIgnoredWhileMealHeld(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio", "Pip")
	h.pressBtn(t, domain.BtnGreen) // hold meal → pending bound to Cleo
	h.fireRot(t, rotary.RotateCW)  // should be ignored
	if h.m.SelectedDog().Name != "Cleo" {
		t.Fatalf("rotary should be ignored while a meal button is held, sel = %s", h.m.SelectedDog().Name)
	}
	h.releaseBtn(t, domain.BtnGreen)
	feeds := h.allFeedings(t)
	if len(feeds) != 1 || feeds[0].DogID != h.m.dogs[0].ID {
		t.Fatalf("pending should commit for Cleo (selected at press time), got %+v", feeds)
	}
}
