package state

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/scottyturner/pupcup/internal/clock"
	"github.com/scottyturner/pupcup/internal/config"
	"github.com/scottyturner/pupcup/internal/device/buttons"
	"github.com/scottyturner/pupcup/internal/device/neopixel"
	"github.com/scottyturner/pupcup/internal/device/oled"
	"github.com/scottyturner/pupcup/internal/device/rotary"
	"github.com/scottyturner/pupcup/internal/domain"
	"github.com/scottyturner/pupcup/internal/eventbus"
	"github.com/scottyturner/pupcup/internal/store"
)

type harness struct {
	m     *Machine
	clk   *clock.Fake
	store *store.Store
	bus   *eventbus.Bus
	btn   *buttons.Fake
	rot   *rotary.Fake
	oled  *oled.Fake
	leds  *neopixel.Fake
	ctx   context.Context
}

func newHarness(t *testing.T, dogNames ...string) *harness {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	for i, name := range dogNames {
		if _, err := st.CreateDog(ctx, domain.Dog{Name: name, AccentColor: "#A8D8B9", SortOrder: i}); err != nil {
			t.Fatal(err)
		}
	}

	cfg := config.Default()
	if err := func() error {
		// validate to populate Location
		// Re-use Load with empty path so env doesn't leak.
		c, err := config.Load("")
		if err != nil {
			return err
		}
		cfg = c
		return nil
	}(); err != nil {
		t.Fatal(err)
	}

	clk := clock.NewFake(time.Date(2026, 4, 29, 8, 0, 0, 0, time.UTC))
	bus := eventbus.New(64, slog.Default())
	t.Cleanup(bus.Close)

	h := &harness{
		clk:   clk,
		store: st,
		bus:   bus,
		btn:   buttons.NewFake(),
		rot:   rotary.NewFake(),
		oled:  oled.NewFake(),
		leds:  neopixel.NewFake(8),
		ctx:   ctx,
	}
	t.Cleanup(func() {
		h.btn.Close()
		h.rot.Close()
		h.oled.Close()
		h.leds.Close()
	})

	m, err := New(Deps{
		Cfg:     &cfg,
		Log:     slog.Default(),
		Clk:     clk,
		Bus:     bus,
		Store:   st,
		Buttons: h.btn,
		Rotary:  h.rot,
		OLED:    h.oled,
		LEDs:    h.leds,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	h.m = m
	return h
}

// fireBtn taps a button: a press immediately followed by a release, processed
// synchronously (the common quick-tap gesture under the both-edge driver).
func (h *harness) fireBtn(t *testing.T, c domain.ButtonColor) {
	t.Helper()
	h.pressBtn(t, c)
	h.releaseBtn(t, c)
}

func (h *harness) pressBtn(t *testing.T, c domain.ButtonColor) {
	t.Helper()
	h.m.onButton(h.ctx, buttons.ButtonEvent{Color: c, Action: buttons.ActionPress, TS: h.clk.Now()})
}

func (h *harness) releaseBtn(t *testing.T, c domain.ButtonColor) {
	t.Helper()
	h.m.onButton(h.ctx, buttons.ButtonEvent{Color: c, Action: buttons.ActionRelease, TS: h.clk.Now()})
}

func (h *harness) fireRot(t *testing.T, k rotary.EventKind) {
	t.Helper()
	h.m.onRotary(h.ctx, rotary.Event{Kind: k, TS: h.clk.Now()})
}

func TestBoot_Idle_ShowsSelector(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio", "Pip")
	if h.m.Mode() != ModeIdle {
		t.Fatalf("mode = %s", h.m.Mode())
	}
	last, ok := h.oled.Last().(oled.DogSelectorScene)
	if !ok {
		t.Fatalf("scene = %T", h.oled.Last())
	}
	if last.Dog.Name != "Cleo" {
		t.Fatalf("dog = %q", last.Dog.Name)
	}
}

func TestRotary_ScrollsDogs(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio", "Pip")
	h.fireRot(t, rotary.RotateCW)
	if h.m.SelectedDog().Name != "Rio" {
		t.Fatalf("after CW: %s", h.m.SelectedDog().Name)
	}
	h.fireRot(t, rotary.RotateCW)
	if h.m.SelectedDog().Name != "Pip" {
		t.Fatalf("after 2x CW: %s", h.m.SelectedDog().Name)
	}
	// Wraps.
	h.fireRot(t, rotary.RotateCW)
	if h.m.SelectedDog().Name != "Cleo" {
		t.Fatalf("after wrap: %s", h.m.SelectedDog().Name)
	}
	// CCW reverses.
	h.fireRot(t, rotary.RotateCCW)
	if h.m.SelectedDog().Name != "Pip" {
		t.Fatalf("CCW: %s", h.m.SelectedDog().Name)
	}
}

func TestMealButtons_RecordAndAdvance(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio", "Pip")
	h.fireBtn(t, domain.BtnGreen) // Cleo full
	if h.m.Mode() != ModeIdle {
		t.Fatalf("expected still idle, got %s", h.m.Mode())
	}
	if h.m.SelectedDog().Name != "Rio" {
		t.Fatalf("expected advance to Rio, got %s", h.m.SelectedDog().Name)
	}
	h.fireBtn(t, domain.BtnYellow) // Rio partial
	if h.m.SelectedDog().Name != "Pip" {
		t.Fatalf("advance to Pip, got %s", h.m.SelectedDog().Name)
	}
	h.fireBtn(t, domain.BtnRed) // Pip none → all fed
	if h.m.Mode() != ModeLockedSummary {
		t.Fatalf("expected LockedSummary, got %s", h.m.Mode())
	}
	// Verify feedings persisted.
	got, err := h.store.ListFeedings(h.ctx, store.FeedingFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d feedings", len(got))
	}
	scoreByDog := map[int64]domain.Score{}
	for _, f := range got {
		scoreByDog[f.DogID] = f.Score
	}
	dogs, _ := h.store.ListDogs(h.ctx)
	if scoreByDog[dogs[0].ID] != domain.ScoreFull ||
		scoreByDog[dogs[1].ID] != domain.ScorePartial ||
		scoreByDog[dogs[2].ID] != domain.ScoreNone {
		t.Fatalf("scores: %+v", scoreByDog)
	}
	// LED bar shows per-dog meal quality: Cleo full=green [0,1], spacer [2],
	// Rio partial=yellow [3,4], spacer [5], Pip none=red [6,7].
	last := h.leds.LastFrame()
	if last == nil {
		t.Fatalf("no LED frame rendered")
	}
	want := []neopixel.Color{
		ledFull, ledFull, neopixel.ColorOff,
		ledPartial, ledPartial, neopixel.ColorOff,
		ledNone, ledNone,
	}
	for i := range want {
		if last[i] != want[i] {
			t.Fatalf("LED[%d] = %+v, want %+v (full frame %+v)", i, last[i], want[i], last)
		}
	}
	// OLED summary scene.
	if _, ok := h.oled.Last().(oled.LockedSummaryScene); !ok {
		t.Fatalf("scene = %T", h.oled.Last())
	}
	// Lock persisted.
	lock, _ := h.store.GetDeviceLock(h.ctx)
	if lock.Until == nil {
		t.Fatal("lock not persisted")
	}
}

func TestLockedMode_IgnoresMealButtons(t *testing.T) {
	h := newHarness(t, "Cleo")
	h.fireBtn(t, domain.BtnGreen) // 1 dog → triggers lock immediately
	if h.m.Mode() != ModeLockedSummary {
		t.Fatalf("expected lock, got %s", h.m.Mode())
	}
	before, _ := h.store.ListFeedings(h.ctx, store.FeedingFilter{})
	h.fireBtn(t, domain.BtnGreen)
	h.fireBtn(t, domain.BtnYellow)
	after, _ := h.store.ListFeedings(h.ctx, store.FeedingFilter{})
	if len(after) != len(before) {
		t.Fatalf("locked mode should ignore meal buttons; before=%d after=%d", len(before), len(after))
	}
}

func TestLockOverride_LongPressClears(t *testing.T) {
	h := newHarness(t, "Cleo")
	h.fireBtn(t, domain.BtnGreen)
	if h.m.Mode() != ModeLockedSummary {
		t.Fatal("expected locked")
	}
	h.fireRot(t, rotary.PressLong)
	if h.m.Mode() != ModeIdle {
		t.Fatalf("expected idle after override, got %s", h.m.Mode())
	}
	lock, _ := h.store.GetDeviceLock(h.ctx)
	if lock.Until != nil {
		t.Fatalf("lock not cleared: %+v", lock)
	}
}

func TestLockExpiry_ClearsOnTick(t *testing.T) {
	h := newHarness(t, "Cleo")
	h.fireBtn(t, domain.BtnGreen)
	// Advance past lock duration.
	h.clk.Advance(5 * time.Hour)
	h.m.onTick(h.ctx)
	if h.m.Mode() != ModeIdle {
		t.Fatalf("expected idle after expiry, got %s", h.m.Mode())
	}
}

func TestGraceTimeout_LocksPartialFromLastMeal(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio", "Pip")
	fedAt := h.clk.Now()
	h.fireBtn(t, domain.BtnGreen) // only Cleo fed; meal incomplete → still Idle
	if h.m.Mode() != ModeIdle {
		t.Fatalf("expected Idle mid-meal, got %s", h.m.Mode())
	}

	// Grace window (default 15m) elapses with the meal still incomplete.
	h.clk.Advance(h.m.d.Cfg.MealCompleteGrace())
	h.m.onTick(h.ctx)

	if h.m.Mode() != ModeLockedSummary {
		t.Fatalf("expected LockedSummary after grace, got %s", h.m.Mode())
	}
	// Un-fed dogs get NO record: exactly one feeding persisted.
	got, _ := h.store.ListFeedings(h.ctx, store.FeedingFilter{})
	if len(got) != 1 {
		t.Fatalf("expected 1 feeding (partial), got %d", len(got))
	}
	// Expiry is timed from the last (only) meal, not from the grace tick.
	lock := h.m.Lock()
	if lock.Until == nil {
		t.Fatal("lock not set")
	}
	want := fedAt.Add(h.m.d.Cfg.MealLock())
	if !lock.Until.Equal(want) {
		t.Fatalf("lock.Until = %s, want %s (last meal + lock)", lock.Until, want)
	}
	if lock.Reason != "meal grace timeout" {
		t.Fatalf("lock reason = %q", lock.Reason)
	}
}

func TestGraceTimeout_ResetByEachFeeding(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio", "Pip")
	h.fireBtn(t, domain.BtnGreen) // Cleo @ t0
	h.clk.Advance(10 * time.Minute)
	h.fireBtn(t, domain.BtnYellow) // Rio @ t0+10m → resets grace timer
	h.clk.Advance(10 * time.Minute)
	h.m.onTick(h.ctx) // only 10m since last meal (< 15m grace) → no lock yet
	if h.m.Mode() != ModeIdle {
		t.Fatalf("grace should have reset on 2nd feeding; got %s", h.m.Mode())
	}
	h.clk.Advance(5 * time.Minute)
	h.m.onTick(h.ctx) // now 15m since last meal → lock
	if h.m.Mode() != ModeLockedSummary {
		t.Fatalf("expected lock 15m after last meal, got %s", h.m.Mode())
	}
	got, _ := h.store.ListFeedings(h.ctx, store.FeedingFilter{})
	if len(got) != 2 {
		t.Fatalf("expected 2 feedings, got %d", len(got))
	}
}

func TestAllFed_ExpiryTimedFromLastMeal(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio")
	h.fireBtn(t, domain.BtnGreen) // Cleo @ t0
	h.clk.Advance(30 * time.Minute)
	lastMeal := h.clk.Now()
	h.fireBtn(t, domain.BtnYellow) // Rio @ t0+30m → all fed → immediate lock
	if h.m.Mode() != ModeLockedSummary {
		t.Fatalf("expected LockedSummary on all-fed, got %s", h.m.Mode())
	}
	lock := h.m.Lock()
	want := lastMeal.Add(h.m.d.Cfg.MealLock())
	if lock.Until == nil || !lock.Until.Equal(want) {
		t.Fatalf("lock.Until = %v, want %s (last meal + lock, not first meal)", lock.Until, want)
	}
	if lock.Reason != "meal complete" {
		t.Fatalf("lock reason = %q", lock.Reason)
	}
}

func TestSnackMode_FromIdle(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio")
	h.fireBtn(t, domain.BtnBlue)
	if h.m.Mode() != ModeSnackMode {
		t.Fatalf("mode = %s", h.m.Mode())
	}
	h.fireBtn(t, domain.BtnBlue) // record snack for Cleo
	snacks, _ := h.store.ListSnacks(h.ctx, store.SnackFilter{})
	if len(snacks) != 1 {
		t.Fatalf("snacks = %d", len(snacks))
	}
}

func TestSnackMode_AllSnackedExits(t *testing.T) {
	h := newHarness(t, "Cleo")
	h.fireBtn(t, domain.BtnBlue) // enter snack
	h.fireBtn(t, domain.BtnBlue) // record + auto-exit (only 1 dog)
	if h.m.Mode() == ModeSnackMode {
		t.Fatal("expected snack mode to exit after all dogs snacked")
	}
}

func TestSnackMode_IdleTimeoutExits(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio")
	h.fireBtn(t, domain.BtnBlue)
	h.clk.Advance(2 * time.Minute)
	h.m.onTick(h.ctx)
	if h.m.Mode() != ModeIdle {
		t.Fatalf("expected idle after snack timeout, got %s", h.m.Mode())
	}
}

func TestBootstrap_RehydratesLock(t *testing.T) {
	h := newHarness(t, "Cleo")
	h.fireBtn(t, domain.BtnGreen) // sets lock
	if h.m.Mode() != ModeLockedSummary {
		t.Fatal("setup")
	}
	// Simulate restart by constructing a fresh machine on the same store/clock.
	h2 := newHarnessExisting(t, h)
	if h2.m.Mode() != ModeLockedSummary {
		t.Fatalf("after restart mode = %s, want LockedSummary", h2.m.Mode())
	}
}

func newHarnessExisting(t *testing.T, prev *harness) *harness {
	t.Helper()
	cfg, _ := config.Load("")
	bus := eventbus.New(64, slog.Default())
	t.Cleanup(bus.Close)
	h := &harness{
		clk:   prev.clk,
		store: prev.store,
		bus:   bus,
		btn:   buttons.NewFake(),
		rot:   rotary.NewFake(),
		oled:  oled.NewFake(),
		leds:  neopixel.NewFake(8),
		ctx:   prev.ctx,
	}
	t.Cleanup(func() {
		h.btn.Close()
		h.rot.Close()
		h.oled.Close()
		h.leds.Close()
	})
	m, err := New(Deps{
		Cfg: &cfg, Log: slog.Default(), Clk: prev.clk, Bus: bus, Store: prev.store,
		Buttons: h.btn, Rotary: h.rot, OLED: h.oled, LEDs: h.leds,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.bootstrap(prev.ctx); err != nil {
		t.Fatal(err)
	}
	h.m = m
	return h
}

func TestEventBus_PublishesFeedings(t *testing.T) {
	h := newHarness(t, "Cleo")
	ch, cancel := h.bus.Subscribe()
	defer cancel()
	h.fireBtn(t, domain.BtnGreen)
	select {
	case e := <-ch:
		if _, ok := e.(domain.FeedRecorded); !ok {
			// could be LockChanged first
			if _, ok2 := e.(domain.LockChanged); !ok2 {
				t.Fatalf("unexpected event %T", e)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
}

// TestRun_NotifyDogsChanged_ReloadsDogs covers the web→device refresh wiring:
// a dog added to the store (as a web create would) becomes visible to the
// running machine after NotifyDogsChanged, with no restart. Exercises the real
// production path (the buffered signal + the Run-loop select case), not just
// the reload helper.
func TestRun_NotifyDogsChanged_ReloadsDogs(t *testing.T) {
	h := newHarness(t, "Cleo")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- h.m.Run(ctx) }()

	if _, err := h.store.CreateDog(ctx, domain.Dog{Name: "Rio", AccentColor: "#A8D8B9", SortOrder: 1}); err != nil {
		t.Fatal(err)
	}
	h.m.NotifyDogsChanged()

	deadline := time.Now().Add(2 * time.Second)
	for {
		h.m.mu.RLock()
		n := len(h.m.dogs)
		h.m.mu.RUnlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("machine did not reload dogs after NotifyDogsChanged; have %d", n)
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	<-done
}

// TestReloadDogs_ClampsSelection verifies a deletion that shrinks the list
// below the current selection resets the cursor rather than leaving it past the
// end (which would panic m.dogs[m.sel] on the next render/select).
func TestReloadDogs_ClampsSelection(t *testing.T) {
	h := newHarness(t, "Cleo", "Rio", "Pip")
	h.fireRot(t, rotary.RotateCW)
	h.fireRot(t, rotary.RotateCW) // select index 2 (Pip)
	if got := h.m.SelectedDog().Name; got != "Pip" {
		t.Fatalf("pre-condition: selected = %q, want Pip", got)
	}

	// Soft-delete the two dogs after the selection (fresh dogs have no history).
	for _, name := range []string{"Rio", "Pip"} {
		dogs, _ := h.store.ListDogs(h.ctx)
		for _, d := range dogs {
			if d.Name == name {
				if err := h.store.SoftDeleteDog(h.ctx, d.ID); err != nil {
					t.Fatal(err)
				}
			}
		}
	}

	h.m.reloadDogs(h.ctx)

	h.m.mu.RLock()
	n, sel := len(h.m.dogs), h.m.sel
	h.m.mu.RUnlock()
	if n != 1 {
		t.Fatalf("dogs after reload = %d, want 1", n)
	}
	if sel != 0 {
		t.Fatalf("sel after reload = %d, want clamped to 0", sel)
	}
	if got := h.m.SelectedDog().Name; got != "Cleo" {
		t.Fatalf("selected = %q, want Cleo", got)
	}
}

func TestScoreColor(t *testing.T) {
	cases := []struct {
		score domain.Score
		want  neopixel.Color
	}{
		{domain.ScoreFull, ledFull},
		{domain.ScorePartial, ledPartial},
		{domain.ScoreNone, ledNone},
		{domain.Score(""), neopixel.ColorOff}, // un-fed dog at a partial-meal lock
		{domain.Score("bogus"), neopixel.ColorOff},
	}
	for _, c := range cases {
		if got := scoreColor(c.score); got != c.want {
			t.Errorf("scoreColor(%q) = %+v, want %+v", c.score, got, c.want)
		}
	}
}

func TestSummaryFrame(t *testing.T) {
	g, y, r := ledFull, ledPartial, ledNone
	off := neopixel.ColorOff

	tests := []struct {
		name   string
		pixels int
		scores []neopixel.Color
		want   []neopixel.Color
	}{
		{
			name: "one dog fills the bar",
			pixels: 8, scores: []neopixel.Color{g},
			want: []neopixel.Color{g, g, g, g, g, g, g, g},
		},
		{
			name: "two dogs split 4/4, no gap",
			pixels: 8, scores: []neopixel.Color{g, r},
			want: []neopixel.Color{g, g, g, g, r, r, r, r},
		},
		{
			name: "three dogs: 2-gap-2-gap-2",
			pixels: 8, scores: []neopixel.Color{g, y, r},
			want: []neopixel.Color{g, g, off, y, y, off, r, r},
		},
		{
			name: "four dogs has no layout -> nil (caller falls back)",
			pixels: 8, scores: []neopixel.Color{g, g, g, g},
			want: nil,
		},
		{
			name: "non-8-pixel bar -> nil",
			pixels: 12, scores: []neopixel.Color{g, y, r},
			want: nil,
		},
		{
			name: "zero dogs -> nil",
			pixels: 8, scores: nil,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summaryFrame(tt.pixels, tt.scores)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d (got %+v)", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("pixel[%d] = %+v, want %+v (full %+v)", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}
