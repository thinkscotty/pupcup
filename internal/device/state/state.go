// Package state implements the device state machine: it reads hardware
// events from the buttons + rotary drivers, drives the OLED + NeoPixel
// outputs, persists state via the SQLite store, and publishes domain events
// on the in-process bus.
//
// Modes:
//
//	Idle           - Rotary scrolls dogs; meal buttons record a feeding for
//	                 the selected dog and advance to the next.
//	LockedSummary  - All dogs have a feeding within the current meal window;
//	                 meal buttons are ignored; LED bar is solid green.
//	                 Long-press rotary SW clears the lock; long-press blue
//	                 enters SnackMode.
//	SnackMode      - Pick a dog with the rotary; tap blue to record a snack.
//	                 Exits on idle timeout or "all dogs recorded".
package state

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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

// Mode is the high-level state of the device.
type Mode int

const (
	ModeIdle Mode = iota
	ModeLockedSummary
	ModeSnackMode
)

func (m Mode) String() string {
	switch m {
	case ModeIdle:
		return "idle"
	case ModeLockedSummary:
		return "locked"
	case ModeSnackMode:
		return "snack"
	}
	return "?"
}

// Deps bundles every collaborator the state machine needs.
type Deps struct {
	Cfg   *config.Config
	Log   *slog.Logger
	Clk   clock.Clock
	Bus   *eventbus.Bus
	Store *store.Store

	Buttons buttons.Driver
	Rotary  rotary.Driver
	OLED    oled.Renderer
	LEDs    neopixel.Strip
}

// Machine is the state machine. Construct with New, then call Run.
type Machine struct {
	d Deps

	mu   sync.RWMutex
	mode Mode
	dogs []domain.Dog
	sel  int

	// mealSession tracks scores recorded in the current meal sweep, by dog id.
	mealSession map[int64]domain.Score
	// snackSession tracks dogs already snacked in the current snack burst.
	snackSession map[int64]bool
	// returnTo is the mode to revert to when SnackMode exits.
	returnTo Mode

	// lock is the persisted meal-lock state.
	lock domain.DeviceLock
	// lastInteract is updated whenever we receive any input; powers the
	// snack-mode idle timeout.
	lastInteract time.Time
}

// New constructs a Machine. Pass at least Cfg, Bus, Store, Buttons, Rotary,
// OLED, and LEDs. Clk and Log default to real / slog.Default.
func New(d Deps) (*Machine, error) {
	if d.Cfg == nil {
		return nil, errors.New("state: nil cfg")
	}
	if d.Bus == nil {
		return nil, errors.New("state: nil bus")
	}
	if d.Store == nil {
		return nil, errors.New("state: nil store")
	}
	if d.Buttons == nil || d.Rotary == nil || d.OLED == nil || d.LEDs == nil {
		return nil, errors.New("state: nil hardware driver")
	}
	if d.Clk == nil {
		d.Clk = clock.Real{}
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	d.Log = d.Log.With("component", "device.state")
	return &Machine{
		d:            d,
		mealSession:  map[int64]domain.Score{},
		snackSession: map[int64]bool{},
	}, nil
}

// Mode returns the current mode (thread-safe).
func (m *Machine) Mode() Mode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mode
}

// Lock returns a snapshot of the device lock (thread-safe).
func (m *Machine) Lock() domain.DeviceLock {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lock
}

// SelectedDog returns the currently selected dog, or zero value if none.
func (m *Machine) SelectedDog() domain.Dog {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.sel < 0 || m.sel >= len(m.dogs) {
		return domain.Dog{}
	}
	return m.dogs[m.sel]
}

// Run blocks until ctx is canceled. It reads from the hardware event channels
// and drives the OLED, LEDs, store, and bus.
func (m *Machine) Run(ctx context.Context) error {
	if err := m.bootstrap(ctx); err != nil {
		return err
	}

	// Periodic tick for clock refresh, lock expiry checks, snack idle.
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case ev, ok := <-m.d.Buttons.Events():
			if !ok {
				return errors.New("state: button driver closed")
			}
			m.onButton(ctx, ev)

		case ev, ok := <-m.d.Rotary.Events():
			if !ok {
				return errors.New("state: rotary driver closed")
			}
			m.onRotary(ctx, ev)

		case <-tick.C:
			m.onTick(ctx)
		}
	}
}

// ----------------------------- bootstrap ------------------------------------

