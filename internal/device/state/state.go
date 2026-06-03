// Package state implements the device state machine: it reads hardware
// events from the buttons + rotary drivers, drives the OLED + NeoPixel
// outputs, persists state via the SQLite store, and publishes domain events
// on the in-process bus.
//
// Modes:
//
//	Idle           - Rotary scrolls dogs; meal buttons record a feeding for
//	                 the selected dog and advance to the next.
//	LockedSummary  - The current meal is complete: either every dog has a
//	                 feeding, or the meal-complete grace timer elapsed with a
//	                 partial session. Meal buttons are ignored; LED bar is
//	                 solid green; the lock expires meal_lock after the last
//	                 recorded meal. Long-press rotary SW clears the lock;
//	                 long-press blue enters SnackMode.
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
	ModeAddInSelect
)

func (m Mode) String() string {
	switch m {
	case ModeIdle:
		return "idle"
	case ModeLockedSummary:
		return "locked"
	case ModeSnackMode:
		return "snack"
	case ModeAddInSelect:
		return "addin"
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
	// lastMealTS is the timestamp of the most recent feeding recorded in the
	// current meal session. The post-meal lock expiry is timed from it
	// (locked_until = lastMealTS + meal_lock) and the meal-complete grace timer
	// measures elapsed time from it. Zero when no meal is in progress.
	lastMealTS time.Time

	// held is the live set of currently-pressed buttons, tracked from the
	// both-edge driver. It disambiguates the add-in chord (a meal button still
	// held when Blue is tapped — or Blue still held when a meal is tapped) from
	// Blue-alone (snack), and gates the deferred meal commit on release.
	held map[domain.ButtonColor]bool
	// pending is the in-memory, not-yet-committed feeding opened on a meal-button
	// press in Idle (deferred commit). It commits on release (plain meal) or on
	// an add-in selection (with the chosen tag). nil when no meal is in progress.
	pending *pendingFeeding
	// addInChoices / addInIndex drive the AddInSelect picker (the ranked tags
	// plus a trailing "Other (name later)" row).
	addInChoices []oled.AddInChoice
	addInIndex   int
	// bluePressedAt timestamps Blue's press while in LockedSummary, so the tick
	// loop can detect a long-press (≥ long_press_ms) and enter SnackMode.
	bluePressedAt time.Time
	// blueArmed is set when Blue is pressed alone in Idle (no meal held): the
	// gesture is a *potential* snack, confirmed on release. It is disarmed if a
	// chord forms (a meal joins the hold) or the hold is otherwise consumed, so
	// a snack-recording Blue tap in SnackMode doesn't re-enter snack on release.
	blueArmed bool

	// refresh carries a non-blocking signal (from NotifyDogsChanged, called by
	// the web layer after a dog is created/edited/deleted) telling the run loop
	// to reload the dog list. Reloading on the run goroutine — rather than from
	// the caller's — keeps all OLED/LED rendering single-threaded.
	refresh chan struct{}
}

// pendingFeeding is an in-memory meal opened on a meal-button press, awaiting
// commit on release (plain) or after an add-in selection (tagged).
type pendingFeeding struct {
	dogID int64
	dog   domain.Dog
	score domain.Score
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
		held:         map[domain.ButtonColor]bool{},
		refresh:      make(chan struct{}, 1),
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

		case <-m.refresh:
			m.reloadDogs(ctx)

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

// NotifyDogsChanged asks the run loop to reload the dog list after a web edit
// creates/updates/deletes a dog. Non-blocking and safe to call from any
// goroutine — including before Run starts: the buffered, coalesced signal is
// applied on the next loop iteration. The reload itself happens on the run
// goroutine (see reloadDogs) so it never races the loop's own rendering.
func (m *Machine) NotifyDogsChanged() {
	select {
	case m.refresh <- struct{}{}:
	default: // a reload is already pending; coalesce
	}
}

// reloadDogs re-reads the dog list and re-renders. Called only from the run
// loop (via the refresh channel), keeping OLED/LED I/O single-threaded.
func (m *Machine) reloadDogs(ctx context.Context) {
	dogs, err := m.d.Store.ListDogs(ctx)
	if err != nil {
		m.d.Log.Warn("reload dogs", "err", err)
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
	// Track the live held-button set from the both-edge driver before dispatch:
	// a press is recorded immediately; a release is cleared *before* the handler
	// runs so it sees which other buttons remain held.
	m.mu.Lock()
	m.lastInteract = m.d.Clk.Now()
	switch ev.Action {
	case buttons.ActionPress:
		m.held[ev.Color] = true
	case buttons.ActionRelease:
		delete(m.held, ev.Color)
	}
	mode := m.mode
	m.mu.Unlock()

	switch mode {
	case ModeIdle:
		m.handleIdleButton(ctx, ev)
	case ModeLockedSummary:
		m.handleLockedButton(ctx, ev)
	case ModeSnackMode:
		m.handleSnackButton(ctx, ev)
	case ModeAddInSelect:
		// The chord's trailing edges (the meal-button and Blue releases) land
		// here once the picker is open; swallow them. Selection is by rotary.
	}
}

// handleIdleButton implements the deferred-commit meal flow and the add-in
// chord (build plan §6.5). A meal-button press opens an in-memory pending
// feeding for the selected dog; release commits it as a plain meal (unless a
// chord opened the picker). Blue alone (no meal held during its hold) is the
// snack button; Blue together with a meal — pressed in either order — opens
// the add-in picker.
func (m *Machine) handleIdleButton(ctx context.Context, ev buttons.ButtonEvent) {
	if ev.Action == buttons.ActionPress {
		if ev.Color == domain.BtnBlue {
			// Reverse-chord trigger: if a meal button is already held, open the
			// picker for the pending feeding. Otherwise arm a potential snack —
			// confirmed on release if no meal joins the hold.
			m.mu.Lock()
			chord := m.mealHeldLocked() && m.pending != nil
			if !chord {
				m.blueArmed = true
			}
			m.mu.Unlock()
			if chord {
				m.enterAddInSelect(ctx)
			}
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
		// Open or update the pending feeding (last-wins on overlapping meals).
		m.pending = &pendingFeeding{dogID: dog.ID, dog: dog, score: score}
		blueHeld := m.held[domain.BtnBlue]
		if blueHeld {
			m.blueArmed = false // a meal joined the Blue hold → it's a chord, not a snack
		}
		m.mu.Unlock()
		// Forward chord: Blue is already held → open the picker.
		if blueHeld {
			m.enterAddInSelect(ctx)
		}
		return
	}

	// Release.
	if ev.Color == domain.BtnBlue {
		m.mu.Lock()
		armed := m.blueArmed
		m.blueArmed = false
		m.mu.Unlock()
		if armed {
			m.enterSnackMode(ctx, ModeIdle)
		}
		return
	}
	if _, ok := ev.Color.MealScore(); !ok {
		return
	}
	m.mu.Lock()
	blueHeld := m.held[domain.BtnBlue]
	stillHeld := m.mealHeldLocked()
	hasPending := m.pending != nil
	m.mu.Unlock()
	// Commit only when no Blue is involved and the last meal button is released
	// (last-wins: the pending already carries the most-recently-pressed score).
	if blueHeld || stillHeld || !hasPending {
		return
	}
	m.commitPending(ctx, ev.TS, nil)
}

func (m *Machine) handleLockedButton(ctx context.Context, ev buttons.ButtonEvent) {
	// Meal buttons are ignored. Blue enters SnackMode only on a long-press,
	// detected on the tick loop; here we just timestamp its press so the tick
	// can measure the hold (build plan §6.5, resolution 3).
	if ev.Color != domain.BtnBlue {
		return
	}
	m.mu.Lock()
	if ev.Action == buttons.ActionPress {
		m.bluePressedAt = ev.TS
	} else {
		m.bluePressedAt = time.Time{}
	}
	m.mu.Unlock()
}

func (m *Machine) handleSnackButton(ctx context.Context, ev buttons.ButtonEvent) {
	if ev.Color != domain.BtnBlue {
		return
	}
	// Snacks record on the Blue press; the release is a no-op. (A long-press
	// from LockedSummary consumes no press here — only its trailing release
	// arrives — so it never records a phantom snack.)
	if ev.Action != buttons.ActionPress {
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
	mealHeld := m.mealHeldLocked()
	m.mu.Unlock()

	if mode == ModeAddInSelect {
		switch ev.Kind {
		case rotary.RotateCW:
			m.moveAddInIndex(+1)
			m.render(ctx)
		case rotary.RotateCCW:
			m.moveAddInIndex(-1)
			m.render(ctx)
		case rotary.PressShort:
			m.selectAddIn(ctx)
		case rotary.PressLong:
			// Escape hatch: commit the pending feeding untagged.
			m.commitPendingUntagged(ctx)
		}
		return
	}

	switch ev.Kind {
	case rotary.RotateCW:
		if mealHeld { // rotary ignored while a meal button is held (resolution 4a)
			return
		}
		m.adjustSelection(+1)
		m.render(ctx)
	case rotary.RotateCCW:
		if mealHeld {
			return
		}
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

// mealHeldLocked reports whether any meal button (G/Y/R) is currently held.
// Caller holds m.mu.
func (m *Machine) mealHeldLocked() bool {
	return m.held[domain.BtnGreen] || m.held[domain.BtnYellow] || m.held[domain.BtnRed]
}

// ----------------------------- add-in picker --------------------------------

// enterAddInSelect opens the per-dog-ranked add-in picker for the pending
// feeding, with a trailing "Other (name later)" row. No-op if no pending.
func (m *Machine) enterAddInSelect(ctx context.Context) {
	m.mu.Lock()
	p := m.pending
	if p == nil {
		m.mu.Unlock()
		return
	}
	dog := p.dog
	m.mu.Unlock()

	ranked, err := m.d.Store.TagsForDog(ctx, dog.ID)
	if err != nil {
		m.d.Log.Warn("addin tags for dog", "dog", dog.ID, "err", err)
	}
	choices := make([]oled.AddInChoice, 0, len(ranked)+1)
	for _, rt := range ranked {
		choices = append(choices, oled.AddInChoice{TagID: rt.ID, Label: rt.Name})
	}
	choices = append(choices, oled.AddInChoice{Label: "Other (name later)", IsOther: true})

	m.mu.Lock()
	m.mode = ModeAddInSelect
	m.addInChoices = choices
	m.addInIndex = 0
	m.blueArmed = false
	m.lastInteract = m.d.Clk.Now()
	m.mu.Unlock()
	m.d.Log.Info("addin picker opened", "dog", dog.Name, "choices", len(choices))
	m.render(ctx)
}

// moveAddInIndex scrolls the picker selection, wrapping at the ends.
func (m *Machine) moveAddInIndex(delta int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.addInChoices)
	if n == 0 {
		return
	}
	m.addInIndex = (m.addInIndex + delta + n) % n
}

// selectAddIn commits the pending feeding with the highlighted choice: a real
// tag, or the reserved Unspecified sentinel for "Other".
func (m *Machine) selectAddIn(ctx context.Context) {
	m.mu.Lock()
	if m.addInIndex < 0 || m.addInIndex >= len(m.addInChoices) {
		m.mu.Unlock()
		return
	}
	choice := m.addInChoices[m.addInIndex]
	m.mu.Unlock()

	tagID := choice.TagID
	if choice.IsOther {
		tagID = store.UnspecifiedTagID
	}
	m.commitPending(ctx, m.d.Clk.Now(), &tagID)
}

// commitPendingUntagged commits the pending feeding as a plain meal (no tag),
// used by the AddInSelect walk-away timeout and the rotary-long escape, so a
// walk-away never loses the meal record.
func (m *Machine) commitPendingUntagged(ctx context.Context) {
	m.commitPending(ctx, m.d.Clk.Now(), nil)
}

// commitPending writes the pending feeding (optionally with one add-in tag),
// clears the pending + picker, returns to Idle, then advances to the next
// un-fed dog and re-checks the all-fed lock condition.
func (m *Machine) commitPending(ctx context.Context, ts time.Time, tagID *int64) {
	m.mu.Lock()
	p := m.pending
	m.pending = nil
	m.addInChoices = nil
	m.addInIndex = 0
	if m.mode == ModeAddInSelect {
		m.mode = ModeIdle
	}
	m.mu.Unlock()
	if p == nil {
		return
	}
	if err := m.recordFeeding(ctx, p.dogID, p.score, domain.SourceButton, ts, tagID); err != nil {
		m.d.Log.Error("record feeding", "err", err, "dog", p.dogID)
		return
	}
	m.afterMealCommit(ctx)
}

// afterMealCommit advances to the next un-fed dog and locks if every dog now
// has a feeding in this session.
func (m *Machine) afterMealCommit(ctx context.Context) {
	m.mu.Lock()
	m.advanceToUnfedDog()
	allFed := len(m.mealSession) >= len(m.dogs) && len(m.dogs) > 0
	m.mu.Unlock()
	if allFed {
		m.transitionToLocked(ctx, "meal complete")
		return
	}
	m.render(ctx)
}

func (m *Machine) onTick(ctx context.Context) {
	m.mu.Lock()
	now := m.d.Clk.Now()
	mode := m.mode
	expired := mode == ModeLockedSummary && m.lock.Until != nil && !now.Before(*m.lock.Until)
	idleSnack := mode == ModeSnackMode && now.Sub(m.lastInteract) >= m.d.Cfg.SnackIdle()
	// Meal-complete grace: a meal is in progress (at least one dog fed) but not
	// every dog has been fed, and the grace window has elapsed since the last
	// recorded meal. Lock with the partial session; the un-fed dog(s) get no
	// record and are added retroactively on the web. With grace = 0 this fires
	// on the first tick after a non-completing feeding (lock immediately).
	graceLock := mode == ModeIdle &&
		len(m.mealSession) > 0 &&
		len(m.mealSession) < len(m.dogs) &&
		!m.lastMealTS.IsZero() &&
		now.Sub(m.lastMealTS) >= m.d.Cfg.MealCompleteGrace()
	// Add-in walk-away: the picker has been idle past addin_idle_seconds → commit
	// the pending feeding untagged so the meal is never lost.
	addInIdle := mode == ModeAddInSelect &&
		now.Sub(m.lastInteract) >= m.d.Cfg.AddInIdle()
	// Blue long-press in LockedSummary → enter SnackMode (detected on the tick).
	blueLong := mode == ModeLockedSummary &&
		m.held[domain.BtnBlue] &&
		!m.bluePressedAt.IsZero() &&
		now.Sub(m.bluePressedAt) >= m.d.Cfg.LongPress()
	m.mu.Unlock()

	switch {
	case expired:
		m.clearLock(ctx, "expired")
	case blueLong:
		m.enterSnackMode(ctx, ModeLockedSummary)
	case addInIdle:
		m.commitPendingUntagged(ctx)
	case graceLock:
		m.transitionToLocked(ctx, "meal grace timeout")
	case idleSnack:
		m.exitSnackMode(ctx)
	default:
		// Re-render once a second so countdown and clock stay current.
		m.render(ctx)
	}
}

// ----------------------------- transitions ----------------------------------

// transitionToLocked enters LockedSummary. The lock expiry is timed from the
// last recorded meal (locked_until = lastMealTS + meal_lock), not from "now",
// so the window is the same whether the meal completed by all-fed or by the
// grace timeout. reason is persisted and logged ("meal complete" when every dog
// was fed, "meal grace timeout" when the grace timer locked a partial session).
func (m *Machine) transitionToLocked(ctx context.Context, reason string) {
	m.mu.Lock()
	base := m.lastMealTS
	m.mu.Unlock()
	if base.IsZero() {
		base = m.d.Clk.Now()
	}
	until := base.Add(m.d.Cfg.MealLock())
	lock := domain.DeviceLock{Until: &until, Reason: reason}
	if err := m.d.Store.SetDeviceLock(ctx, lock); err != nil {
		m.d.Log.Error("persist lock", "err", err)
	}
	m.mu.Lock()
	m.mode = ModeLockedSummary
	m.lock = lock
	fed, total := len(m.mealSession), len(m.dogs)
	m.mu.Unlock()
	m.d.Bus.Publish(domain.LockChanged{Lock: lock, At: m.d.Clk.Now()})
	m.d.Log.Info("locked summary entered", "until", until, "reason", reason, "fed", fed, "dogs", total)
	m.render(ctx)
}

func (m *Machine) clearLock(ctx context.Context, reason string) {
	if err := m.d.Store.SetDeviceLock(ctx, domain.DeviceLock{}); err != nil {
		m.d.Log.Error("clear lock", "err", err)
	}
	m.mu.Lock()
	m.mode = ModeIdle
	m.lock = domain.DeviceLock{}
	m.lastMealTS = time.Time{}
	m.pending = nil
	m.addInChoices = nil
	m.addInIndex = 0
	m.bluePressedAt = time.Time{}
	m.blueArmed = false
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
	m.pending = nil
	m.bluePressedAt = time.Time{}
	m.blueArmed = false
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

func (m *Machine) recordFeeding(ctx context.Context, dogID int64, score domain.Score, source domain.Source, ts time.Time, tagID *int64) error {
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
	// Attach the chosen add-in (if any) and reload so the published event
	// carries it. A tag failure must not lose the meal — log and continue.
	if tagID != nil {
		if err := m.d.Store.AttachTag(ctx, f.ID, *tagID); err != nil {
			m.d.Log.Warn("attach add-in tag", "feeding", f.ID, "tag", *tagID, "err", err)
		} else if reloaded, gerr := m.d.Store.GetFeeding(ctx, f.ID); gerr == nil {
			f = reloaded
		}
	}
	m.mu.Lock()
	m.mealSession[dogID] = score
	m.lastMealTS = ts
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
	lastInteract := m.lastInteract
	pending := m.pending
	addInChoices := make([]oled.AddInChoice, len(m.addInChoices))
	copy(addInChoices, m.addInChoices)
	addInIndex := m.addInIndex
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
		// Mark any dog that also received a snack since this lock began (the
		// `*` marker, SummaryEntry.HasSnack). Queried fresh each render so a
		// snack added from the web during the lock also shows. The lock began
		// at locked_until - meal_lock.
		snacked := map[int64]bool{}
		if lock.Until != nil {
			since := lock.Until.Add(-m.d.Cfg.MealLock())
			if sn, err := m.d.Store.ListSnacks(ctx, store.SnackFilter{Since: since}); err != nil {
				m.d.Log.Warn("summary snacks", "err", err)
			} else {
				for _, s := range sn {
					snacked[s.DogID] = true
				}
			}
		}
		var entries []oled.SummaryEntry
		for _, d := range dogs {
			entries = append(entries, oled.SummaryEntry{
				DogName:  d.Name,
				Score:    mealSession[d.ID],
				HasSnack: snacked[d.ID],
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
		idleAt := lastInteract.Add(m.d.Cfg.SnackIdle())
		if !idleAt.Before(now) {
			remaining = idleAt.Sub(now)
		}
		scene = oled.SnackModeScene{Dog: dog, Remaining: remaining, AlreadyRecorded: already}
	case ModeAddInSelect:
		var dog domain.Dog
		var score domain.Score
		if pending != nil {
			dog = pending.dog
			score = pending.score
		}
		scene = oled.AddInSelectScene{Dog: dog, Score: score, Choices: addInChoices, Index: addInIndex}
	}
	if err := m.d.OLED.Render(scene); err != nil {
		m.d.Log.Warn("oled render", "err", err)
	}

	// LEDs: the locked summary shows each dog's meal quality as a colored
	// segment across the bar (see summaryFrame); snack mode is a gentle blue
	// pulse; the add-in picker is steady amber; everything else is dark.
	frame := make([]neopixel.Color, m.d.LEDs.N())
	switch mode {
	case ModeLockedSummary:
		scores := make([]neopixel.Color, len(dogs))
		for i, d := range dogs {
			scores[i] = scoreColor(mealSession[d.ID])
		}
		if seg := summaryFrame(len(frame), scores); seg != nil {
			frame = seg
		} else {
			// 4+ dogs (or a bar that isn't 8 pixels): no defined segment
			// layout, so fall back to the original solid green = "fed".
			fillFrame(frame, ledFull)
		}
	case ModeSnackMode:
		// Crude pulse via second-resolution oscillation; refined animator can replace.
		if now.Second()%2 == 0 {
			fillFrame(frame, neopixel.Color{B: 24})
		} else {
			fillFrame(frame, neopixel.Color{B: 8})
		}
	case ModeAddInSelect:
		// Steady amber while the add-in picker is open.
		fillFrame(frame, neopixel.Color{R: 28, G: 16})
	}
	for i, c := range frame {
		if err := m.d.LEDs.SetPixel(i, c); err != nil {
			m.d.Log.Warn("led setpixel", "i", i, "err", err)
			break
		}
	}
	if err := m.d.LEDs.Show(); err != nil {
		m.d.Log.Warn("led show", "err", err)
	}
}

// LED colors for the locked meal summary, tuned dim to sit alongside the
// existing green (adjust on-device to taste): green = ate all, yellow = ate
// some, red = refused. An un-fed dog (empty score) renders dark.
var (
	ledFull    = neopixel.Color{G: 32}
	ledPartial = neopixel.Color{R: 40, G: 24}
	ledNone    = neopixel.Color{R: 40}
)

// scoreColor maps a meal score to its LED segment color. An empty/unknown
// score — a dog that never got a button press before a partial-meal lock —
// renders dark.
func scoreColor(s domain.Score) neopixel.Color {
	switch s {
	case domain.ScoreFull:
		return ledFull
	case domain.ScorePartial:
		return ledPartial
	case domain.ScoreNone:
		return ledNone
	default:
		return neopixel.ColorOff
	}
}

// fillFrame paints every pixel of frame the same color.
func fillFrame(frame []neopixel.Color, c neopixel.Color) {
	for i := range frame {
		frame[i] = c
	}
}

// summaryFrame lays out the per-dog meal-quality segments on an 8-pixel bar,
// one color per dog in sort order:
//
//	1 dog : [0..7]
//	2 dogs: [0..3] [4..7]
//	3 dogs: [0,1] (2 dark) [3,4] (5 dark) [6,7]
//
// It returns nil when the (pixelCount, dogCount) pair has no defined layout
// (4+ dogs, or a bar that isn't 8 pixels), signaling the caller to fall back
// to a solid fill.
func summaryFrame(pixelCount int, scores []neopixel.Color) []neopixel.Color {
	if pixelCount != 8 {
		return nil
	}
	frame := make([]neopixel.Color, 8)
	switch len(scores) {
	case 1:
		fillFrame(frame, scores[0])
	case 2:
		frame[0], frame[1], frame[2], frame[3] = scores[0], scores[0], scores[0], scores[0]
		frame[4], frame[5], frame[6], frame[7] = scores[1], scores[1], scores[1], scores[1]
	case 3:
		frame[0], frame[1] = scores[0], scores[0]
		frame[3], frame[4] = scores[1], scores[1]
		frame[6], frame[7] = scores[2], scores[2]
	default:
		return nil
	}
	return frame
}
