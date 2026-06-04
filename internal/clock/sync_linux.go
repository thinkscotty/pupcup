//go:build linux

package clock

import "golang.org/x/sys/unix"

// SystemSync queries the kernel's NTP discipline state via adjtimex. After a
// cold boot with no network the clock is unsynchronized (the kernel sets
// STA_UNSYNC); systemd-timesyncd or chrony clears that bit once it has locked
// onto a time source. Implementation-agnostic — works regardless of which NTP
// daemon is running.
type SystemSync struct{}

func (SystemSync) Synced() bool {
	var tx unix.Timex
	if _, err := unix.Adjtimex(&tx); err != nil {
		return false
	}
	return tx.Status&unix.STA_UNSYNC == 0
}
