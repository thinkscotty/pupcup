// Package hostinit wraps periph.io host registration so callers don't have
// to import periph.io directly. Init is idempotent.
package hostinit

import (
	"sync"

	"periph.io/x/host/v3"
)

var (
	once sync.Once
	err  error
)

// Init registers periph.io drivers. Safe to call multiple times.
func Init() error {
	once.Do(func() {
		_, err = host.Init()
	})
	return err
}
