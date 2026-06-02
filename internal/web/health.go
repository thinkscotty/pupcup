package web

import (
	"encoding/json"
	"net/http"
	"time"
)

// healthResponse is the JSON shape returned by /healthz (build plan §9). It is
// consumed by the systemd watchdog and a future Home Assistant integration.
type healthResponse struct {
	OK              bool       `json:"ok"`
	Version         string     `json:"version"`
	UptimeS         int64      `json:"uptime_s"`
	LastButtonEvent *time.Time `json:"last_button_event"`
	DBSizeBytes     int64      `json:"db_size_bytes"`
	DeviceLocked    bool       `json:"device_locked"`
	LockedUntil     *time.Time `json:"locked_until"`
}

// handleHealthz reports liveness and a snapshot of device state. It derives
// everything from the store on demand (no event-bus subscription). A failure
// to read any individual field is logged and degrades that field rather than
// failing the whole probe — the probe stays "ok" as long as the process serves.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	now := s.clk.Now()
	resp := healthResponse{
		OK:      true,
		Version: s.version,
		UptimeS: int64(now.Sub(s.startedAt).Seconds()),
	}

	if n, err := s.store.SizeBytes(); err != nil {
		s.log.Warn("healthz db size", "err", err)
	} else {
		resp.DBSizeBytes = n
	}

	if ts, err := s.store.LastButtonEventTime(r.Context()); err != nil {
		s.log.Warn("healthz last button event", "err", err)
	} else {
		resp.LastButtonEvent = ts
	}

	if lock, err := s.store.GetDeviceLock(r.Context()); err != nil {
		s.log.Warn("healthz device lock", "err", err)
	} else {
		resp.DeviceLocked = lock.IsLocked(now)
		resp.LockedUntil = lock.Until
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(resp); err != nil {
		s.log.Error("healthz encode", "err", err)
	}
}
