# PupCup — Software Build Plan

The end-to-end software design for PupCup: a single pure-Go binary that runs on a Raspberry Pi Zero 2W and simultaneously (a) drives the physical button device — buttons, rotary encoder, OLED, and NeoPixel status bar — and (b) serves a friendly local-network web app for richer logging, editing, and analytics. This plan covers architecture, package layout, data model, hardware drivers, web layer, deployment, and verification.

> Hardware build is documented separately in [pupcup_hardware_build.md](pupcup_hardware_build.md). This plan assumes that build is complete and the Pi is provisioned per its instructions.

## 1. Goals and non-goals

### Goals
1. Press-and-go logging of meal feedings (Green/Yellow/Red) and snacks (Blue) per dog from the physical device, with sub-second latency from button press to OLED confirmation and DB write.
2. Local-network web app for richer entry (specifics, illness, stress events), editing past entries, retroactive entries, and v1 analytics.
3. Single-device deployment — one Pi, one binary, one SQLite file.
4. "Were the dogs fed?" glanceable answer via the front-edge LED bar after meals.
5. Pure Go, no CGO, cross-compilable from a developer laptop.
6. Pleasant, playful UI — soft pastels, paw-print accents, friendly typography.
7. Future-feature seams: data-trend analytics and Home Assistant integration are anticipated but not built in v1.

### Non-goals (explicit)
- No authentication, user accounts, or per-user attribution. The site binds to LAN interfaces only.
- No cloud sync, remote access, or port-forwarding guidance.
- No mobile native app.
- No SPA / JS framework / bundler. HTMX only, served from Go templates.
- No 3D-printed enclosure design. (Mechanical happens after this plan executes.)
- No internationalization beyond `America/New_York` and US English.
- No correlation/trend analytics in v1 — only a feeding history table and an eating-quality-rate-over-time chart.

## 2. High-level architecture

A single Go binary with multiple goroutines, communicating through a typed in-memory event bus. The hardware layer publishes domain events; the web layer reads and writes domain state through a SQLite-backed store; both layers share a clock, config, and logger.

```
┌──────────────────────── pupcup (single Go binary) ─────────────────────────┐
│                                                                            │
│   ┌─ device/ ─────────────┐   ┌─ event bus (chan) ─┐   ┌─ web/ ─────────┐  │
│   │  buttons goroutine    │──▶│  domain events     │──▶│  HTTP server   │  │
│   │  rotary goroutine     │   │  (FeedRecorded,    │   │  html/template │  │
│   │  oled goroutine       │   │   SnackRecorded,   │   │  + HTMX        │  │
│   │  neopixel goroutine   │◀──│   LockChanged,…)   │   │                │  │
│   │  state machine        │   └────────────────────┘   └────────────────┘  │
│   └───────────────────────┘                                                │
│                       ▼                                                    │
│                 ┌──────────────────────────────┐                           │
│                 │  store/  (modernc.org/sqlite)│                           │
│                 └──────────────────────────────┘                           │
│                                                                            │
│   ┌─ mdns goroutine (grandcat/zeroconf advertising pupcup.local) ──────┐   │
│   └────────────────────────────────────────────────────────────────────┘   │
└────────────────────────────────────────────────────────────────────────────┘
```

Why single-binary single-process: simplest deployment, no IPC, lowest RAM footprint, leverages Go concurrency primitives. The event bus interface is the seam that lets us split into separate processes later if we want to (it would only require swapping a buffered channel for a localhost socket).

## 3. Repository layout