func (m *Machine) bootstrap(ctx context.Context) error {
	dogs, err := m.d.Store.ListDogs(ctx)
	if err != nil {
		return fmt.Errorf("state: load dogs: %w", err)
	}
	if len(dogs) == 0 {
		m.d.Log.Warn("no dogs configured; rotary will be inert until one is added via the web app")
	}
	lock, err := m.d.Store.GetDeviceLock(ctx)
	if err != nil {
		return fmt.Errorf("state: load lock: %w", err)
	}

	m.mu.Lock()
	m.dogs = dogs
	m.sel = 0
	m.lock = lock
	m.lastInteract = m.d.Clk.Now()
	if lock.IsLocked(m.d.Clk.Now()) {
		m.mode = ModeLockedSummary
		m.rehydrateMealSession(ctx)
	} else {
		m.mode = ModeIdle
		// If lock is set but expired, clear it.
		if lock.Until != nil {
			m.lock = domain.DeviceLock{}
			_ = m.d.Store.SetDeviceLock(ctx, m.lock)
			m.d.Bus.Publish(domain.LockChanged{Lock: m.lock, At: m.d.Clk.Now()})
		}
	}
	m.mu.Unlock()

	m.render(ctx)
	return nil
}

// rehydrateMealSession populates mealSession from the last feedings, used
// when the device boots into a still-active LockedSummary state. Called with
// m.mu held by the caller.
func (m *Machine) rehydrateMealSession(ctx context.Context) {
	if m.lock.Until == nil {
		return
	}
	since := m.lock.Until.Add(-m.d.Cfg.MealLock())
	feedings, err := m.d.Store.ListFeedings(ctx, store.FeedingFilter{Since: since})
	if err != nil {
		m.d.Log.Warn("rehydrate session", "err", err)
		return
	}
	for _, f := range feedings {
		if _, ok := m.mealSession[f.DogID]; !ok {
			m.mealSession[f.DogID] = f.Score
		}
	}
}

// RefreshDogs re-loads the dog list (call after web edits create/delete dogs).
// Safe to call concurrently with Run.
func (m *Machine) RefreshDogs(ctx context.Context) {
	dogs, err := m.d.Store.ListDogs(ctx)
	if err != nil {
		m.d.Log.Warn("refresh dogs", "err", err)
		return
	}
	m.mu.Lock()
	m.dogs = dogs
	if m.sel >= len(dogs) {
		m.sel = 0
	}
	m.mu.Unlock()
	m.render(ctx)
}

// ----------------------------- input handlers -------------------------------

func (m *Machine) onButton(ctx context.Context, ev buttons.ButtonEvent) {
	m.mu.Lock()
	m.lastInteract = m.d.Clk.Now()
	mode := m.mode
	m.mu.Unlock()

	switch mode {
	case ModeIdle:
		m.handleIdleButton(ctx, ev)
	case ModeLockedSummary:
		m.handleLockedButton(ctx, ev)
	case ModeSnackMode:
		m.handleSnackButton(ctx, ev)
	}
}

func (m *Machine) handleIdleButton(ctx context.Context, ev buttons.ButtonEvent) {
	if ev.Color == domain.BtnBlue {
		m.enterSnackMode(ctx, ModeIdle)
		return
	}
	score, ok := ev.Color.MealScore()
	if !ok {
		return
	}
	m.mu.Lock()
	if len(m.dogs) == 0 {
		m.mu.Unlock()
		return
	}
	dog := m.dogs[m.sel]
	m.mu.Unlock()

	if err := m.recordFeeding(ctx, dog.ID, score, domain.SourceButton, ev.TS); err != nil {
		m.d.Log.Error("record feeding", "err", err, "dog", dog.ID)
		return
	}

	m.mu.Lock()
	// Advance to next dog (skip dogs already in this session).
	m.advanceToUnfedDog()
	allFed := len(m.mealSession) >= len(m.dogs) && len(m.dogs) > 0
	m.mu.Unlock()

	if allFed {
		m.transitionToLocked(ctx)
		return
	}
	m.render(ctx)
}

func (m *Machine) handleLockedButton(ctx context.Context, ev buttons.ButtonEvent) {
	// In locked mode, meal buttons are ignored; blue requires long-press
	// (handled in enterSnackMode via long-press detection — but our buttons
	// driver only emits taps). Per build plan §6.5 the long-press of BLUE
	// happens in this mode; since the buttons driver doesn't emit hold, we
	// approximate by entering snack mode on a single tap of blue here too.
	// TODO: extend buttons driver with hold detection if a tap-only proves
	// too easy to trigger accidentally.
	if ev.Color == domain.BtnBlue {
		m.enterSnackMode(ctx, ModeLockedSummary)
		return
	}
	// Otherwise ignored.
}

