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
	m       *Machine
	clk     *clock.Fake
	store   *store.Store
	bus     *eventbus.Bus
	btn     *buttons.Fake
	rot     *rotary.Fake
	oled    *oled.Fake
	leds    *neopixel.Fake
	ctx     context.Context
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
		Clk:    clk,
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

// fire injects a button event and processes it synchronously.
func (h *harness) fireBtn(t *testing.T, c domain.ButtonColor) {
	t.Helper()
	h.m.onButton(h.ctx, buttons.ButtonEvent{Color: c, TS: h.clk.Now()})
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
	// LED bar should be green-ish.
	last := h.leds.LastFrame()
	if last == nil || last[0].G == 0 {
		t.Fatalf("expected green LEDs, got %+v", last)
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