```
pupcup/
├── cmd/
│   ├── pupcup/             # main daemon
│   │   └── main.go         # config load, wiring, signal handling
│   └── hwprobe/            # standalone bring-up tools (one tiny main per peripheral)
│       ├── oled/main.go
│       ├── buttons/main.go
│       ├── rotary/main.go
│       └── neopixel/main.go
├── internal/
│   ├── config/             # YAML + env config; one struct, validated on load
│   ├── clock/              # time.Now wrapper for testability (real & fake)
│   ├── store/              # SQLite wrappers — schema, migrations, queries
│   ├── domain/             # types: Dog, Feeding, Snack, IllnessEvent, StressEvent, ButtonColor, Score, FeedKind
│   ├── eventbus/           # typed pub/sub on buffered channel
│   ├── device/
│   │   ├── buttons/        # 4-button driver (debounced, periph.io)
│   │   ├── rotary/         # KY-040 quadrature decoder + button + long-press
│   │   ├── oled/           # SSD1306 wrapper + screen renderer
│   │   ├── neopixel/       # SK6812 SPI bit-bang driver (pure Go)
│   │   └── state/          # device state machine
│   ├── web/
│   │   ├── server.go       # http.Server, routes, middleware
│   │   ├── handlers/       # one file per page (dashboard.go, dogs.go, …)
│   │   ├── templates/      # *.html — base + partials
│   │   ├── static/         # css, htmx.min.js, fonts, paw icons
│   │   └── chart/          # server-rendered SVG helpers
│   └── mdns/               # zeroconf wrapper
├── deploy/
│   ├── pupcup.service      # systemd unit
│   ├── deploy.sh           # cross-compile + rsync + restart
│   └── bootstrap.sh        # first-time install on a fresh Pi (creates user, dirs, perms)
├── migrations/             # SQL DDL files, numbered: 0001_init.sql, …
├── test/
│   ├── store_test.go
│   ├── state_test.go
│   └── handlers_test.go
├── go.mod
├── go.sum
├── README.md
├── pupcup_hardware_build.md
└── pupcup_build_plan.md    # (this file)
```

Public surface = `cmd/pupcup/main.go` + the `internal/` packages. Nothing in `pkg/`; this isn't a library.

## 4. Dependencies (locked)

| Module | Purpose |
|---|---|
| `periph.io/x/conn/v3` + `periph.io/x/host/v3` + `periph.io/x/devices/v3` | GPIO, I²C, SPI, SSD1306 driver |
| `modernc.org/sqlite` | Pure-Go SQLite (no CGO) |
| `github.com/grandcat/zeroconf` | mDNS advertising |
| `gopkg.in/yaml.v3` | Config file parsing |

Standard library: `log/slog` (logging), `html/template`, `net/http`, `embed` (templates + static), `database/sql`. No third-party HTTP framework, no router framework — `net/http` ServeMux (Go 1.22+ method matching) is sufficient.

## 5. Data model

All times stored as **UTC** in the DB; presentation layer converts to `America/New_York`. SQLite enforces foreign keys (`PRAGMA foreign_keys = ON`).

### 5.1 Schema (DDL, simplified)

```sql
CREATE TABLE dogs (
    id              INTEGER PRIMARY KEY,
    name            TEXT NOT NULL,
    accent_color    TEXT NOT NULL DEFAULT '#A8D8B9',  -- hex incl '#'
    photo_path      TEXT,                              -- relative to /var/lib/pupcup/photos/
    sort_order      INTEGER NOT NULL DEFAULT 0,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE feedings (
    id              INTEGER PRIMARY KEY,
    dog_id          INTEGER NOT NULL REFERENCES dogs(id) ON DELETE RESTRICT,
    ts_utc          DATETIME NOT NULL,
    kind            TEXT NOT NULL CHECK (kind IN ('standard','nonstandard')) DEFAULT 'standard',
    score           TEXT NOT NULL CHECK (score IN ('full','partial','none')),
    specifics       TEXT,
    source          TEXT NOT NULL CHECK (source IN ('button','web')),
    deleted_at      DATETIME,
    edited_at       DATETIME,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_feedings_dog_ts ON feedings(dog_id, ts_utc) WHERE deleted_at IS NULL;

CREATE TABLE snacks (
    id              INTEGER PRIMARY KEY,
    dog_id          INTEGER NOT NULL REFERENCES dogs(id) ON DELETE RESTRICT,
    ts_utc          DATETIME NOT NULL,
    specifics       TEXT,
    source          TEXT NOT NULL CHECK (source IN ('button','web')),
    deleted_at      DATETIME,
    edited_at       DATETIME,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_snacks_dog_ts ON snacks(dog_id, ts_utc) WHERE deleted_at IS NULL;

CREATE TABLE illness_events (
    id              INTEGER PRIMARY KEY,
    dog_id          INTEGER NOT NULL REFERENCES dogs(id) ON DELETE RESTRICT,
    start_date      DATE NOT NULL,
    end_date        DATE,                              -- NULL = ongoing
    notes           TEXT,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE stress_events (
    id              INTEGER PRIMARY KEY,
    dog_id          INTEGER REFERENCES dogs(id) ON DELETE RESTRICT,  -- NULL = whole household
    start_date      DATE NOT NULL,
    end_date        DATE,
    kind            TEXT,                              -- e.g. "travel", "houseguests", "boarding"
    notes           TEXT,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Singleton: device runtime state that should survive restarts.
CREATE TABLE device_state (
    id                  INTEGER PRIMARY KEY CHECK (id = 1),
    locked_until_utc    DATETIME,                      -- post-meal lock expiry
    last_lock_reason    TEXT,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO device_state (id) VALUES (1);
```