func (m *Machine) handleSnackButton(ctx context.Context, ev buttons.ButtonEvent) {
	if ev.Color != domain.BtnBlue {
		return
	}
	m.mu.Lock()
	if len(m.dogs) == 0 {
		m.mu.Unlock()
		return
	}
	dog := m.dogs[m.sel]
	m.mu.Unlock()

	if err := m.recordSnack(ctx, dog.ID, domain.SourceButton, ev.TS); err != nil {
		m.d.Log.Error("record snack", "err", err, "dog", dog.ID)
		return
	}
	m.mu.Lock()
	allSnacked := len(m.snackSession) >= len(m.dogs)
	m.mu.Unlock()
	if allSnacked {
		m.exitSnackMode(ctx)
		return
	}
	m.render(ctx)
}

func (m *Machine) onRotary(ctx context.Context, ev rotary.Event) {
	m.mu.Lock()
	m.lastInteract = m.d.Clk.Now()
	mode := m.mode
	m.mu.Unlock()

	switch ev.Kind {
	case rotary.RotateCW:
		m.adjustSelection(+1)
		m.render(ctx)
	case rotary.RotateCCW:
		m.adjustSelection(-1)
		m.render(ctx)
	case rotary.PressShort:
		// Reserved for future use (e.g. confirm a partial snack). Currently a no-op.
	case rotary.PressLong:
		if mode == ModeLockedSummary {
			m.clearLock(ctx, "rotary override")
		}
	}
}

func (m *Machine) onTick(ctx context.Context) {
	m.mu.Lock()
	now := m.d.Clk.Now()
	mode := m.mode
	expired := mode == ModeLockedSummary && m.lock.Until != nil && !now.Before(*m.lock.Until)
	idleSnack := mode == ModeSnackMode && now.Sub(m.lastInteract) >= m.d.Cfg.SnackIdle()
	m.mu.Unlock()

	switch {
	case expired:
		m.clearLock(ctx, "expired")
	case idleSnack:
		m.exitSnackMode(ctx)
	default:
		// Re-render once a second so countdown and clock stay current.
		m.render(ctx)
	}
}

// ----------------------------- transitions ----------------------------------

func (m *Machine) transitionToLocked(ctx context.Context) {
	now := m.d.Clk.Now()
	until := now.Add(m.d.Cfg.MealLock())
	lock := domain.DeviceLock{Until: &until, Reason: "meal complete"}
	if err := m.d.Store.SetDeviceLock(ctx, lock); err != nil {
		m.d.Log.Error("persist lock", "err", err)
	}
	m.mu.Lock()
	m.mode = ModeLockedSummary
	m.lock = lock
	m.mu.Unlock()
	m.d.Bus.Publish(domain.LockChanged{Lock: lock, At: now})
	m.d.Log.Info("locked summary entered", "until", until)
	m.render(ctx)
}

func (m *Machine) clearLock(ctx context.Context, reason string) {
	if err := m.d.Store.SetDeviceLock(ctx, domain.DeviceLock{}); err != nil {
		m.d.Log.Error("clear lock", "err", err)
	}
	m.mu.Lock()
	m.mode = ModeIdle
	m.lock = domain.DeviceLock{}
	for k := range m.mealSession {
		delete(m.mealSession, k)
	}
	for k := range m.snackSession {
		delete(m.snackSession, k)
	}
	m.mu.Unlock()
	m.d.Bus.Publish(domain.LockChanged{Lock: domain.DeviceLock{}, At: m.d.Clk.Now()})
	m.d.Log.Info("lock cleared", "reason", reason)
	m.render(ctx)
}

func (m *Machine) enterSnackMode(ctx context.Context, returnTo Mode) {
	m.mu.Lock()
	m.returnTo = returnTo
	m.mode = ModeSnackMode
	for k := range m.snackSession {
		delete(m.snackSession, k)
	}
	m.mu.Unlock()
	m.d.Log.Info("snack mode entered", "return_to", returnTo.String())
	m.render(ctx)
}

func (m *Machine) exitSnackMode(ctx context.Context) {
	m.mu.Lock()
	target := m.returnTo
	m.mode = target
	for k := range m.snackSession {
		delete(m.snackSession, k)
	}
	m.mu.Unlock()
	m.d.Log.Info("snack mode exited", "to", target.String())
	m.render(ctx)
}

// ----------------------------- selection ------------------------------------

func (m *Machine) adjustSelection(delta int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.dogs) == 0 {
		return
	}
	m.sel = (m.sel + delta + len(m.dogs)) % len(m.dogs)
}

// advanceToUnfedDog moves m.sel to the next dog whose meal hasn't been
// recorded in this session. Caller holds m.mu.
func (m *Machine) advanceToUnfedDog() {
	if len(m.dogs) == 0 {
		return
	}
	for i := 1; i <= len(m.dogs); i++ {
		idx := (m.sel + i) % len(m.dogs)
		if _, fed := m.mealSession[m.dogs[idx].ID]; !fed {
			m.sel = idx
			return
		}
	}
	// All fed — leave m.sel where it is; transitionToLocked will fire.
}

