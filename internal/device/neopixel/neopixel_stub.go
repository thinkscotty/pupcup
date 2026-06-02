//go:build !linux

package neopixel

import "log/slog"

// Config is unused on stub platforms but kept so callers can share code.
type Config struct {
	Device string
	N      int
}

// New on non-Linux returns a Fake strip so the binary links for laptop dev.
func New(cfg Config, log *slog.Logger) (Strip, error) {
	_ = log
	return NewFake(cfg.N), nil
}