### 5.2 Domain types (Go)

```go
type Score string  // "full" | "partial" | "none"
type FeedKind string  // "standard" | "nonstandard"
type ButtonColor string // "green" | "yellow" | "red" | "blue"
type Source string // "button" | "web"

type Dog struct { ID int64; Name, AccentColor, PhotoPath string; SortOrder int }
type Feeding struct { ID, DogID int64; TS time.Time; Kind FeedKind; Score Score; Specifics string; Source Source; DeletedAt, EditedAt *time.Time }
type Snack struct { ID, DogID int64; TS time.Time; Specifics string; Source Source; DeletedAt, EditedAt *time.Time }
type IllnessEvent struct { ID, DogID int64; Start time.Time; End *time.Time; Notes string }
type StressEvent struct { ID int64; DogID *int64; Start time.Time; End *time.Time; Kind, Notes string }
```

### 5.3 Migration strategy

- One numbered SQL file per migration, applied in order on startup.
- A `schema_migrations(version, applied_at)` table tracks applied versions.
- Migrations are forward-only; a backup of the DB file is copied to `pupcup.sqlite.bak.YYYYMMDD-HHMMSS` before any migration runs.
- v1 ships with a single `0001_init.sql` containing the schema above plus a default seed of three dogs (configurable in `bootstrap.sh`).

## 6. Hardware drivers

All drivers are in `internal/device/<name>/`. Each exposes a small interface so tests can swap a fake.

### 6.1 Buttons (`device/buttons`)

```go
type Driver interface {
    Events() <-chan ButtonEvent
    Close() error
}
type ButtonEvent struct { Color domain.ButtonColor; TS time.Time }
```

- Uses `periph.io/x/conn/v3/gpio` with `PullUp` and edge detection on falling edge.
- Software debounce: 25 ms quiet period after a falling edge before next event accepted on the same pin.
- One goroutine per button reading edges, plus a fan-in to the `Events()` channel.

### 6.2 Rotary encoder (`device/rotary`)

```go
type Driver interface {
    Events() <-chan RotaryEvent
    Close() error
}
type RotaryEvent struct {
    Kind    RotaryEventKind  // "rotate_cw" | "rotate_ccw" | "press_short" | "press_long"
    TS      time.Time
}
```

- Quadrature decoder using `periph.io` `gpio.WaitForEdge` on CLK; reads DT inside the handler to determine direction. Direction inversion config flag.
- Debounce 5 ms on the rotary lines (some KY-040 modules are dirty without a hardware filter).
- `SW`: short press = release within 1.5 s; long press = held ≥ 1.5 s. Long-press emits `press_long` exactly once on cross-threshold, no repeat.

### 6.3 OLED (`device/oled`)

```go
type Renderer interface {
    Render(scene Scene) error
    Close() error
}
type Scene interface { isScene() }   // sealed
type DogSelectorScene struct { Dog domain.Dog; Index, Total int }
type LockedSummaryScene struct { Entries []SummaryEntry; LockedUntil time.Time }
type SnackModeScene struct { Dog domain.Dog; Remaining time.Duration; AlreadyRecorded []int64 }
type SplashScene struct { Message string }
```

