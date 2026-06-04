//go:build !linux

package clock

// SystemSync on non-Linux (developer machines, CI) assumes the clock is good,
// so feedings created during local development are never flagged unverified.
type SystemSync struct{}

func (SystemSync) Synced() bool { return true }
