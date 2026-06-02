# PupCup

A single Go binary for a Raspberry Pi Zero 2W that drives a physical button device and serves a local-network web app for tracking dog feedings.

See [pupcup_build_plan.md](pupcup_build_plan.md) for architecture and [pupcup_hardware_build.md](pupcup_hardware_build.md) for the hardware build.

## Quick start (laptop)

```sh
go build ./...
go test ./...
# Defaults bind :80 and write to /var/lib/pupcup (needs root); for local dev use
# a high port, a writable DB path, and a writable photo dir. On a laptop the
# hardware drivers are Fakes, so the daemon seeds the household's dogs, shows the
# boot splash, serves the web app, advertises mDNS, and exits cleanly on Ctrl-C.
PUPCUP_LISTEN=:8080 PUPCUP_DB_PATH=./pupcup-dev.sqlite PUPCUP_PHOTO_DIR=./photos go run ./cmd/pupcup
# Visit http://localhost:8080/ for the dashboard (who's been fed today, with
# per-dog quick-add buttons), /feedings to record meals & snacks with a
# retroactive timestamp and edit/delete past entries (HTMX-driven), /illness
# and /stress to log date-range health/stress events (ongoing toggle +
# one-click set-end; stress can be whole-household), /history for the unified
# timeline of every meal/snack/illness/stress (filter by dog, type, and date
# range), /dogs to add/edit dogs (name, accent color, photo), a dog's name for
# its detail page (/dogs/{id} — an eating-quality SVG chart over a 7/30/90-day
# window, summary stats, and a history table), or /healthz for the probe.
```

## Cross-compile + deploy to Pi

> Deployment tooling (`deploy/deploy.sh`, `bootstrap.sh`, `pupcup.service`, `config.example.yaml`) lands in milestone 14 — see [pupcup_build_plan.md](pupcup_build_plan.md) §10. Until then, cross-compile manually:

```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o build/pupcup ./cmd/pupcup
```

## License

Source code is licensed under the [MIT License](LICENSE). Hardware design
documentation (wiring, BOM, pinout, layouts, bring-up procedures) is licensed
separately under [Creative Commons Attribution 4.0 International (CC BY 4.0)](LICENSE-HARDWARE).
