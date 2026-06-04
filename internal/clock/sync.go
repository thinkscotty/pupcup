package clock

// Synchronizer reports whether the system clock is disciplined to an external
// time source (NTP). The device uses it to flag feedings recorded before the
// clock synced: with no RTC, a cold boot while offline leaves the clock wrong
// until NTP catches up, so those timestamps are guesses worth a human glance.
type Synchronizer interface {
	Synced() bool
}

// FakeSync is a fixed-answer Synchronizer for tests.
type FakeSync struct{ Value bool }

func (f FakeSync) Synced() bool { return f.Value }