- Uses `periph.io/x/devices/v3/ssd1306` over I²C (`/dev/i2c-1`, address `0x3C`).
- 128×64 framebuffer; full-redraw on scene change, partial-redraw for clock ticks.
- Embedded bitmap fonts (small, medium, large) compiled into the binary via `//go:embed`. "Large" = 24-px tall sans for dog names; "small" = 8-px for status.
- Anti-burn-in: invert pixels every 24 hours; periodically nudge content position by 1 px.

### 6.4 NeoPixel (`device/neopixel`) — custom pure-Go SPI driver

```go
type Strip interface {
    SetAll(c Color) error
    SetPixel(i int, c Color) error
    Show() error
    Close() error
}
type Color struct { R, G, B uint8 }   // RGB; SK6812RGBW handled with a white channel inferred or extended
```

- Open `/dev/spidev0.0` via `periph.io/x/conn/v3/spi/spireg` at **2.4 MHz** clock.
- Encode each WS data bit as **3 SPI bits**: `1` → `0b110`, `0` → `0b100`. At 2.4 MHz this yields the canonical 800 kHz / 1.25 µs WS timing within tolerance.
- Per-frame buffer = `nLEDs × 24 bits × 3 SPI bits / 8 bits per byte = nLEDs × 9` bytes (72 bytes for 8 LEDs). Plus a 50 µs reset gap (≥ 30 zero bytes at 2.4 MHz).
- The driver pre-allocates the buffer in `NewStrip(n)` to avoid per-frame allocations.
- Animation goroutine ticks at 30 Hz only when an animation is active (idle-off state writes a blank frame and stops ticking).

### 6.5 Device state machine (`device/state`)

States:
- `Idle` — OLED shows the per-dog selector page; rotary scrolls through dogs; meal buttons (G/Y/R) record a feeding for the selected dog and advance to next dog automatically.
- `LockedSummary` — entered when all dogs have a feeding within the current "meal window" (or any meal recorded triggers a 4-h lock by default); OLED shows the per-dog summary list; LED bar glows green; meal buttons are ignored.
- `SnackMode` — entered by tap of Blue while in Idle, or **press-and-hold (≥ 1.5 s) of Blue** while in LockedSummary; OLED shows "SNACK — pick dog"; rotary picks a dog; tapping Blue records a snack for the selected dog. Exits automatically on either (a) all dogs recorded, or (b) 60-second inactivity, returning to the prior state.
- `EditOverride` — entered by long-press of rotary SW while in LockedSummary; clears `device_state.locked_until_utc`; immediately transitions back to Idle.

State transitions emit `domain.LockChanged` events and persist to `device_state`. On startup, the state is reconstructed from `device_state` so a reboot during the lock window resumes correctly.

## 7. Web layer

### 7.1 Routes

| Method | Path | Purpose |
|---|---|---|
| GET | `/` | Dashboard: today's per-dog status, "fed?" indicators, quick-add buttons |
| GET | `/dogs` | List + manage dogs |
| POST | `/dogs` | Create a dog |
| GET | `/dogs/{id}` | Per-dog detail: history table + analytics chart |
| POST | `/dogs/{id}` | Update name / color / photo |
| DELETE | `/dogs/{id}` | Soft-delete (only when zero non-deleted feedings) |
| POST | `/feedings` | Add (HTMX) — supports retroactive timestamp |
| GET | `/feedings/{id}/edit` | Edit form (HTMX dialog) |
| PATCH | `/feedings/{id}` | Update |
| DELETE | `/feedings/{id}` | Soft-delete |
| POST | `/snacks` | Same shape as feedings |
| PATCH | `/snacks/{id}` | |
| DELETE | `/snacks/{id}` | |
| GET | `/illness` | Index of illness events |
| POST | `/illness` | Add |
| PATCH | `/illness/{id}` | Update (commonly used to set `end_date`) |
| GET | `/stress` | Index of stress events |
| POST | `/stress` | Add |
| PATCH | `/stress/{id}` | |
| GET | `/history` | Unified timeline (filterable by dog / kind / date range) |
| GET | `/healthz` | JSON liveness probe (used by systemd watchdog and future Home Assistant) |
| GET | `/static/*` | Embedded static assets via `embed.FS` |

### 7.2 Templates

