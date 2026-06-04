# PupCup — Tech Stack Overview

## Language & runtime

| | |
|---|---|
| **Go 1.25** | Single language for the entire project — daemon, web server, hardware drivers, and CLI probes. The build produces one static binary with no runtime dependencies. |
| **CGO disabled** | `CGO_ENABLED=0` across all builds. The SQLite driver (`modernc.org/sqlite`) is a pure-Go port, which is what makes this possible. |

## Web layer

| Package | Role |
|---|---|
| `net/http` (stdlib) | HTTP server — no framework. Routes are registered by hand. |
| `html/template` (stdlib) | Server-side HTML rendering. All pages are full server renders; no client-side routing. |
| **HTMX 2.0.4** | Bundled JS library (`internal/web/static/htmx.min.js`). Drives partial-page swaps (inline edit, delete confirms, date-range filters) without a build step. |
| `app.css` | Hand-written CSS — no framework or preprocessor. |

## Persistence

| Package | Role |
|---|---|
| **`modernc.org/sqlite v1.50`** | Pure-Go SQLite driver (no CGO, no system library). Embedded in the binary. |
| SQL migrations (`migrations/`) | Plain `.sql` files applied in order at startup via `migrations.go`. No ORM. |

## Hardware drivers

| Package | Role |
|---|---|
| **`periph.io/x/conn/v3`** | HAL abstraction — GPIO, SPI, I²C interfaces. All hardware code talks through periph types. |
| **`periph.io/x/devices/v3`** | Peripheral drivers. Used for the SSD1306 OLED (`ssd1306.NewI2C`). |
| **`periph.io/x/host/v3`** | Host initializer (`host.Init`) — registers the Pi's GPIO/SPI/I²C controllers on startup. |
| Custom GC9A01 driver | Hand-rolled SPI1 driver in `internal/device/gc9a01/` — periph has no GC9A01 support. RGB565 framebuffer, chunked DMA-free flush. |
| Custom NeoPixel driver | SPI0-based SK6812 driver in `internal/device/neopixel/` — encodes WS2812 bit timing into SPI byte patterns at 2.4 MHz. |
| Custom button driver | `internal/device/buttons/` — libgpiod v2 via periph, active-low with pull-ups, falling-edge + confirm-low debounce. |
| Custom rotary driver | `internal/device/rotary/` — Buxton full-step state machine on both CLK and DT edges. Required because the naive single-edge approach produces spurious reversal on this KY-040. |

All device packages use a **build-tag split**: `*_linux.go` (real driver, `//go:build linux`) and `*_stub.go` (no-op Fake, all other platforms). The binary compiles and runs on macOS for development without touching GPIO.

## Networking

| Package | Role |
|---|---|
| **`github.com/grandcat/zeroconf v1.0`** | mDNS/DNS-SD — advertises `pupcup.local` on the LAN so phones and laptops can reach the dashboard without knowing the Pi's IP. |
| **`github.com/miekg/dns v1.1`** | DNS library used internally by zeroconf (indirect dependency). |

## Configuration

| Package | Role |
|---|---|
| **`gopkg.in/yaml.v3`** | YAML config file parsing (`/etc/pupcup/config.yaml`). Any field can also be overridden by a `PUPCUP_*` environment variable. |

## Utilities (indirect / stdlib)

| Package | Role |
|---|---|
| `github.com/google/uuid v1.6` | UUID generation for dog and event IDs. |
| `github.com/dustin/go-humanize v1.0` | Human-readable time and number formatting in templates. |
| `github.com/ncruces/go-strftime v1.0` | `strftime`-style date formatting for SQLite queries. |
| `github.com/cenkalti/backoff v2.2` | Retry with exponential back-off (used by zeroconf). |
| `golang.org/x/{net,sync,sys,crypto}` | Standard extended library — networking primitives, sync helpers, OS syscalls, crypto (mDNS/DNS deps). |
| `modernc.org/{libc,mathutil,memory}` | Pure-Go C runtime shims that `modernc.org/sqlite` depends on. |

## Build & CI

| Tool | Role |
|---|---|
| **Go toolchain** | `go build`, `go test`, `go vet` — no Makefile needed for most tasks. |
| **GitHub Actions** (`.github/workflows/release.yml`) | On a `v*` tag push: cross-compiles `linux/arm64` and `linux/armv7` static binaries, writes a `SHA256SUMS` manifest, and publishes them as GitHub Release assets. |
| `deploy/deploy.sh` | Local dev deploy script — cross-compiles arm64, rsyncs over SSH, installs root-owned to `/opt/pupcup/`, and restarts the systemd service. |
| `install.sh` | One-line on-Pi installer — downloads the release binary for the Pi's architecture, verifies its checksum, and provisions the full service. |

## Target platform

Raspberry Pi 3B+ running **Raspberry Pi OS Lite (64-bit, Trixie/Debian 13)**. The binary runs as an unprivileged `pupcup` service user with `gpio`/`spi`/`i2c` group access and `CAP_NET_BIND_SERVICE` (to bind `:80`). Time is kept by `systemd-timesyncd` (no hardware RTC).
