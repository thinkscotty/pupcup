# PupCup

A single Go binary for a Raspberry Pi 3B+ that drives a physical button device and serves a local-network web app for tracking dog feedings.

See [pupcup_build_plan.md](pupcup_build_plan.md) for architecture and [pupcup_hardware_build.md](pupcup_hardware_build.md) for the hardware build.

## Install on a Raspberry Pi (one line)

Once the device is wired up, you can provision a fresh Pi end-to-end over SSH —
no Go toolchain, no repo checkout. Works on a **Pi 3, 3B+, 4, or Zero 2 W**
running a 64-bit (or armv7) Raspberry Pi OS.

1. **Flash the OS** with [Raspberry Pi Imager](https://www.raspberrypi.com/software/):
   Raspberry Pi OS Lite (64-bit), and in *Edit Settings* set the hostname,
   enable **SSH** (paste your public key), and configure **wifi**.
2. **SSH in and run the installer**, telling it which display you wired:

   ```sh
   ssh <user>@<hostname>.local
   curl -fsSL https://raw.githubusercontent.com/thinkscotty/pupcup/main/install.sh \
     | sudo bash -s -- --display gc9a01
   ```

The installer enables I²C/SPI and the RTC, installs the matching prebuilt
release binary (checksum-verified), creates the `pupcup` service, and reboots.
When it comes back, the dashboard is at `http://<hostname>.local/`.

Options: `--display gc9a01|oled`, `--timezone <IANA>`, `--hostname <name>`,
`--version <tag>`, `--no-reboot` (run `sudo bash install.sh --help` for all).
Prefer to do it by hand, or want the verification/UAT checklist? See
[docs/initial_pi_flash.md](docs/initial_pi_flash.md). Releases are cut by pushing
a `v*` tag (see [.github/workflows/release.yml](.github/workflows/release.yml)).

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
# window, summary stats, and a history table), /tags to manage the add-in
# catalog (the extras mixed into a meal — tag any meal on /feedings with
# inline chips), or /healthz for the probe.
```

## Cross-compile + deploy to Pi

Deployment tooling lives in [deploy/](deploy/) — see [pupcup_build_plan.md](pupcup_build_plan.md) §10 for details.

**First time, on a fresh Pi** (run as a sudoer — the bring-up admin account, not the `nologin` `pupcup` service user):

```sh
# on the Pi, from a checkout of this repo
./deploy/bootstrap.sh        # creates the pupcup user, dirs, installs the config + systemd unit, enables the service
sudo nano /etc/pupcup/config.yaml   # adjust timezone etc. if the defaults don't fit
```

**Every update, from your laptop:**

```sh
./deploy/deploy.sh                              # cross-compiles arm64, ships, sudo-installs, restarts
TARGET=scotty@raspberrypi.local ./deploy/deploy.sh   # override the SSH host
```

`deploy.sh` builds a stripped, version-stamped static binary
(`GOOS=linux GOARCH=arm64 CGO_ENABLED=0 ... -ldflags "-s -w -X main.version=<sha>"`),
rsyncs it over, installs it root-owned to `/opt/pupcup/pupcup`, and restarts the
`pupcup` systemd service (which runs as the unprivileged `pupcup` user with
gpio/i2c/spi access and the `CAP_NET_BIND_SERVICE` capability to bind `:80`). To
cross-compile by hand without deploying:

```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o build/pupcup ./cmd/pupcup
```

## License

Source code is licensed under the [MIT License](LICENSE). Hardware design
documentation (wiring, BOM, pinout, layouts, bring-up procedures) is licensed
separately under [Creative Commons Attribution 4.0 International (CC BY 4.0)](LICENSE-HARDWARE).