- `internal/web/templates/` is embedded via `//go:embed` so no template files are deployed alongside the binary.
- Layout: `base.html` defines the page chrome; pages extend with `{{define "content"}}…{{end}}`.
- Partials: `feeding_row.html`, `dog_card.html`, `chart_eating_quality.html`, `confirm_modal.html` — these are HTMX swap targets.
- Helper funcs registered in a single `funcMap`: `formatTime`, `formatDate`, `pawIcon`, `accentColor`, `score…` etc.

### 7.3 Charts (server-side SVG)

- Chart helper in `internal/web/chart/` produces SVG strings. No JS chart libs.
- v1 charts:
  - **Stacked bar over time** — full / partial / none counts per day for a window (7, 30, 90 days). Used on each dog detail page and on the dashboard summary card.
- The helper is intentionally minimal (rectangles + text + a few axes); future charts (heatmaps, scatter) live in the same package.

### 7.4 Style

- Single hand-written CSS file `app.css` (~3 KB compressed) embedded.
- Palette:
  - Background `#FFF7F0` (warm cream)
  - Accent green `#A8D8B9`, yellow `#F8D8A0`, red `#F2A6A1`, blue `#A8C8F8`
  - Text `#3A332B`
  - Subtle border/shadow `rgba(58,51,43,0.08)`
- Typography: Quicksand 400/500/700 (subset, ~18 KB woff2), self-hosted from `/static/fonts/`.
- Component shapes: 16-px rounded corners, 1-px borders, soft drop shadows. Generous padding. Paw-print accent SVG used as bullet/divider/empty-state.
- Density: dashboard prioritizes "today" cards above the fold; deeper pages use a single column at ≤ 720 px max-width and a two-column dashboard grid above 720 px.

### 7.5 mDNS

- `internal/mdns/` wraps `grandcat/zeroconf` to advertise:
  - Service: `_http._tcp`
  - Hostname: `pupcup.local`
  - Port: 80
  - TXT record: `version=<build>`
- Relies on the on-Pi `avahi-daemon` for the `.local` resolver from non-Apple clients. The dashboard prints the IP at the top as a fallback for clients (some Android setups) where mDNS doesn't resolve.

## 8. Configuration

YAML at `/etc/pupcup/config.yaml`, with environment variable overrides (prefix `PUPCUP_`). The shipped default:

```yaml
listen: ":80"
db_path: /var/lib/pupcup/pupcup.sqlite
photo_dir: /var/lib/pupcup/photos
timezone: America/New_York

# Hardware
spi_device: /dev/spidev0.0
i2c_bus: 1
oled_addr: 0x3C
neopixel_count: 8
button_pins:
  green: 5
  yellow: 6
  red: 13
  blue: 19
rotary_pins:
  clk: 17
  dt: 27
  sw: 22
button_debounce_ms: 25
rotary_debounce_ms: 5
long_press_ms: 1500

# Behavior
meal_lock_minutes: 240
snack_mode_idle_seconds: 60
default_feed_kind: standard
mdns_hostname: pupcup
```

Validated on load; missing required values fail fast with a clear error message.

## 9. Logging and observability

- Structured logs via `log/slog` JSON to stdout — captured by journald.
- Log fields: `time`, `level`, `msg`, `component` (e.g. `device.buttons`, `web.handler`, `store`), plus context-specific (e.g. `dog_id`, `score`, `latency_ms`).
- Levels: `INFO` for domain events (feedings, snacks, lock changes), `DEBUG` for low-level hardware ticks, `WARN` for recoverable issues, `ERROR` for the rest.
- `/healthz` returns:
  ```json
  {
    "ok": true,
    "version": "0.1.0",
    "uptime_s": 12345,
    "last_button_event": "2026-04-29T08:45:13-04:00",
    "db_size_bytes": 184320,
    "device_locked": true,
    "locked_until": "2026-04-29T12:30:00-04:00"
  }
  ```
- systemd `WatchdogSec=60` with `sd_notify` heartbeats from the main loop.

## 10. Deployment

### 10.1 Cross-compile + ship

