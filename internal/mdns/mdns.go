// Package mdns advertises the PupCup web app on the local network via
// multicast DNS (Bonjour/Avahi), so a phone on the home wifi can reach it at
// http://<hostname>.local without knowing the Pi's IP. It wraps
// grandcat/zeroconf and is a soft dependency: if registration fails (common on
// a dev laptop with no multicast permission), it logs a warning and the daemon
// keeps running — the dashboard prints the raw IP as a fallback.
package mdns

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"

	"github.com/grandcat/zeroconf"
)

// service is the DNS-SD service type advertised. PupCup serves plain HTTP.
const service = "_http._tcp"

// Advertiser holds the parameters of a single mDNS service registration.
type Advertiser struct {
	instance string
	port     int
	txt      []string
	log      *slog.Logger
}

// New builds an Advertiser. instance is the advertised hostname/instance name
// (e.g. "pupcup", resolvable as pupcup.local), port is the HTTP port, and
// version is published as a TXT record.
func New(instance string, port int, version string, log *slog.Logger) *Advertiser {
	if log == nil {
		log = slog.Default()
	}
	return &Advertiser{
		instance: instance,
		port:     port,
		txt:      []string{"version=" + version},
		log:      log.With("component", "mdns"),
	}
}

// Run registers the service and blocks until ctx is cancelled, then withdraws
// the advertisement. A registration failure is logged (not returned) so mDNS
// trouble never takes down the daemon; Run then blocks on ctx so callers can
// treat it uniformly.
func (a *Advertiser) Run(ctx context.Context) error {
	server, err := zeroconf.Register(a.instance, service, "local.", a.port, a.txt, nil)
	if err != nil {
		a.log.Warn("mDNS registration failed; continuing without it", "err", err)
		<-ctx.Done()
		return nil
	}
	a.log.Info("advertising mDNS service",
		"instance", a.instance, "service", service, "port", a.port)
	<-ctx.Done()
	server.Shutdown()
	return nil
}

// PortFromListen extracts the numeric port from a net listen address such as
// ":80" or "0.0.0.0:8080".
func PortFromListen(listen string) (int, error) {
	// net.SplitHostPort needs a host part; ":80" already has one (empty host).
	_, portStr, err := net.SplitHostPort(listen)
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, fmt.Errorf("parse port %q: %w", portStr, err)
	}
	return port, nil
}
