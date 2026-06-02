// Package systemd is a minimal, dependency-free sd_notify client: it lets the
// daemon signal readiness and pet the watchdog under a Type=notify unit. Every
// function is a safe no-op (nil error) when the process is not running under
// systemd — NOTIFY_SOCKET / WATCHDOG_USEC are unset — so the same binary runs
// unchanged during laptop development. Pure Go, no CGO, no libsystemd.
package systemd

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Notify sends a status line (e.g. "READY=1") to the systemd notify socket.
// No-op with a nil error when NOTIFY_SOCKET is unset.
func Notify(state string) error {
	addr := os.Getenv("NOTIFY_SOCKET")
	if addr == "" {
		return nil
	}
	// A leading '@' denotes a Linux abstract-namespace socket: replace it with
	// the NUL byte the kernel expects.
	if strings.HasPrefix(addr, "@") {
		addr = "\x00" + addr[1:]
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte(state))
	return err
}

// Ready signals that startup is complete (READY=1).
func Ready() error { return Notify("READY=1") }

// Stopping signals that shutdown has begun (STOPPING=1).
func Stopping() error { return Notify("STOPPING=1") }

// watchdogInterval returns the ping period — half of WATCHDOG_USEC per
// sd_watchdog_enabled(3) — or 0 when the watchdog is disabled or addressed to
// a different PID.
func watchdogInterval() time.Duration {
	usec := os.Getenv("WATCHDOG_USEC")
	if usec == "" {
		return 0
	}
	if pid := os.Getenv("WATCHDOG_PID"); pid != "" && pid != strconv.Itoa(os.Getpid()) {
		return 0
	}
	n, err := strconv.ParseInt(usec, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Microsecond / 2
}

// RunWatchdog pings WATCHDOG=1 at half the configured watchdog interval until
// ctx is canceled. It returns immediately when the watchdog is disabled, so it
// is safe (and cheap) to always call as `go systemd.RunWatchdog(ctx)`.
func RunWatchdog(ctx context.Context) {
	d := watchdogInterval()
	if d <= 0 {
		return
	}
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = Notify("WATCHDOG=1")
		}
	}
}