`deploy/deploy.sh`:
```sh
#!/usr/bin/env bash
set -euo pipefail
TARGET=${TARGET:-pupcup@pupcup.local}
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
  go build -trimpath -ldflags "-s -w -X main.version=$(git rev-parse --short HEAD)" \
  -o build/pupcup ./cmd/pupcup
rsync -avz --progress build/pupcup "$TARGET:/tmp/pupcup.new"
ssh "$TARGET" 'sudo install -m 0755 /tmp/pupcup.new /opt/pupcup/pupcup && sudo systemctl restart pupcup && sudo systemctl status pupcup --no-pager'
```

### 10.2 systemd unit

`/etc/systemd/system/pupcup.service`:
```ini
[Unit]
Description=PupCup feeding tracker
After=network-online.target avahi-daemon.service time-sync.target
Wants=network-online.target

[Service]
Type=notify
User=pupcup
Group=pupcup
SupplementaryGroups=gpio i2c spi
ExecStart=/opt/pupcup/pupcup --config /etc/pupcup/config.yaml
Restart=on-failure
RestartSec=5
WatchdogSec=60
RuntimeDirectory=pupcup
StateDirectory=pupcup
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
NoNewPrivileges=true
ReadWritePaths=/var/lib/pupcup /etc/pupcup
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
```

### 10.3 First-time bootstrap

`deploy/bootstrap.sh` runs once on a fresh Pi:
```sh
sudo useradd --system --home /var/lib/pupcup --shell /usr/sbin/nologin pupcup || true
sudo usermod -aG gpio,i2c,spi pupcup
sudo install -d -o pupcup -g pupcup /var/lib/pupcup /var/lib/pupcup/photos /etc/pupcup
sudo install -m 0644 deploy/config.example.yaml /etc/pupcup/config.yaml
sudo install -m 0644 deploy/pupcup.service /etc/systemd/system/pupcup.service
sudo systemctl daemon-reload
sudo systemctl enable pupcup
```

After bootstrap, regular updates use `deploy.sh` only.

## 11. Testing strategy

### 11.1 Unit tests (laptop, fast)
- `store/`: open an in-memory SQLite, run migrations, exercise CRUD + soft-delete + filters.
- `domain/` and `device/state/`: pure-Go state-machine tests with a fake clock and fake bus.
- `web/handlers/`: spin up `httptest.Server` against the in-memory store; assert HTML fragments returned by HTMX endpoints.
- Goal: `go test ./...` runs in < 5 s.

### 11.2 Hardware integration tests (on-device)
- The `cmd/hwprobe/` tools (one per peripheral) double as integration tests during build. Re-run them after any wiring change.
- Optional: `cmd/hwprobe/all` runs through every probe in sequence.

### 11.3 End-to-end UAT (manual checklist)
See § 13.

## 12. Implementation milestones

Suggested execution order. Each milestone ends in a runnable, demonstrable artifact.

1. **OS + hardware probes** — Pi provisioned per the hardware doc; `hwprobe` programs verify each peripheral.
2. **OLED hello** — `device/oled` package + a "Hello PupCup" splash on boot.
3. **Button + rotary events** — `device/buttons` and `device/rotary` packages emit events on stdout.
4. **NeoPixel pure-Go driver** — solid colors, walking pixel, smooth fade. Verifies SPI 3-bit encoding and level shifter.
5. **SQLite store** — schema, migrations, CRUD, soft-delete, in-memory test DB.
6. **Domain types + event bus** — typed pub/sub.
7. **Device state machine** — wires hardware events to bus + store + OLED scenes + LED states. End of this milestone: pressing buttons records feedings; the OLED reflects state; LEDs glow green for 4 hours after a meal.
8. **Web shell** — `net/http` server, base template, 404, healthz, embedded statics, mDNS.
9. **Dashboard + dogs management** — list of today's status, manage dogs (name/color/photo).
10. **Feedings & snacks CRUD** — add via HTMX, retroactive timestamp picker, edit, soft-delete with confirm.
11. **Illness + stress events** — date-range form, "ongoing" toggle, set-end action.
12. **History page** — unified, filterable timeline.
13. **Per-dog detail + chart** — eating-quality stacked-bar SVG.
14. **systemd unit + deploy.sh + bootstrap.sh** — production install on the Pi.
15. **UAT pass + polish** — run the checklist; fix any rough edges; finalize the README.

