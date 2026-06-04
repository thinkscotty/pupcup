// Package domain defines the core PupCup types and the events flowing on the
// in-process bus. This package has no dependencies outside the standard
// library and is safe to import from every layer.
package domain

import (
	"fmt"
	"time"
)

// Score is the eating-quality outcome of a meal feeding.
type Score string

const (
	ScoreFull    Score = "full"
	ScorePartial Score = "partial"
	ScoreNone    Score = "none"
)

func (s Score) Valid() bool {
	switch s {
	case ScoreFull, ScorePartial, ScoreNone:
		return true
	}
	return false
}

// FeedKind distinguishes a standard kibble meal from a one-off (e.g. a vet
// elimination diet day, a bland-food day during illness).
type FeedKind string

const (
	FeedStandard    FeedKind = "standard"
	FeedNonstandard FeedKind = "nonstandard"
)

func (k FeedKind) Valid() bool {
	switch k {
	case FeedStandard, FeedNonstandard:
		return true
	}
	return false
}

// ButtonColor identifies one of the four physical buttons on the device.
type ButtonColor string

const (
	BtnGreen  ButtonColor = "green"
	BtnYellow ButtonColor = "yellow"
	BtnRed    ButtonColor = "red"
	BtnBlue   ButtonColor = "blue"
)

func (b ButtonColor) Valid() bool {
	switch b {
	case BtnGreen, BtnYellow, BtnRed, BtnBlue:
		return true
	}
	return false
}

// MealScore returns the score implied by a meal-button color, ok=false for blue.
func (b ButtonColor) MealScore() (Score, bool) {
	switch b {
	case BtnGreen:
		return ScoreFull, true
	case BtnYellow:
		return ScorePartial, true
	case BtnRed:
		return ScoreNone, true
	}
	return "", false
}

// Source identifies what created an entry: physical device or web app.
type Source string

const (
	SourceButton Source = "button"
	SourceWeb    Source = "web"
)

func (s Source) Valid() bool {
	switch s {
	case SourceButton, SourceWeb:
		return true
	}
	return false
}

// Dog is a household pet whose feedings we track.
type Dog struct {
	ID          int64
	Name        string
	AccentColor string // hex including '#'
	PhotoPath   string // relative to PhotoDir; empty if none
	SortOrder   int
	CreatedAt   time.Time
}

// FeedTag is one entry in the household's add-in catalog (e.g. "Shredded
// Chicken"). The reserved IsUnspecified sentinel is the device "Other (name
// later)" choice; archived tags are hidden from pickers but preserved on past
// feedings.
type FeedTag struct {
	ID            int64
	Name          string
	IsUnspecified bool
	ArchivedAt    *time.Time
	CreatedAt     time.Time
}

// Feeding is one meal recording for a dog. Tags are the add-ins mixed into the
// meal; it carries zero or more.
type Feeding struct {
	ID        int64
	DogID     int64
	TS        time.Time // UTC
	Kind      FeedKind
	Score     Score
	Specifics string
	Source    Source
	// TimeUnverified marks a feeding the device recorded before its system clock
	// had synced (no RTC; NTP not yet reached). Its TS is a best guess; the web
	// UI flags it so a human can confirm or correct the time, which clears this.
	TimeUnverified bool
	Tags           []FeedTag
	DeletedAt      *time.Time
	EditedAt       *time.Time
	CreatedAt      time.Time
}

// Snack is a between-meals treat recording for a dog.
type Snack struct {
	ID        int64
	DogID     int64
	TS        time.Time // UTC
	Specifics string
	Source    Source
	DeletedAt *time.Time
	EditedAt  *time.Time
	CreatedAt time.Time
}

// IllnessEvent is a date-range marker that a dog was sick. End nil = ongoing.
type IllnessEvent struct {
	ID        int64
	DogID     int64
	Start     time.Time
	End       *time.Time
	Notes     string
	CreatedAt time.Time
}

// StressEvent is a date-range marker for a dog or whole household stressor.
// DogID nil = whole household.
type StressEvent struct {
	ID        int64
	DogID     *int64
	Start     time.Time
	End       *time.Time
	Kind      string
	Notes     string
	CreatedAt time.Time
}

// DeviceLock describes the device's locked state, persisted across restarts.
type DeviceLock struct {
	Until  *time.Time
	Reason string
}

// IsLocked returns whether the lock is active at the given moment.
func (d DeviceLock) IsLocked(now time.Time) bool {
	return d.Until != nil && now.Before(*d.Until)
}

// Bus events
//
// All events implement Event (a marker). Subscribers type-switch on the
// concrete type. Events are pure data — no methods on the concrete events.

type Event interface{ isEvent() }

type FeedRecorded struct {
	Feeding Feeding
	Dog     Dog
}

func (FeedRecorded) isEvent() {}

type SnackRecorded struct {
	Snack Snack
	Dog   Dog
}

func (SnackRecorded) isEvent() {}

type LockChanged struct {
	Lock DeviceLock
	At   time.Time
}

func (LockChanged) isEvent() {}

type EntryEdited struct {
	Kind string // "feeding" | "snack" | "illness" | "stress" | "dog"
	ID   int64
}

func (EntryEdited) isEvent() {}

type EntryDeleted struct {
	Kind string
	ID   int64
}

func (EntryDeleted) isEvent() {}

// Validate returns an error if the dog is missing required fields.
func (d Dog) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("dog name is required")
	}
	if d.AccentColor == "" {
		return fmt.Errorf("dog accent_color is required")
	}
	return nil
}

// Validate returns an error if the feeding is malformed.
func (f Feeding) Validate() error {
	if f.DogID == 0 {
		return fmt.Errorf("feeding dog_id is required")
	}
	if f.TS.IsZero() {
		return fmt.Errorf("feeding ts is required")
	}
	if !f.Score.Valid() {
		return fmt.Errorf("feeding score %q invalid", f.Score)
	}
	if !f.Kind.Valid() {
		return fmt.Errorf("feeding kind %q invalid", f.Kind)
	}
	if !f.Source.Valid() {
		return fmt.Errorf("feeding source %q invalid", f.Source)
	}
	return nil
}

// Validate returns an error if the tag is missing a name.
func (t FeedTag) Validate() error {
	if t.Name == "" {
		return fmt.Errorf("tag name is required")
	}
	return nil
}

// Validate returns an error if the snack is malformed.
func (s Snack) Validate() error {
	if s.DogID == 0 {
		return fmt.Errorf("snack dog_id is required")
	}
	if s.TS.IsZero() {
		return fmt.Errorf("snack ts is required")
	}
	if !s.Source.Valid() {
		return fmt.Errorf("snack source %q invalid", s.Source)
	}
	return nil
}
