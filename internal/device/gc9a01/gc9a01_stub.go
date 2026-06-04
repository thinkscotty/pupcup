//go:build !linux

package gc9a01

import (
	"log/slog"

	"github.com/scottyturner/pupcup/internal/device/display"
)

// New on non-Linux returns a Fake so the binary links for laptop development
// and the hwprobe/daemon build on macOS.
func New(cfg Config, log *slog.Logger) (display.Renderer, error) {
	_ = cfg
	_ = log
	return NewFake(), nil
}