## 13. Verification / UAT checklist

Run each on the deployed device.

### 13.1 Cold-boot
- [ ] Plug in the Pi cold. Within 30 s the OLED shows the dog selector.
- [ ] `pupcup.local` resolves from a phone on the home wifi and the dashboard loads.
- [ ] LED bar is off in idle.

### 13.2 Button-driven feeding
- [ ] Rotate the dial — OLED cycles through dogs; wraps at the ends.
- [ ] Select dog A; tap GREEN — OLED briefly confirms; the dashboard now shows dog A as fed full.
- [ ] Select dog B; tap YELLOW — partial feeding recorded.
- [ ] Select dog C; tap RED — none recorded.
- [ ] LED bar transitions to solid green.
- [ ] OLED transitions to the locked summary scene listing each dog with its score.
- [ ] Within 4 hours, taps on G/Y/R are ignored (silent).
- [ ] Tap of BLUE alone in the locked period is ignored; press-and-hold of BLUE (≥ 1.5 s) enters snack mode; pick a dog; tap BLUE; snack recorded; the snack-mode scene exits after 60 s of inactivity, returning to the locked summary.
- [ ] Long-press rotary SW (≥ 1.5 s) clears the lock; LED fades out; OLED returns to selector.
- [ ] After 4 hours the lock auto-clears.

### 13.3 Web app
- [ ] Dashboard accurately reflects today's entries.
- [ ] Edit a past feeding (change timestamp + score) — change is reflected in history and in the dog's detail chart.
- [ ] Soft-delete a feeding — entry disappears from the table; chart updates.
- [ ] Add a retroactive feeding via web with a custom timestamp — appears in the correct chronological position.
- [ ] Add an illness event spanning yesterday → today with an "ongoing" end; later set the end date.
- [ ] Add a stress event for the whole household.
- [ ] Manage dogs: rename, change accent color, upload a photo. Photo appears on dashboard.

### 13.4 Resilience
- [ ] Reboot Pi with home wifi turned off. Press a button — feeding recorded with a sane timestamp from DS1307. Re-enable wifi — `pupcup.local` reachable; the entry appears.
- [ ] Run `./deploy.sh` while the device is in the locked state — service restarts, OLED briefly blanks, LED bar resumes green, lock state preserved (verified via dashboard).
- [ ] Pull power abruptly mid-write — on next boot, no DB corruption (SQLite WAL mode) and the state machine resumes correctly.
- [ ] `systemctl status pupcup` shows `active (running)`; journald shows structured logs.

### 13.5 Polish
- [ ] Dashboard renders cleanly on iPhone Safari and a desktop browser.
- [ ] Buttons and dials have no perceptible input lag (< 100 ms button-to-OLED).
- [ ] No spurious feedings from button bounce after vigorous press.
- [ ] OLED text is legible from across the room.

When all items pass, v1 is shippable.

## 14. Future-feature seams (designed-in, not built)

- **Trend / correlation analytics** — the schema's date-range model for illness/stress and the foreign-keyed `feedings`/`snacks` tables already support views like "what did this dog eat in the 48h before each sick day". Add SQL views and chart helpers in a future phase without schema changes.
- **Time-of-day heatmap** — derivable from `feedings.ts_utc` directly. New chart helper, no schema work.
- **Home Assistant integration** — `/healthz` is the foundation. Add `/ha/states` returning a per-dog "fed/hungry" boolean; expose via Home Assistant's RESTful sensor or, optionally, MQTT. The event bus already publishes the events that would feed an MQTT publisher.
- **Multi-user attribution** — would require a sessions table and middleware; the current schema stores `source` (button vs web), which is the seam that becomes `actor_id` later.
- **Cloud sync / family-shared instance** — the SQLite file is a single-file primitive; a future phase could add Litestream replication to S3/B2 without touching application code.
- **OTA self-update** — currently `deploy.sh` is the update path; a future phase could add an admin-only POST endpoint that accepts a signed binary and triggers the systemd restart cycle.

These are deliberately kept out of v1 to stay focused on getting a polished, reliable single-device build into household use first.
