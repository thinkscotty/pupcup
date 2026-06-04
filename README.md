# PupCup 🐾

**A tiny appliance that answers one question: did the dog get fed?**

PupCup is a single Go binary running on a Raspberry Pi that is *both* a physical
push-button device and a local-network web app. Tap a button on the box when you
feed a dog; everyone in the house can see — on the device's screen, on its glowing
status bar, or from any phone or laptop on the wifi — who's eaten today and how
well they ate. Over time it builds a feeding history and surfaces trends, so you
can spot the slow build-up of a picky stretch or correlate poor eating with a
stressful week or an illness.

It runs entirely on your home network. There is no cloud account, no app store,
and nothing leaves the house.

> **Status:** Alpha. The hardware build is complete and proven on a bench unit;
> the full application is built and runs but has not yet had a long soak on real
> hardware. Expect rough edges.

<!-- Drop screenshots/photos here once the enclosure exists:
![The PupCup device](docs/img/device.jpg)
![The dashboard](docs/img/dashboard.png)
-->

---

## Table of contents

- [Why PupCup](#why-pupcup)
- [What it does](#what-it-does)
- [The physical device](#the-physical-device)
- [Install on a Raspberry Pi (one line)](#install-on-a-raspberry-pi-one-line)
- [Using the web app](#using-the-web-app)
- [Run it on your laptop (no hardware needed)](#run-it-on-your-laptop-no-hardware-needed)
- [Build the hardware](#build-the-hardware)
- [Configuration](#configuration)
- [Building from source & deploying](#building-from-source--deploying)
- [Project layout](#project-layout)
- [Documentation](#documentation)
- [License](#license)

---

## Why PupCup

A whiteboard on the fridge works right up until someone forgets to wipe it, or
two people both top up the bowl because neither knew the other already had. PupCup
replaces the whiteboard with something that's:

- **Glanceable** — a colored LED bar and an always-on screen tell you the state
  of the house's dogs without unlocking anything.
- **Honest about history** — every feeding is timestamped and kept, so "is she
  eating less lately?" becomes a chart instead of a hunch.
- **Friction-free to log** — feeding a dog is one button press at the bowl, not a
  phone-app round trip. The phone view is there when you want detail or you're
  not in the kitchen.
- **Yours** — a $70-ish parts list, open hardware docs, and a single static
  binary. It lives on your shelf and your network, not someone's server.

## What it does

1. **Tells the household whether the dogs were fed, and how well they ate.** Each
   meal is rated at the moment you log it: ate fully, ate some, or refused.
2. **Keeps a feeding history over time**, plus optional logs for snacks,
   illnesses, and stressors (vet visits, travel, fireworks, a new houseguest).
3. **Looks for patterns** — per-dog eating-quality charts over 7/30/90-day
   windows, and a unified timeline that lines meals up against the illness and
   stress events you've recorded, so associations are easy to eyeball.

## The physical device

The box is a Raspberry Pi with four colored buttons, a rotary knob, a round
screen, and an 8-pixel LED bar — all on one perfboard, no separate microcontroller.

**Logging a meal at the bowl:**

1. Turn the **rotary knob** to pick which dog you're feeding (the screen shows
   who's selected).
2. Press the button that matches how they ate:

   | Button | Meaning |
   |---|---|
   | 🟢 **Green** | Ate the whole meal |
   | 🟡 **Yellow** | Ate some of it |
   | 🔴 **Red** | Wouldn't eat |
   | 🔵 **Blue** | A snack/treat (not a meal) |

3. The **LED bar** lights that dog's pixel in the color you pressed, so the most
   recent state is visible across the room. After everyone's eaten, the device
   shows a **locked summary** and the bar reflects each dog's meal quality.

A long-press on the rotary knob overrides the post-meal lock if you need to
correct an entry. None of this requires the web app — but every press shows up
there instantly, and anything logged on the web shows up on the device.

See [docs/pupcup_hardware_build.md](docs/pupcup_hardware_build.md) for the full
build, including the round **GC9A01** LCD (default) and a cheaper **SSD1306 OLED**
alternative.

## Install on a Raspberry Pi (one line)

Once your device is wired up (or even just a bare Pi, if you only want the web
app), you can provision it end-to-end over SSH — no Go toolchain, no repo
checkout. Works on a **Pi 3, 3B+, 4, or Zero 2 W** running 64-bit (or armv7)
Raspberry Pi OS.

1. **Flash the OS** with [Raspberry Pi Imager](https://www.raspberrypi.com/software/):
   pick *Raspberry Pi OS Lite (64-bit)*, then in **Edit Settings** set the
   hostname, enable **SSH** (paste your public key), and configure **wifi**.

2. **SSH in and run the installer**, telling it which display you wired:

   ```sh
   ssh <user>@<hostname>.local
   curl -fsSL https://raw.githubusercontent.com/thinkscotty/pupcup/main/install.sh \
     | sudo bash -s -- --display gc9a01
   ```

   Prefer to read before you pipe to root? Download it first:

   ```sh
   curl -fsSLO https://raw.githubusercontent.com/thinkscotty/pupcup/main/install.sh
   less install.sh
   sudo bash install.sh --display gc9a01
   ```

The installer enables the SPI buses (plus I²C on the OLED variant), downloads the
prebuilt release binary for your Pi's architecture and **verifies its checksum**,
creates an unprivileged `pupcup` service user, writes a config and a systemd unit,
and reboots. When the Pi comes back, the dashboard is at **`http://<hostname>.local/`**
(or the Pi's IP address — handy if mDNS is flaky on your network).

**Common options:**

| Flag | Default | Purpose |
|---|---|---|
| `--display gc9a01\|oled` | `gc9a01` | Which panel you wired |
| `--timezone <IANA>` | `America/New_York` | e.g. `America/Chicago` |
| `--hostname <name>` | `pupcup` | System + mDNS name |
| `--version <tag>` | `latest` | A specific release, e.g. `v0.1.0` |
| `--no-reboot` | _(reboots)_ | Provision but don't reboot yet |

Run `sudo bash install.sh --help` for the full list. The installer is
**idempotent** — safe to re-run after changing a flag. Prefer to provision by
hand, or want the step-by-step verification/UAT checklist? See
[docs/initial_pi_flash.md](docs/initial_pi_flash.md).

> **Tip:** Before assembling the enclosure, it's worth confirming each peripheral
> works in isolation with the bundled hardware probes — see
> [Build the hardware](#build-the-hardware).

## Using the web app

Open `http://<hostname>.local/` from any device on the same network. The web app
is server-rendered and HTMX-driven — fast, no build step, works fine on a phone.

| Page | What it's for |
|---|---|
| **Dashboard** (`/`) | Who's been fed today, with per-dog quick-add buttons |
| **Feedings** (`/feedings`) | Record meals & snacks (with a retroactive timestamp), tag a meal's add-ins, and edit/delete past entries |
| **History** (`/history`) | One unified timeline of every meal, snack, illness, and stress event — filter by dog, type, and date range |
| **Dog detail** (`/dogs/{id}`) | An eating-quality chart over a 7/30/90-day window, summary stats, and that dog's history |
| **Dogs** (`/dogs`) | Add/edit dogs (name, accent color, photo) |
| **Illness** (`/illness`), **Stress** (`/stress`) | Log date-range health/stress events (with an "ongoing" toggle and one-click "set end"); stress can apply to the whole household |
| **Tags** (`/tags`) | Manage the add-in catalog — the extras mixed into a meal, taggable inline on `/feedings` |
| **Health** (`/healthz`) | Liveness probe |

## Run it on your laptop (no hardware needed)

You don't need a Pi to develop or try the web app. On macOS or Linux the hardware
drivers compile to no-op **Fakes**, so the daemon seeds a sample household, serves
the full web app, and advertises mDNS — all without touching real GPIO.

```sh
go build ./...
go test ./...

# Defaults bind :80 and write to /var/lib/pupcup (root-only). For local dev, use
# a high port and writable paths:
PUPCUP_LISTEN=:8080 \
PUPCUP_DB_PATH=./pupcup-dev.sqlite \
PUPCUP_PHOTO_DIR=./photos \
  go run ./cmd/pupcup
```

Visit **http://localhost:8080/**. Ctrl-C exits cleanly.

## Build the hardware

The full parts list, wiring narrative, pinout, perfboard layout, OS provisioning,
and acceptance criteria are in
**[docs/pupcup_hardware_build.md](docs/pupcup_hardware_build.md)**. It documents the
default round-LCD build and a dedicated section for the cheaper OLED variant.

**Test as you build.** The repo ships five standalone hardware probes under
[cmd/hwprobe/](cmd/hwprobe/) — one each for the buttons, NeoPixel bar, GC9A01 LCD,
OLED, and rotary encoder. Running them on a freshly-flashed Pi *before* you commit
to the enclosure lets you confirm every peripheral in isolation and catch a
crossed wire while it's still easy to fix. The build doc's
[bring-up tests](docs/pupcup_hardware_build.md#8-hardware-bring-up-tests) walk
through them, and [hardware_test_setup.md](hardware_test_setup.md) is the
copy-paste guide from blank SD card to "all five probes green."

## Configuration

The daemon starts from built-in defaults and overlays a YAML config; any field can
also be overridden by a `PUPCUP_<UPPER_SNAKE>` environment variable (env wins).
The installer writes a minimal `/etc/pupcup/config.yaml`; the **fully documented**
key list — network, storage, display, behavior timers, photo limits — lives in
[deploy/config.example.yaml](deploy/config.example.yaml).

The soldered pin assignments (buttons, rotary) come from the binary's built-in
defaults in [internal/config/config.go](internal/config/config.go), which is the
single source of truth, so a pin correction ships with a normal release rather
than needing a hand-edit on every device.

After editing the config: `sudo systemctl restart pupcup`. Watch logs with
`journalctl -u pupcup -f`.

## Building from source & deploying

Releases are pure-Go static binaries cross-compiled in CI. Cut one by pushing a
version tag — [.github/workflows/release.yml](.github/workflows/release.yml) builds
`linux/arm64` and `linux/armv7`, writes a `SHA256SUMS` manifest, and publishes
them as release assets (which `install.sh` then fetches and checksum-verifies):

```sh
git tag v0.1.0 && git push origin v0.1.0
```

For iterative development against a real Pi, the [deploy/](deploy/) scripts
cross-compile, ship over SSH, install root-owned, and restart the service:

```sh
# First time, on a fresh Pi (run as a sudoer, from a checkout of this repo):
./deploy/bootstrap.sh        # creates the pupcup user, dirs, config + systemd unit

# Every update, from your laptop:
./deploy/deploy.sh
TARGET=scotty@raspberrypi.local ./deploy/deploy.sh   # override the SSH host
```

To cross-compile by hand without deploying:

```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o build/pupcup ./cmd/pupcup
```

The service runs as the unprivileged `pupcup` user with `gpio`/`i2c`/`spi` group
access and `CAP_NET_BIND_SERVICE` (so it can bind `:80` without being root).

## Project layout

```
cmd/pupcup/          the daemon: web server + device event loop
cmd/hwprobe/         five standalone hardware test probes
internal/
  config/            defaults, YAML + env loading, validation (pin source of truth)
  device/            hardware drivers — buttons, rotary, neopixel, gc9a01, oled
                     (each: *_linux.go real driver, *_stub.go, a shared iface, a Fake)
  domain/            core types: dogs, feedings, scores, snacks, events
  store/             SQLite persistence
  web/               HTTP handlers, templates, static assets, SVG charts
  eventbus/          fan-out between the device loop and the web layer
  mdns/  seed/  systemd/  clock/   supporting services
deploy/              bootstrap.sh, deploy.sh, systemd unit, example config
migrations/          SQL schema migrations
docs/                hardware build, Pi flashing, architecture build plan
install.sh           the one-line on-Pi installer
```

The device packages use a **build-tag split** (`*_linux.go` real drivers vs.
`*_stub.go` everywhere else) so the whole project compiles and unit-tests on
macOS while the real GPIO/SPI/I²C code only builds for the Pi.

## Documentation

| Document | What's in it |
|---|---|
| [docs/pupcup_hardware_build.md](docs/pupcup_hardware_build.md) | Parts, wiring, pinout, perfboard layout, OS setup, bring-up tests, OLED variant |
| [hardware_test_setup.md](hardware_test_setup.md) | Blank SD card → all five hardware probes passing |
| [docs/initial_pi_flash.md](docs/initial_pi_flash.md) | Manual provisioning + verification/UAT checklist |
| [docs/pupcup_build_plan.md](docs/pupcup_build_plan.md) | Architecture and the original build plan |
| [deploy/config.example.yaml](deploy/config.example.yaml) | Every config key, documented, with defaults |

## License

Source code is licensed under the [MIT License](LICENSE). Hardware design
documentation — wiring, BOM, pinout, layouts, and bring-up procedures — is
licensed separately under
[Creative Commons Attribution 4.0 International (CC BY 4.0)](LICENSE-HARDWARE).
