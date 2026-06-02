//go:build !linux

package buttons

import (
	"log/slog"
	"time"
)

// New on non-Linux platforms returns a Fake driver so the binary still
// links (useful for laptop dev). It never emits events.
func New(pins Pins, debounce time.Duration, log *slog.Logger) (Driver, error) {
	_ = log
	_ = pins
	_ = debounce
	return NewFake(), nil
}
