//go:build !linux

package rotary

import "log/slog"

// New on non-Linux returns a Fake driver so the binary links.
func New(cfg Config, log *slog.Logger) (Driver, error) {
	_ = cfg
	_ = log
	return NewFake(), nil
}