// ----------------------------- recording ------------------------------------

func (m *Machine) recordFeeding(ctx context.Context, dogID int64, score domain.Score, source domain.Source, ts time.Time) error {
	f, err := m.d.Store.CreateFeeding(ctx, domain.Feeding{
		DogID:  dogID,
		TS:     ts.UTC(),
		Kind:   domain.FeedKind(m.d.Cfg.DefaultFeedKind),
		Score:  score,
		Source: source,
	})
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.mealSession[dogID] = score
	dog := m.findDog(dogID)
	m.mu.Unlock()
	m.d.Bus.Publish(domain.FeedRecorded{Feeding: f, Dog: dog})
	m.d.Log.Info("feeding recorded", "dog_id", dogID, "score", score, "source", source)
	return nil
}

func (m *Machine) recordSnack(ctx context.Context, dogID int64, source domain.Source, ts time.Time) error {
	sn, err := m.d.Store.CreateSnack(ctx, domain.Snack{
		DogID:  dogID,
		TS:     ts.UTC(),
		Source: source,
	})
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.snackSession[dogID] = true
	dog := m.findDog(dogID)
	m.mu.Unlock()
	m.d.Bus.Publish(domain.SnackRecorded{Snack: sn, Dog: dog})
	m.d.Log.Info("snack recorded", "dog_id", dogID, "source", source)
	return nil
}

// findDog returns the dog matching id, or zero. Caller holds m.mu.
func (m *Machine) findDog(id int64) domain.Dog {
	for _, d := range m.dogs {
		if d.ID == id {
			return d
		}
	}
	return domain.Dog{}
}

// ----------------------------- rendering ------------------------------------

func (m *Machine) render(ctx context.Context) {
	_ = ctx
	m.mu.RLock()
	mode := m.mode
	dogs := make([]domain.Dog, len(m.dogs))
	copy(dogs, m.dogs)
	sel := m.sel
	now := m.d.Clk.Now()
	mealSession := make(map[int64]domain.Score, len(m.mealSession))
	for k, v := range m.mealSession {
		mealSession[k] = v
	}
	snackSession := make(map[int64]bool, len(m.snackSession))
	for k, v := range m.snackSession {
		snackSession[k] = v
	}
	lock := m.lock
	m.mu.RUnlock()

	var scene oled.Scene
	switch mode {
	case ModeIdle:
		if len(dogs) == 0 {
			scene = oled.SplashScene{Message: "ADD A DOG", Now: now}
		} else {
			scene = oled.DogSelectorScene{Dog: dogs[sel], Index: sel, Total: len(dogs), Now: now}
		}
	case ModeLockedSummary:
		var entries []oled.SummaryEntry
		for _, d := range dogs {
			entries = append(entries, oled.SummaryEntry{
				DogName:  d.Name,
				Score:    mealSession[d.ID],
				HasSnack: false, // populated by future snack-during-lock handling
			})
		}
		until := time.Time{}
		if lock.Until != nil {
			until = *lock.Until
		}
		scene = oled.LockedSummaryScene{Entries: entries, LockedUntil: until, Now: now}
	case ModeSnackMode:
		var dog domain.Dog
		if len(dogs) > 0 {
			dog = dogs[sel]
		}
		var already []int64
		for id := range snackSession {
			already = append(already, id)
		}
		remaining := time.Duration(0)
		idleAt := m.lastInteract.Add(m.d.Cfg.SnackIdle())
		if !idleAt.Before(now) {
			remaining = idleAt.Sub(now)
		}
		scene = oled.SnackModeScene{Dog: dog, Remaining: remaining, AlreadyRecorded: already}
	}
	if err := m.d.OLED.Render(scene); err != nil {
		m.d.Log.Warn("oled render", "err", err)
	}

	// LEDs: solid green when locked, off otherwise. Snack mode: gentle blue pulse.
	var color neopixel.Color
	switch mode {
	case ModeLockedSummary:
		color = neopixel.Color{G: 32}
	case ModeSnackMode:
		// Crude pulse via second-resolution oscillation; refined animator can replace.
		if now.Second()%2 == 0 {
			color = neopixel.Color{B: 24}
		} else {
			color = neopixel.Color{B: 8}
		}
	}
	if err := m.d.LEDs.SetAll(color); err != nil {
		m.d.Log.Warn("led setall", "err", err)
	}
	if err := m.d.LEDs.Show(); err != nil {
		m.d.Log.Warn("led show", "err", err)
	}
}
