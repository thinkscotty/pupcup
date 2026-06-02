//go:build !linux

package oled

import "log/slog"

// Config is unused on stub platforms but kept so callers can share code.
type Config struct {
	I2CBus uint8
	Addr   uint16
}

// New on non-Linux returns the Fake renderer so binaries link for laptop dev.
func New(cfg Config, log *slog.Logger) (Renderer, error) {
	_ = cfg
	_ = log
	return NewFake(), nil
}
