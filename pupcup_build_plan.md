# PupCup — Software Build Plan

The end-to-end software design for PupCup: a single pure-Go binary that runs on a Raspberry Pi Zero 2W and simultaneously (a) drives the physical button device — buttons, rotary encoder, OLED, and NeoPixel status bar — and (b) serves a friendly local-network web app for richer logging, editing, and analytics. This plan covers architecture, package layout, data model, hardware drivers, web layer, deployment, and verification.

> Hardware build is documented separately in [pupcup_hardware_build.md](pupcup_hardware_build.md). This plan assumes that build is complete and the Pi is provisioned per its instructions.

## Progress Summary

_Last updated: 2026-06-02 — **milestone 13 done** (per-dog detail + eating-quality chart): a new per-dog page, **`GET /dogs/{id}`**, reached from the dog's name on the dashboard and the dogs-management list. It shows a **server-rendered SVG stacked-bar chart** of eating quality — full / partial / none meals per day — over a selectable look-back window (**7 / 30 / 90 days**, default 30, chosen via a plain `?window=` GET param so it works without JavaScript), a **summary** (meal total, per-score counts + whole-percent shares, snack count), and a **read-only history table** (meals + snacks merged newest-first; edits stay on `/feedings`, with links out to `/feedings` and the filtered `/history?dog=`). The chart helper is a new minimal package, **`internal/web/chart`** — pure functions returning `template.HTML`, no JS chart libs; bar colors come from the shared palette via `.bar-full`/`.bar-partial`/`.bar-none` CSS classes so the chart never drifts from the badges elsewhere, and an all-zero window renders a friendly empty-state. Feedings are bucketed by household-local day; the window filter reuses the store's instant bounds. Unknown / soft-deleted dogs and non-numeric ids 404. Builds/vets/tests green (new chart unit tests — empty-state, per-score segments, segment omission, well-formed SVG — plus detail-page 404, render, window-range-filter, and empty-window tests); arm64 cross-compile and a **live laptop smoke** (detail links on `/` and `/dogs`, `/dogs/{id}` 200 with chart + stats + table, window 7/30/90 + bad-window→30 fallback, 404s) pass._
_Earlier on 2026-06-02 — **milestone 12 done** (unified history timeline): a new read-only page, **`GET /history`**, that merges every recorded activity — meals, snacks, illness, and stress — into one newest-first timeline, **filterable by dog, by entry type, and by date range**. Filtering is plain **GET query params** (`dog` / `type` / `from` / `to`), so the page works without JavaScript and is bookmarkable; feedings/snacks are date-range-filtered in the store while the few illness/stress rows are pulled per-dog and **overlap-filtered in memory** (an event shows if its `[start, end]` span — `end` nil = ongoing/open — intersects the window). A household stress event surfaces for any single-dog filter; each row carries a neutral category tag (Meal/Snack/Illness/Stress) plus the per-kind detail (score/notes/range/kind). Calendar dates stay UTC-stable (reusing `fmtEventDate`); instants render in the household loc. Builds/vets/tests green (new merged-timeline, dog-filter, type-filter, date-range-bounds, event-overlap, and no-dogs tests); arm64 cross-compile and a **live laptop smoke** (nav link, `/history` 200, merged 3-kind timeline, type/dog/date filters, household-stress retention) pass._
_Earlier on 2026-06-02 — **milestone 11 done** (illness + stress events): two new HTMX-driven pages, **`/illness`** and **`/stress`**, modelled on `/feedings`. `/illness` logs a per-dog sickness with a start date, an **"ongoing" toggle** (the end-date picker disables while it's checked), and notes; `/stress` logs a stressor for one dog **or the whole household** (nullable dog) with an optional **kind** (travel/boarding/…). Each lists its history newest-first with inline **edit** and **delete-with-confirm**; an ongoing row carries a one-click **"set end" action** (a `PATCH` posting the unchanged fields plus an end date). Events are **calendar dates** (not instants): parsed and stored in UTC with no timezone shift and displayed via a UTC-based `fmtEventDate`, so the day never rolls under a far-east `loc` (regression-tested). Create degrades to a **Post/Redirect/Get without JavaScript**; edit/delete/set-end are htmx-only; validation errors retarget a banner (`HX-Retarget`/`HX-Reswap`). Builds/vets/tests green (new illness/stress create·edit·update·set-end·delete, household-vs-dog, ongoing-toggle, end-before-start, and no-TZ-shift tests); arm64 cross-compile and a **live laptop smoke** (nav links, both index pages 200, ongoing create→row, set-end PATCH, household stress, retarget header, non-JS PRG) pass._
_Earlier on 2026-06-02 — **milestone 10 done** (feedings & snacks CRUD via HTMX): the **`/feedings`** page records meals and snacks (dog · score · kind · optional notes · a retroactive `datetime-local` picker) and lists recent activity (feedings + snacks merged, newest-first) with per-row inline **edit** and **delete-with-confirm**. HTMX (vendored as `static/htmx.min.js` — no CDN) drives it: `POST /feedings` prepends the new row (out-of-band-removing the empty-state placeholder); `GET /feedings/{id}/edit` swaps in an inline edit form; `PATCH /feedings/{id}` returns the refreshed row; `DELETE /feedings/{id}` (with `hx-confirm`) removes it; `GET /feedings/{id}` restores the read-only row for the edit form's Cancel — and the same five for `/snacks`. The **dashboard** (`GET /`) gained per-dog **quick-add** buttons (Full/Some/None) that `hx-post` a feeding for *now* and swap in the refreshed dog card (the `dog_status_card` partial, also rendered standalone). Validation errors retarget a banner (`HX-Retarget`/`HX-Reswap`) so bad input never tears the list or the row; meal/snack **create degrades to a Post/Redirect/Get without JavaScript** (edit/delete are htmx-only). Web-created entries carry `source=web`; edits set `edited_at`. Builds/vets/tests green (new feedings/snacks CRUD, retroactive-timestamp, quick-add-card, htmx-error, and merge-ordering tests); arm64 cross-compile and a **live laptop smoke** (htmx asset served, meal/snack create→row, quick-add→card, edit-form prefill, PATCH→row, empty-200 delete, retarget headers, non-JS PRG fallback) pass. **Milestones 8–9** (web shell, `internal/mdns` advertiser, dashboard, dogs management, photos) remain in place. The web layer is still request/response and does not subscribe to the event bus. Keep this section and the §12 milestone ledger in sync as major components land._

**Status legend:** ✅ Completed · 🟡 Partly completed (see note) · ⬜ Not started. Markers appear on the concrete build elements in §3–§13; the strategy/requirements sections (§1, §2, §14) are intentionally unmarked. The per-milestone ledger in **§12** is the primary, regularly-updated tracker — update it alongside this summary.

Hardware bring-up is complete — every peripheral (OLED, four buttons, KY-040 rotary, SK6812 NeoPixel, DS1307 RTC) is confirmed working, and the four `cmd/hwprobe` tools that exercise them are in place. On top of that, the pure-Go software baseline compiles, `go vet`s, unit-tests, and cross-compiles to `linux/arm64` cleanly. That baseline includes the full persistence layer (`internal/store` — dogs, feedings, snacks, illness, stress, and device-lock CRUD with soft-delete, filters, numbered migrations, and a pre-migration backup), the domain types and the in-process event bus, the configuration loader (YAML + `PUPCUP_*` env overrides + fail-fast validation), the testable clock, and all four hardware drivers behind build-tag-split interfaces with Fakes.

The device state machine is unit-tested for its three implemented modes — `Idle`, `LockedSummary`, and `SnackMode` — covering dog selection, meal recording, the all-dogs-fed lock, the meal-complete grace timeout (locks a partial session, leaving un-fed dogs unrecorded), last-meal-timed lock expiry, the snack marker, lock persistence/rehydration across restart, and the long-press override. The daemon entrypoint (`cmd/pupcup/main.go`) is **now wired**: it loads config, opens and first-boot-seeds the store (three dogs via the embedded `internal/seed`), constructs the four drivers (real on the Pi, Fakes elsewhere via the build-tag split) plus the state machine, runs the device loop, and shuts down gracefully on SIGINT/SIGTERM with sd_notify readiness + watchdog heartbeats (`internal/systemd`). It builds, vets, unit-tests, cross-compiles to `linux/arm64`, and has been smoke-run on the laptop (seeds 3 dogs on first boot, no re-seed thereafter, clean shutdown). The buttons driver is still press-only, pending the both-edge upgrade the add-in chord depends on (milestone 10.5).

The web shell is now in place (milestone 8): `internal/web` serves the app chrome through an embedded `html/template` base layout, embedded static CSS, a custom 404, and the `/healthz` liveness probe (deriving its fields — uptime, DB size, device-lock state, last button-sourced entry — from the store on demand, with no event-bus subscription), all behind a request-logging middleware; `internal/mdns` advertises the service over multicast DNS as a soft dependency (a registration failure is logged, never fatal). Both are wired into `main.go` and shut down gracefully with the device loop.

The dashboard and dogs-management/photo surfaces landed in milestone 9 (per-dog daily feeding status; dog CRUD with name·color·photo and `/photos/{id}` serving), and the **feedings & snacks CRUD surface landed in milestone 10** — the `/feedings` page (record meal/snack with a retroactive timestamp, inline edit, delete-with-confirm) plus dashboard quick-add, all HTMX-driven (htmx vendored locally, create degrading to PRG without JS). The **illness + stress event pages landed in milestone 11** (`/illness`, `/stress` — date-range events with an "ongoing" toggle and a one-click "set end" action; stress events can be household-wide), the **unified history timeline landed in milestone 12** (`GET /history` — every meal/snack/illness/stress merged newest-first, filterable by dog/type/date-range via plain GET query params; read-only, no JS required), and the **per-dog detail page + eating-quality chart landed in milestone 13** (`GET /dogs/{id}` — a server-rendered SVG stacked-bar chart of full/partial/none meals per day over a 7/30/90-day window, a per-score summary, and a read-only meal/snack history table; the chart helper is the new `internal/web/chart` package). Not yet started: the add-in meal-tags feature end-to-end (schema, domain type, store ranking methods, device `AddInSelect` state, and web tag surfaces), and all deployment artifacts (systemd unit, `deploy.sh`, `bootstrap.sh`, `config.example.yaml`). The §6.5 behavioral questions (lock trigger, meal window, Blue long-press, deferred-commit edge cases) and the photo-serving approach were **resolved on 2026-06-02** and specified in §6.5/§7/§8; the lock-trigger and meal-window refinements (grace timeout, last-meal-timed expiry, snack marker) are now **implemented** (milestone 7.5), while the deferred-commit flow, the add-in chord + `AddInSelect`, and the Blue-long-press both-edge upgrade remain for milestone 10.5. The SQLite durability stance is `synchronous=NORMAL` (no corruption on power loss; the last feeding(s) may be lost on a hard cut and re-added retroactively).

## 1. Goals and non-goals

### Goals
1. Press-and-go logging of meal feedings (Green/Yellow/Red) and snacks (Blue) per dog from the physical device, with sub-second latency from button release to OLED confirmation and DB write.
2. Local-network web app for richer entry (specifics, illness, stress events), editing past entries, retroactive entries, and v1 analytics.
3. Single-device deployment — one Pi, one binary, one SQLite file.
4. "Were the dogs fed?" glanceable answer via the front-edge LED bar after meals.
5. Pure Go, no CGO, cross-compilable from a developer laptop.
6. Pleasant, playful UI — soft pastels, paw-print accents, friendly typography.
7. Future-feature seams: data-trend analytics and Home Assistant integration are anticipated but not built in v1.
8. **Add-in meal tags** — record optional "add-ins" mixed into an otherwise standard meal (e.g. shredded chicken, cheese, freeze-dried liver) both from the web app and from the physical device, so the picky-eater dataset captures *what was added* alongside *how well it was eaten*. Tags are a configurable per-household catalog; a feeding can carry any number of them (web) and is taggable retroactively.

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

## 3. Repository layout — 🟡

> 🟡 **Partly completed** — most `internal/` packages exist and build, including `internal/web/` (the shell plus the milestone-9 dashboard + dogs-management/photo surfaces) and `internal/mdns/`; `cmd/pupcup/main.go` is wired and drives the device + web + mDNS; only `deploy/` does not exist yet. Per-path status is annotated inline below. **Note:** tests are co-located as `<pkg>_test.go` inside each package (no top-level `test/` dir). Web handlers live flat in the `web` package root (`web.go`/`dogs.go`/`photos.go`) rather than the originally-sketched `handlers/` subdir.

```
pupcup/
├── cmd/
│   ├── pupcup/             # main daemon
│   │   └── main.go         # config load, driver wiring, state machine, web server + mDNS, signal handling, sd_notify — ✅
│   └── hwprobe/            # standalone bring-up tools (one tiny main per peripheral) — ✅
│       ├── oled/main.go        # ✅
│       ├── buttons/main.go     # ✅
│       ├── rotary/main.go      # ✅
│       └── neopixel/main.go    # ✅
├── internal/
│   ├── config/             # YAML + env config; one struct, validated on load — ✅
│   ├── clock/              # time.Now wrapper for testability (real & fake) — ✅
│   ├── store/              # SQLite wrappers — schema, migrations, queries — 🟡 (core CRUD ✅; tag methods pending, §5.4)
│   ├── domain/             # types: Dog, Feeding, Snack, FeedTag, IllnessEvent, StressEvent, ButtonColor, Score, FeedKind — 🟡 (FeedTag + Feeding.Tags pending)
│   ├── eventbus/           # typed pub/sub on buffered channel — ✅
│   ├── seed/               # embedded first-boot seed data (seed_data.yaml: dogs + tag list) — ✅
│   ├── systemd/            # pure-Go sd_notify: readiness + watchdog, no-op off systemd — ✅
│   ├── device/
│   │   ├── hostinit/       # idempotent periph.io host.Init wrapper — ✅
│   │   ├── buttons/        # 4-button driver (debounced, periph.io) — 🟡 (press-only; both-edge upgrade pending, §6.1)
│   │   ├── rotary/         # KY-040 Buxton table decoder + button + long-press — ✅
│   │   ├── oled/           # SSD1306 wrapper + screen renderer — 🟡 (AddInSelectScene pending, §6.3)
│   │   ├── neopixel/       # SK6812 pure-Go SPI driver (3-bit WS encoding) — ✅
│   │   └── state/          # device state machine — 🟡 (3 modes ✅ & tested & wired into main.go; deferred-commit + AddInSelect pending)
│   ├── web/                # 🟡 shell ✅ (m8) + dashboard/dogs ✅ (m9) + feedings/snacks ✅ (m10) + illness/stress ✅ (m11) + history ✅ (m12) + per-dog detail/chart ✅ (m13); add-in tag surfaces pending (10.5)
│   │   ├── web.go          # Server, routes, methodOverride, dashboard handler, request logging, graceful Serve — ✅
│   │   ├── dogs.go         # dogs CRUD handlers + photo upload validation/save — ✅ (9)
│   │   ├── photos.go       # GET /photos/{id} serve with Clean + dir-prefix guard — ✅ (9)
│   │   ├── templates.go    # embed + per-page parse + buffered render + score/time funcs — ✅
│   │   ├── health.go       # /healthz JSON probe — ✅
│   │   │   # (handlers live flat in the package root — web.go/dogs.go/photos.go — not a handlers/ subdir)
│   │   ├── dogs_detail.go  # GET /dogs/{id} detail: chart days + meal stats + history table — ✅ (13)
│   │   ├── templates/      # *.html — base + dashboard/dogs/feedings/illness/stress/history/dog_detail + 404 ✅; tag-chip partials ⬜ (10.5)
│   │   ├── static/         # app.css ✅ + htmx.min.js ✅; fonts, paw icons ⬜
│   │   └── chart/          # server-rendered SVG helpers (stacked-bar eating quality) — ✅ (13)
│   └── mdns/               # zeroconf wrapper — ✅ (soft-dependency advertiser)
├── deploy/                 # ⬜ not started (no files yet)
│   ├── pupcup.service      # systemd unit
│   ├── deploy.sh           # cross-compile + rsync + restart
│   └── bootstrap.sh        # first-time install on a fresh Pi (creates user, dirs, perms)
├── migrations/             # SQL DDL files, numbered: 0001_init.sql, … — 🟡 (0001 core tables ✅; feed_tags/feeding_tags + seeds pending)
├── go.mod
├── go.sum
├── README.md
├── pupcup_hardware_build.md
└── pupcup_build_plan.md    # (this file)
```

Public surface = `cmd/pupcup/main.go` + the `internal/` packages. Nothing in `pkg/`; this isn't a library.

## 4. Dependencies (locked) — 🟡

> 🟡 **Partly completed** — `periph.io/*`, `modernc.org/sqlite`, `gopkg.in/yaml.v3`, and now `github.com/grandcat/zeroconf` (with `golang.org/x/net`/`miekg/dns` bumped to Go-1.25-compatible releases) are in `go.mod`. The web layer uses stdlib only (`net/http`, `html/template`, `embed`).

| Module | Purpose | Status |
|---|---|---|
| `periph.io/x/conn/v3` + `periph.io/x/host/v3` + `periph.io/x/devices/v3` | GPIO, I²C, SPI, SSD1306 driver | ✅ in go.mod; used by drivers |
| `modernc.org/sqlite` | Pure-Go SQLite (no CGO) | ✅ in go.mod; used by store |
| `github.com/grandcat/zeroconf` | mDNS advertising | ✅ added (milestone 8) |
| `gopkg.in/yaml.v3` | Config file parsing | ✅ in go.mod; used by config |

Standard library: `log/slog` (logging), `html/template`, `net/http`, `embed` (templates + static), `database/sql`. No third-party HTTP framework, no router framework — `net/http` ServeMux (Go 1.22+ method matching) is sufficient.

## 5. Data model — 🟡

### 5.1 Schema (DDL, simplified) — 🟡

> 🟡 **Partly completed** — `dogs`, `feedings`, `snacks`, `illness_events`, `stress_events`, and `device_state` are created in `0001_init.sql` (✅). The live migration additionally carries `dogs.deleted_at` and extra `idx_*_ts` indexes not shown below. `feed_tags`/`feeding_tags`, the reserved Unspecified sentinel, and the seed data are **not yet added**.

All times stored as **UTC** in the DB; presentation layer converts to `America/New_York`. SQLite enforces foreign keys (`PRAGMA foreign_keys = ON`).

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

-- Configurable household catalog of meal add-ins (shredded chicken, cheese, …).
CREATE TABLE feed_tags (
    id              INTEGER PRIMARY KEY,
    name            TEXT NOT NULL,
    is_unspecified  INTEGER NOT NULL DEFAULT 0,  -- 1 = reserved "Other / name later" sentinel
    archived_at     DATETIME,                    -- soft-hide from pickers without losing history
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
-- Names are stored Title-Cased and matched case-insensitively (NOCASE), so
-- "cheese"/"Cheese" can't coexist; unique among live (non-archived) tags only.
CREATE UNIQUE INDEX idx_feed_tags_name ON feed_tags(name COLLATE NOCASE) WHERE archived_at IS NULL;
-- Exactly one reserved sentinel: device "Other" attaches this so the feeding is
-- recorded immediately and surfaced on the web "needs a name" queue.
INSERT INTO feed_tags (id, name, is_unspecified) VALUES (1, 'Unspecified add-in', 1);

-- Many-to-many: a feeding carries zero or more add-in tags.
CREATE TABLE feeding_tags (
    feeding_id      INTEGER NOT NULL REFERENCES feedings(id) ON DELETE CASCADE,
    tag_id          INTEGER NOT NULL REFERENCES feed_tags(id) ON DELETE RESTRICT,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (feeding_id, tag_id)
);
CREATE INDEX idx_feeding_tags_tag ON feeding_tags(tag_id);

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

### 5.2 Domain types (Go) — 🟡

> 🟡 **Partly completed** — every type below except `FeedTag` (and `Feeding.Tags`) is implemented in [internal/domain/domain.go](internal/domain/domain.go) with `Valid()`/`Validate()` helpers. `FeedTag` and the `Tags []FeedTag` field land with the add-in feature.

```go
type Score string  // "full" | "partial" | "none"
type FeedKind string  // "standard" | "nonstandard"
type ButtonColor string // "green" | "yellow" | "red" | "blue"
type Source string // "button" | "web"

type Dog struct { ID int64; Name, AccentColor, PhotoPath string; SortOrder int }
type FeedTag struct { ID int64; Name string; IsUnspecified bool; ArchivedAt *time.Time }
type Feeding struct { ID, DogID int64; TS time.Time; Kind FeedKind; Score Score; Specifics string; Source Source; Tags []FeedTag; DeletedAt, EditedAt *time.Time }
type Snack struct { ID, DogID int64; TS time.Time; Specifics string; Source Source; DeletedAt, EditedAt *time.Time }
type IllnessEvent struct { ID, DogID int64; Start time.Time; End *time.Time; Notes string }
type StressEvent struct { ID int64; DogID *int64; Start time.Time; End *time.Time; Kind, Notes string }
```

### 5.3 Migration strategy — 🟡

> 🟡 **Partly completed** — the runner is done & working in [internal/store/store.go](internal/store/store.go): a `schema_migrations(version, applied_at)` table, lexical-ordered apply from the embedded `migrations.FS`, each migration in its own transaction, and a pre-migration file backup (adding a `wal_checkpoint(TRUNCATE)` before the copy is a pending refinement). The three dogs now seed at the **app layer** on first boot (✅, see below). **Outstanding:** the starter `feed_tags` and the Unspecified sentinel land with the `feed_tags` table in milestone 10.5.

- One numbered SQL file per migration, applied in order on startup.
- A `schema_migrations(version, applied_at)` table tracks applied versions.
- Migrations are forward-only; before any migration runs, a `PRAGMA wal_checkpoint(TRUNCATE)` folds the WAL into the main file and a backup is copied to `pupcup.sqlite.bak.YYYYMMDD-HHMMSS` (a single self-contained file).
- v1 seed data lives in [internal/seed/seed_data.yaml](internal/seed/seed_data.yaml) — three dogs (Riley, Bentley, Bard) and eight starter `feed_tags` (Shredded Chicken, Cheese, Parmesan, Wet Food, Freeze-Dried Liver, Freeze-Dried Beef Patty, Milk, Rice). **Dogs are seeded at the app layer, not via a migration:** `main.go` calls into `internal/seed` on startup and inserts them only when the `dogs` table is empty (idempotent, so a re-deploy never duplicates). This deliberately keeps seed rows out of the always-run migration path — several `store`/`device/state` unit tests open a fresh in-memory DB and assume an empty `dogs` table, which a seed migration would break. Dog ids land at 1–3. The starter `feed_tags` and the reserved `Unspecified add-in` sentinel (`id = 1`, inserted **first** so tag ids are 1 + 2–9) are seeded via the add-in SQL migration in milestone 10.5 (no existing test assumes an empty `feed_tags` table — it doesn't exist yet); `seed.FeedTags()` exposes the same list so that migration derives from one source of truth.

### 5.4 Add-in tag ranking (per-dog) — ⬜

> ⬜ **Not started** — no `feed_tags` schema, `RankedTag` type, or `TagsForDog` method exists yet.

Both the device picker (§6.5) and the web tag-picker order candidate tags by **how
often *this dog* has received each tag**, most-used first. The store exposes:

```go
// TagsForDog returns live (non-archived) tags ranked by this dog's usage.
// Ordering: per-dog use count DESC, then global use count DESC, then name ASC.
// The reserved Unspecified sentinel is excluded from the ranked body — callers
// that need an "Other" affordance append it themselves.
func (s *Store) TagsForDog(dogID int64) ([]RankedTag, error)
```

```sql
SELECT t.id, t.name,
       COUNT(ft.feeding_id) AS dog_uses
FROM feed_tags t
LEFT JOIN feeding_tags ft ON ft.tag_id = t.id
LEFT JOIN feedings f ON f.id = ft.feeding_id
     AND f.dog_id = :dog_id AND f.deleted_at IS NULL
WHERE t.archived_at IS NULL AND t.is_unspecified = 0
GROUP BY t.id
ORDER BY dog_uses DESC, t.name ASC;
```

A never-used tag still appears (count 0, alphabetical), so a newly created tag is
immediately selectable. Ranking is computed fresh per pick — there's no
denormalized counter to keep in sync.

Tag names are normalized to **Title Case** on create/rename (e.g. `shredded chicken` → `Shredded Chicken`) and deduplicated case-insensitively against live tags (the NOCASE unique index in §5.1), so the catalog stays clean whether a tag is added from the device, the web, or the seed file.

## 6. Hardware drivers — 🟡

All drivers are in `internal/device/<name>/`. Hardware bring-up is complete and
these packages already exist as the **known-good baseline** — carry their shape
forward rather than re-deriving it:

- **Build-tag split per driver.** Each device package ships `<name>_linux.go`
  (real, `//go:build linux`), `<name>_stub.go` (every other GOOS), a shared
  interface file, and a `Fake`. This is why the project compiles and unit-tests
  on a macOS laptop while the real drivers only build for the Pi. Don't tag a
  whole feature `linux`-only.
- **Host init.** Everything talks to hardware through `periph.io/x/conn/v3` +
  `periph.io/x/devices/v3`. Call
  [`hostinit.Init()`](internal/device/hostinit/hostinit.go) (wraps `host.Init`,
  idempotent) once before opening any device.
- **Lifecycle.** Drivers expose `Events() <-chan` (buffered, cap 16) and a
  `Close()` guarded by `sync.Once` that closes a `stop` channel, `wg.Wait()`s the
  watcher goroutines, then closes the event channel. Edge loops poll with
  `WaitForEdge(50ms)` so `stop` stays responsive.
- Each driver exposes a small interface plus a `Fake` so tests swap hardware out.

### 6.1 Buttons (`device/buttons`) — 🟡

> 🟡 **Partly completed** — press-only driver + `Fake` done and hardware-verified in [buttons_linux.go](internal/device/buttons/buttons_linux.go). The both-edge (press/release) upgrade below is **pending** — note it changes `ButtonEvent` (adds an `Action`), which is a breaking change to the `Fake`, the existing state-machine tests, and `cmd/hwprobe/buttons`.

```go
type Driver interface {
    Events() <-chan ButtonEvent
    Close() error
}
type ButtonAction string // "press" | "release"
type ButtonEvent struct { Color domain.ButtonColor; Action ButtonAction; TS time.Time }
```

- Uses `periph.io/x/conn/v3/gpio` with `PullUp` and **both-edge** detection:
  falling edge → `press` (re-read and confirm `Low` to reject bounce, per the
  known-good [buttons_linux.go](internal/device/buttons/buttons_linux.go)
  pattern); rising edge → `release` (re-read and confirm `High`).
- **Why both edges (changed from press-only):** the add-in chord (§6.5) needs to
  know a meal button is *still held* when Blue is tapped, and the deferred-commit
  rule fires a feeding on meal-button **release**. Emitting press+release lets the
  state machine track the live held-button set itself; the driver stays stateless
  beyond debounce.
- Software debounce: 25 ms quiet period after each edge before the next event on
  the same pin is accepted.
- One goroutine per button reading edges, plus a fan-in to the `Events()` channel.

### 6.2 Rotary encoder (`device/rotary`) — ✅

> ✅ **Completed** — table-based Buxton decoder, push-button, and short/long-press all implemented and hardware-verified in [rotary_linux.go](internal/device/rotary/rotary_linux.go); `Fake` present.

```go
type Driver interface {
    Events() <-chan Event
    Close() error
}
type EventKind string  // "rotate_cw" | "rotate_ccw" | "press_short" | "press_long"
type Event struct {
    Kind    EventKind
    TS      time.Time
}
```

- **Table-based Buxton full-step decoder — a hard requirement, not a choice.**
  This KY-040 emits a spurious reverse detent under the naive "sample DT on
  CLK's falling edge" method, so that approach was tried and abandoned (commit
  `c05a58e`). The proven driver
  ([rotary_linux.go](internal/device/rotary/rotary_linux.go)) instead watches
  **both** CLK and DT for any edge (`gpio.WaitForEdge`, 50 ms poll so `stop`
  stays responsive), re-reads both lines into a 2-bit `(CLK<<1 | DT)` pinstate,
  and steps a 7-state Gray-code table that emits exactly one event per detent
  and falls back to `R_START` on any invalid (bounce) transition.
- **Bounce rejection is structural**, so there is no time-based debounce on the
  rotation lines — `rotary_debounce_ms` is retained in config for compatibility
  but the decoder ignores it. A direction-inversion flag (`invert`) swaps CW/CCW
  at emit time.
- `SW`: short press = release after ≥ 25 ms and before the long-press threshold;
  long press = held ≥ 1.5 s. Long-press emits `press_long` exactly once on
  cross-threshold (while still held), and the subsequent release is silent — no
  repeat, no trailing short-press.

### 6.3 OLED (`device/oled`) — 🟡

> 🟡 **Partly completed** — driver, embedded fonts, and the `DogSelectorScene`/`LockedSummaryScene`/`SnackModeScene`/`SplashScene` renderers are implemented in [oled_linux.go](internal/device/oled/oled_linux.go) + [scenes.go](internal/device/oled/scenes.go) and hardware-verified. **Pending:** `AddInSelectScene`/`AddInChoice` (add-in feature). _(The scene signatures below include the `Now time.Time` field the renderer uses.)_

```go
type Renderer interface {
    Render(scene Scene) error
    Close() error
}
type Scene interface { isScene() }   // sealed
type DogSelectorScene struct { Dog domain.Dog; Index, Total int; Now time.Time }
type LockedSummaryScene struct { Entries []SummaryEntry; LockedUntil time.Time; Now time.Time }
type SnackModeScene struct { Dog domain.Dog; Remaining time.Duration; AlreadyRecorded []int64 }
type AddInSelectScene struct { Dog domain.Dog; Score domain.Score; Choices []AddInChoice; Index int }
type SplashScene struct { Message string; Now time.Time }
// SummaryEntry: DogName string; Score domain.Score; HasSnack bool (the * snack marker, §6.5).

// AddInChoice is one row in the add-in picker. The final row is always the
// synthetic "Other (name later)" entry (IsOther = true) that attaches the
// reserved Unspecified sentinel tag.
type AddInChoice struct { TagID int64; Label string; IsOther bool }
```

- Uses `periph.io/x/devices/v3/ssd1306` over I²C — opens the bus by number
  (`i2creg.Open("1")`) and constructs via `ssd1306.NewI2C`. Note: that
  constructor **hardcodes address `0x3C`**; the `oled_addr` config value is
  validated on load but not consumed by the driver, so a `0x3D`-jumpered panel
  would need a code change, not just config.
- 128×64 framebuffer; full-redraw on scene change, partial-redraw for clock ticks.
- Embedded bitmap fonts (small, medium, large) compiled into the binary via `//go:embed`. "Large" = 24-px tall sans for dog names; "small" = 8-px for status.
- Anti-burn-in: invert pixels every 24 hours; periodically nudge content position by 1 px.

### 6.4 NeoPixel (`device/neopixel`) — custom pure-Go SPI driver — ✅

> ✅ **Completed** — pure-Go SPI 3-bit-encoding driver + `Fake` implemented in [neopixel_linux.go](internal/device/neopixel/neopixel_linux.go), unit-tested, and hardware-verified through the level shifter.

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
- Encode each WS data bit as **3 SPI bits**: `1` → `0b110`, `0` → `0b100`. At 2.4 MHz this yields the canonical 800 kHz / 1.25 µs WS timing within tolerance. Pixels are emitted in **G-R-B byte order, MSB first** (the SK6812/WS2812 wire order) — a swapped order is the usual cause of the "green when you expected red" symptom in the §8.6 bring-up test.
- Per-frame buffer = `nLEDs × 24 bits × 3 SPI bits / 8 bits per byte = nLEDs × 9` bytes (72 bytes for 8 LEDs). Plus a 50 µs reset gap (≥ 30 zero bytes at 2.4 MHz).
- The driver pre-allocates the buffer in `NewStrip(n)` to avoid per-frame allocations.
- Animation goroutine ticks at 30 Hz only when an animation is active (idle-off state writes a blank frame and stops ticking).

### 6.5 Device state machine (`device/state`) — 🟡

> 🟡 **Design resolved (2026-06-02); implementation advancing.** `Idle`, `LockedSummary`, and `SnackMode` are implemented, unit-tested, and **now constructed and run by `cmd/pupcup/main.go`** (milestone 7.5 — the device loop runs end-to-end). Covered: dog scroll, meal record + auto-advance, all-dogs-fed lock, the **15-minute grace-timeout lock path** (locks a partial session; un-fed dogs left unrecorded), **last-meal-timed expiry** (`locked_until = last_meal_ts + meal_lock`), the **snack marker** (`SummaryEntry.HasSnack`, queried from the snacks table per render so web-added snacks also show), persistence/rehydration across restart, and the long-press override. **Outstanding (milestone 10.5):** the deferred-commit meal flow, the add-in chord (both directions) + `AddInSelect` state, Blue long-press in `LockedSummary` (the live code still enters SnackMode on a plain Blue tap — see the TODO in `handleLockedButton`; it depends on the both-edge buttons upgrade), and `EditOverride` naming.

States:
- **`Idle`** — OLED shows the per-dog selector; rotary scrolls dogs (wraps at the
  ends). Meal buttons (G/Y/R) use a **deferred commit**, and Blue is disambiguated
  by whether a meal button is involved:
  - **Meal-button press** opens an in-memory *pending feeding* for the **selected
    dog** (score = button color). Nothing is written yet. **While a meal button is
    held, rotary input is ignored** so the pending stays bound to the dog selected
    at press time (resolution 4a).
  - **A second meal button pressed while one is held → last wins:** the pending
    feeding's score becomes the most-recently-pressed color (4b).
  - **Meal-button release with no Blue involved** → commit the pending feeding as a
    plain standard meal, advance to the next un-fed dog, and re-check the lock
    condition below. Latency is measured release-to-confirmation, so a normal quick
    tap still feels instant.
  - **Add-in chord (either order)** → transition to `AddInSelect` carrying the
    pending feeding: *hold a meal button, then tap Blue* **or** *hold Blue, then
    tap a meal button* (the symmetric reverse chord — both open the add-in picker,
    4c). The pending feeding's dog = selected dog, score = the meal color.
  - **Blue alone** (a Blue press→release with no meal button pressed during the
    hold) → enters `SnackMode`.
- **`AddInSelect`** — OLED shows the per-dog-ranked add-in picker (§5.4) for the
  pending feeding via `AddInSelectScene`, plus a trailing **"Other (name later)"**
  row. Rotary scrolls; **rotary SW short-press selects** the highlighted choice:
  - a real tag → commit the pending feeding with that one tag attached;
  - "Other" → commit with the reserved *Unspecified* sentinel attached (surfaced on
    the web "needs a name" queue);
  - inactivity (`addin_idle_seconds`, default 30) → commit the pending feeding
    **untagged** as a standard meal, so a walk-away never loses the meal record.
  In every case the machine then advances to the next un-fed dog and **re-checks
  the lock condition** (resolution 4d). One tag per chord by design — for multiple
  add-ins, edit on the web.
- **`LockedSummary`** — entered when the current meal completes. **Completion rule
  (resolutions 1 & 2):**
  - the moment **all dogs have a recorded feeding** in the current session → lock
    immediately; **otherwise**
  - a **grace timer** (`meal_complete_grace_minutes`, default 15), **reset on each
    new feeding**, runs; if it elapses with at least one dog fed and the meal still
    incomplete → lock with the partial session and **leave the un-fed dog(s) with
    no record** (they are added retroactively on the web).
  - In both cases the lock **expiry is timed from the last recorded meal**:
    `locked_until = last_meal_ts + meal_lock_minutes` (240 min). There is no
    separate "meal window" — the grace timer subsumes it (resolution 2).
  - OLED shows the per-dog summary (fed dogs badged G/Y/R, un-fed dogs "–", plus a
    `*` snack marker per the note below); LED bar glows solid green; meal buttons
    are ignored.
- **`SnackMode`** — entered by **Blue alone in Idle** (above), or by
  **press-and-hold of Blue ≥ `long_press_ms`** while in LockedSummary. **Blue
  long-press is detected on the 1-second tick** (resolution 3): the machine
  timestamps Blue's press from the §6.1 both-edge event and, on each tick, if Blue
  is still held past the threshold while in LockedSummary, enters SnackMode once;
  the eventual Blue release is then consumed silently (no trailing snack). OLED
  shows "SNACK — pick dog"; rotary picks a dog; tapping Blue records a snack. Exits
  on all-dogs-recorded or `snack_mode_idle_seconds` (60) inactivity, returning to
  the prior state.
- **`EditOverride`** — long-press of rotary SW while in LockedSummary; clears
  `device_state.locked_until_utc`; immediately returns to Idle.

**Blue disambiguation:** Blue alone (no meal button pressed during its hold) is the
snack button; Blue together with a meal button — pressed in **either order** — is
the add-in modifier. The held-button set the machine tracks from the §6.1
press/release events is what tells the two apart, so there's no timing race.

**Snack marker (v1):** the LockedSummary list shows a `*` next to any dog that also
has a snack recorded since the current lock began (`SummaryEntry.HasSnack`,
populated from the snacks table — shipped in v1 per the resolved clarifications).

State transitions emit `domain.LockChanged` events and persist to `device_state`.
The pending feeding, the in-progress meal session, and the grace timer are
**in-memory only** and are not persisted: a reboot mid-meal drops them (any
already-recorded feedings remain in the DB and on the web; the user re-taps those
not yet recorded). On startup the lock state is reconstructed from `device_state`,
so a reboot during the lock window resumes correctly.

## 7. Web layer — 🟡

> 🟡 **Shell + dashboard + dogs + feedings/snacks + illness/stress + history + per-dog detail/chart done (milestones 8–13)** — `internal/web` serves the app shell (a `net/http` ServeMux router, an embedded `html/template` base layout + top-nav, embedded static assets incl. **vendored `htmx.min.js`**, a custom 404, the `/healthz` probe, request-logging middleware with `component=web.handler`/`latency_ms`, and a 5 s graceful-shutdown `Serve`), the **dashboard** (`GET /` — per-dog "fed today / last fed + score" for the local day, **plus quick-add buttons**), **dogs management** (`/dogs` list/create/update with name·color·photo, soft-delete, and `GET /photos/{id}` photo serving with a Clean + dir-prefix guard; uploads validated to JPEG/PNG · ≤`photo_max_kb` · ≤`photo_max_px`), and the **feedings & snacks CRUD** surface (`/feedings` — record meal/snack with a retroactive `datetime-local` picker, merged recent-activity list, inline edit, `hx-confirm` delete; HTMX fragment endpoints for `feedings`/`snacks` create/edit/update/delete), and the **illness & stress event** surfaces (`/illness`, `/stress` — date-range events with an "ongoing" toggle and a one-click "set end" `PATCH`; stress events are per-dog or whole-household; calendar dates kept in UTC so they don't drift by timezone), and the **unified history timeline** (`GET /history` — every meal/snack/illness/stress merged newest-first, filterable by dog/type/date-range via plain GET query params; read-only), and the **per-dog detail page** (`GET /dogs/{id}` — a server-rendered SVG eating-quality stacked-bar chart over a 7/30/90-day window via a plain `?window=` GET param, a per-score summary, and a read-only meal/snack history table; the chart helper is the new `internal/web/chart` package). Plain-form `DELETE`/`PATCH` on dogs work via a `methodOverride` middleware, and feeding/snack/illness/stress **create degrades to a PRG without JavaScript**; the HTMX edit/delete/set-end interactions issue the real verbs directly. Add-in tag chips on `/feedings` are 10.5. The web layer is plain **request/response** — it does not subscribe to the event bus or push live updates in v1 (resolution 6); pages reflect state on load/refresh, and `/healthz` reads the store on demand.

### 7.1 Routes — 🟡

> Live now (milestones 8–10): `GET /` (the real **dashboard** — per-dog "fed today / last fed + score" status for the current local day, **with quick-add buttons** that `hx-post` a feeding for *now* and swap the refreshed dog card), the **feedings & snacks** set (`GET /feedings`; `POST /feedings` & `POST /snacks` create; `GET /feedings/{id}` & `GET /snacks/{id}` row fragments for edit-Cancel; `GET /…/{id}/edit` inline edit forms; `PATCH /…/{id}` update; `DELETE /…/{id}` soft-delete — all HTML-fragment responses driven by vendored htmx, with create degrading to PRG without JS), the **dogs-management** set — `GET /dogs`, `POST /dogs` (create), `POST /dogs/{id}` (update name·color·photo, multipart), `DELETE /dogs/{id}` (soft-delete, store-guarded against deleting a dog with feeding history), `GET /photos/{id}` (serve from `photo_dir` via `http.ServeFile` with a Clean + dir-prefix guard; 404 if none) — plus `GET /healthz`, `GET /static/*` (embedded), and the custom 404 catch-all. **Photo uploads** are validated to JPEG/PNG, ≤`photo_max_kb`, ≤`photo_max_px` on the largest edge, rejected otherwise. The few non-GET/POST verbs (`DELETE`/`PATCH`) are reachable from plain HTML forms via a `methodOverride` middleware (hidden `_method` field), so the app works without JavaScript; HTMX issues the real verbs in milestone 10. The feedings/snacks/illness/stress sets (milestones 10–11), the read-only **`GET /history`** timeline — filterable by `dog`/`type`/`from`/`to` query params (milestone 12) — and the per-dog **`GET /dogs/{id}`** detail page — eating-quality SVG chart + summary + history table, windowed by a `?window=7|30|90` GET param (milestone 13) — are all live.

| Method | Path | Purpose |
|---|---|---|
| GET | `/` | Dashboard: per-dog "last fed at HH:MM + score" status, quick-add buttons |
| GET | `/dogs` | List + manage dogs |
| POST | `/dogs` | Create a dog |
| GET | `/dogs/{id}` | Per-dog detail: history table + analytics chart |
| POST | `/dogs/{id}` | Update name / color / photo (multipart; JPEG/PNG, ≤150 KB, ≤320×320, rejected if larger) |
| DELETE | `/dogs/{id}` | Soft-delete (only when zero non-deleted feedings) |
| GET | `/photos/{id}` | Serve a dog's photo from photo_dir via `http.ServeFile` (Clean + dir-prefix guard); 404 if none |
| GET | `/feedings` | Recent feedings — reverse-chronological list optimized for quick add-in tagging (inline tag chips + picker) |
| POST | `/feedings` | Add (HTMX) — supports retroactive timestamp |
| GET | `/feedings/{id}/edit` | Edit form (HTMX dialog) — includes the add-in tag multiselect + create-on-the-fly field |
| PATCH | `/feedings/{id}` | Update (incl. full set of attached tags) |
| DELETE | `/feedings/{id}` | Soft-delete |
| POST | `/feedings/{id}/tags` | Attach a tag (HTMX chip add); body may name a new tag, created if absent |
| DELETE | `/feedings/{id}/tags/{tagID}` | Detach a tag (HTMX chip remove) |
| GET | `/tags` | Manage the add-in tag catalog |
| POST | `/tags` | Create a tag |
| PATCH | `/tags/{id}` | Rename a tag |
| DELETE | `/tags/{id}` | Archive a tag (soft; preserves history on past feedings) |
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

### 7.2 Templates — 🟡

> Base layout (now with a Today/Feedings/Illness/Stress/History/Dogs top-nav + a deferred `htmx.min.js` script) plus `dashboard.html`, `dogs.html`, `dog_detail.html`, `feedings.html`, `illness.html`, `stress.html`, `history.html`, and `404.html` are embedded and rendering (`home.html` was replaced by `dashboard.html` in milestone 9). Each page is parsed together with `base.html` into its own template set so the `{{define "content"}}` blocks don't collide; `templates.render` executes the full `base` layout into a buffer first (a template error becomes a 500, not a half-written 200), and `templates.fragment` executes a single named `{{define}}` block for HTMX partial responses. `dashboard.html` defines `dog_status_card` (rendered both in the grid and standalone for quick-add); `feedings.html` defines `entry_row`, `new_entry` (row + OOB empty-state removal), `feeding_edit`, `snack_edit`, and `dog_options`; `illness.html`/`stress.html` mirror that shape with `*_row`/`*_new`/`*_edit` (the ongoing row embeds an inline "set end" form; the stress edit form carries a who-select incl. *Whole household*); `history.html` is read-only — a plain GET filter form (dog/type/from/to) over a single `history_row` partial that branches on the entry type; `dog_detail.html` is read-only too — a dog header with 7/30/90-day window tabs (plain `?window=` links), the eating-quality chart (SVG injected as `template.HTML` from `internal/web/chart`), a per-score summary stat grid, and a meal/snack history table. Helper funcs registered: `fmtTime`/`fmtDate`/`fmtClock`/`fmtInputDateTime`/`fmtEventDate`/`fmtInputDate`, `scoreLabel`, `scoreClass` (the `fmtEventDate`/`fmtInputDate` pair format calendar-only dates in UTC so they don't drift by timezone). The tag-chip partials arrive with 10.5.

- `internal/web/templates/` is embedded via `//go:embed` so no template files are deployed alongside the binary.
- Layout: `base.html` defines the page chrome; pages extend with `{{define "content"}}…{{end}}`.
- Partials: `feeding_row.html`, `dog_card.html`, `chart_eating_quality.html`, `confirm_modal.html`, `tag_chips.html` (the attached-tag chip list + add control), `tag_picker.html` (per-dog-ranked suggestions) — these are HTMX swap targets.
- The recent-feedings page (`GET /feedings`) flags any feeding still carrying the *Unspecified* sentinel with a "name this add-in" affordance, so device-side "Other" selections get resolved on the web in one click.
- Helper funcs registered in a single `funcMap`: `formatTime`, `formatDate`, `pawIcon`, `accentColor`, `score…` etc.

### 7.3 Charts (server-side SVG) — ✅

> Done (milestone 13). `internal/web/chart` exposes `StackedBar(days, width, height) template.HTML` — pure functions, no JS chart libs. Bars are plain `<rect>`s stacked full (bottom) → partial → none (top); colors come from app.css via `.bar-full`/`.bar-partial`/`.bar-none`, so the chart shares the palette and never drifts from the badges. The SVG is inlined (not an `<img>`) so it inherits that CSS and scales via `viewBox`. Cumulative segment edges avoid gaps/overlaps; a few evenly-spaced x-axis date labels keep wide windows legible; per-bar `<title>`s give hover counts; an all-zero window renders a friendly empty-state instead of dividing by zero. Unit-tested (empty-state, per-score segments, segment omission, well-formed SVG).

- Chart helper in `internal/web/chart/` produces SVG strings. No JS chart libs.
- v1 charts:
  - **Stacked bar over time** — full / partial / none counts per day for a window (7, 30, 90 days). Live on each dog detail page (`GET /dogs/{id}`); the helper is reusable for a future dashboard summary card.
- The helper is intentionally minimal (rectangles + text + a few axes); future charts (heatmaps, scatter) live in the same package.

### 7.4 Style — 🟡

> The embedded `app.css` is in place with the palette, 16-px rounded card/chip shapes, header/footer chrome, the top-nav, the **dashboard status grid** (responsive `auto-fill` cards with score-colored badges/dots + avatars + quick-add buttons), the **dogs-management** UI (add/edit-via-`<details>`/delete forms, color picker + suggested-palette hint, flash banners, buttons), and the **feedings** UI (responsive meal/snack add-form grids, the recent-activity list with score-colored entry dots and snack badges, and the inline edit-form grid), and the **per-dog detail** UI (window-tab pills, the eating-quality chart with palette-sourced bar colors + legend, the summary stat-tile grid, and the read-only history table). **Outstanding:** the self-hosted Quicksand woff2 (the shell falls back to a warm system/`ui-rounded` stack for now).

- Single hand-written CSS file `app.css` (~3 KB compressed) embedded.
- Palette:
  - Background `#FFF7F0` (warm cream)
  - Accent green `#A8D8B9`, yellow `#F8D8A0`, red `#F2A6A1`, blue `#A8C8F8`
  - Text `#3A332B`
  - Subtle border/shadow `rgba(58,51,43,0.08)`
- Typography: Quicksand 400/500/700 (subset, ~18 KB woff2), self-hosted from `/static/fonts/`.
- Component shapes: 16-px rounded corners, 1-px borders, soft drop shadows. Generous padding. Paw-print accent SVG used as bullet/divider/empty-state.
- Density: dashboard prioritizes "today" cards above the fold; deeper pages use a single column at ≤ 720 px max-width and a two-column dashboard grid above 720 px.

### 7.5 mDNS — ✅

- `internal/mdns/` wraps `grandcat/zeroconf` to advertise:
  - Service: `_http._tcp`
  - Instance/hostname: from `mdns_hostname` (default `pupcup`, resolvable as `pupcup.local`)
  - Port: derived from `listen` via `mdns.PortFromListen` (so dev on `:8080` advertises the right port)
  - TXT record: `version=<build>`
- Registration is a **soft dependency**: `Advertiser.Run(ctx)` logs and continues if registration fails (common on a dev laptop with no multicast permission), then blocks on `ctx` and withdraws the advertisement on shutdown — so mDNS trouble never takes down the daemon.
- Relies on the on-Pi `avahi-daemon` for the `.local` resolver from non-Apple clients. The page header prints the advertised host (`<hostname>.local`) and the dashboard will print the IP at the top as a fallback for clients (some Android setups) where mDNS doesn't resolve.
- Note: `grandcat/zeroconf` v1.0.0 pins an ancient `golang.org/x/net`/`miekg/dns` that fails to link under Go 1.25 (`syscall.recvmsg`); both are bumped to current releases in `go.mod`.

## 8. Configuration — 🟡

> 🟡 **Mostly completed** — the loader, `PUPCUP_*` env overrides, fail-fast validation, and the accessors are implemented in [config.go](internal/config/config.go), **including** `meal_complete_grace_minutes` (`MealCompleteGrace()`), `addin_idle_seconds` (`AddInIdle()`), and `photo_max_kb`/`photo_max_px`, plus `ButtonDebounce()`/`RotaryDebounce()` helpers. **Outstanding:** only the shipped `deploy/config.example.yaml` file (deferred to milestone 14).

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
  green: 21
  yellow: 16
  red: 12
  blue: 20
rotary_pins:
  clk: 17
  dt: 27
  sw: 22
button_debounce_ms: 25
rotary_debounce_ms: 5    # retained for config compat; UNUSED by the rotary
                         # driver — the Buxton decoder rejects bounce structurally
long_press_ms: 1500

# Behavior
meal_lock_minutes: 240
meal_complete_grace_minutes: 15  # after the first dog is fed, wait this long for
                                 # the rest before locking with a partial meal
                                 # (un-fed dogs get no record); reset by each feeding
snack_mode_idle_seconds: 60
addin_idle_seconds: 30    # AddInSelect walk-away timeout; commits the pending
                          # meal untagged rather than losing it
default_feed_kind: standard
mdns_hostname: pupcup

# Web / photos
photo_max_kb: 150         # reject dog-photo uploads larger than this
photo_max_px: 320         # reject uploads wider/taller than this (no downscaler in v1)
```

> Implementation note: these fields are implemented on the [config.Config](internal/config/config.go)
> struct — `meal_complete_grace_minutes` (`MealCompleteGrace()`, default 15, `>= 0`;
> 0 = lock as soon as the next 1 s tick sees the meal still incomplete),
> `addin_idle_seconds` (`AddInIdle()`, default 30, `>= 1`), and
> `photo_max_kb` / `photo_max_px` (defaults 150 / 320, `>= 1`). For laptop dev
> without root, override the privileged port and the production DB path:
> `PUPCUP_LISTEN=:8080 PUPCUP_DB_PATH=./pupcup-dev.sqlite`.

Validated on load; missing required values fail fast with a clear error message.

## 9. Logging and observability — 🟡

> 🟡 **Partly completed** — structured `log/slog` JSON to stdout is wired in [main.go](cmd/pupcup/main.go) with a `version` field, and `sd_notify` readiness + `WatchdogSec` heartbeats are implemented in [internal/systemd](internal/systemd/notify.go) and wired into the daemon (a safe no-op off systemd). The `/healthz` endpoint is **now implemented** ([internal/web/health.go](internal/web/health.go)) returning the JSON below, and the web request-logging middleware emits `component=web.handler` + `latency_ms`. **Outstanding:** extending the `latency_ms` convention to the remaining handlers as they're built.

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

## 10. Deployment — ⬜

> ⬜ **Not started** — `deploy/` does not exist yet (no `deploy.sh`, `bootstrap.sh`, `pupcup.service`, or `config.example.yaml`). The README quick-start was updated to defer these to this milestone (laptop dev runs with `PUPCUP_LISTEN=:8080`).

### 10.0 Provisioning prerequisites — ✅ (OS-level, set at bring-up)

> The app trusts the **system clock** (`time.Now()`) and never reads the DS1307 directly. Correct timestamps with **no network** depend on the OS image having the **`rtc-ds1307` device-tree overlay enabled and `hwclock` syncing on boot** — already configured during hardware bring-up (the RTC is kernel-claimed; it shows `UU` on an I²C scan). See [pupcup_hardware_build.md](pupcup_hardware_build.md). If a future re-image drops the overlay, offline feedings carry a wrong time until NTP recovers — re-enable the overlay rather than adding app-side clock handling.

### 10.1 Cross-compile + ship — ⬜

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

### 10.2 systemd unit — ⬜

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

> First-deploy check: the unit relies on `SupplementaryGroups=gpio i2c spi` for hardware access (no `PrivateDevices`, so `/dev` stays visible). After install, confirm the device nodes are reachable as the `pupcup` user — e.g. `sudo -u pupcup test -r /dev/i2c-1 && sudo -u pupcup test -r /dev/gpiochip0 && sudo -u pupcup test -r /dev/spidev0.0`.

### 10.3 First-time bootstrap — ⬜

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

## 11. Testing strategy — 🟡

### 11.1 Unit tests (laptop, fast) — 🟡

> 🟡 **Partly completed** — `store`, `device/state` (incl. grace-timeout partial lock, last-meal-timed expiry, grace reset per feeding), `domain`, `config`, `eventbus`, `neopixel`, `seed`, `systemd`, and `web` have passing tests; `go test ./...` is green and fast. `web` covers the shell/healthz, the dashboard, dogs CRUD + photo upload/serve + traversal guard, (m10) feedings/snacks create·edit·update·delete over HTMX — htmx fragment vs PRG branching, retroactive-timestamp TZ math, quick-add card rendering, validation-error retargeting, and recent-activity merge ordering — (m11) illness/stress create·edit·update·set-end·delete — the household-vs-dog split, the ongoing toggle, end-before-start validation, and the calendar-date no-timezone-shift guard — (m12) the unified `/history` timeline — merged-kind rendering, dog/type filtering, feed-instant date-range bounds, the illness/stress overlap-window test, and the empty/no-dogs state — and (m13) the per-dog `/dogs/{id}` detail page (404 for unknown/non-numeric/soft-deleted ids, chart+stats+table render, window-range filtering across 7/30/90 days, empty-window state) plus the `internal/web/chart` stacked-bar unit tests (empty-state, per-score segments, segment omission, well-formed SVG). **Outstanding:** the add-in tag coverage listed below (10.5).

- `store/`: open an in-memory SQLite, run migrations, exercise CRUD + soft-delete + filters. Add-in coverage: tag create/rename/archive (names normalized to Title Case, deduped case-insensitively), attach/detach on a feeding, `TagsForDog` per-dog ranking order (used > unused, count desc, alphabetical tiebreak), and that archiving a tag preserves it on historical feedings.
- `domain/` and `device/state/`: pure-Go state-machine tests with a fake clock and fake bus. Add-in coverage: deferred commit (press→release with no Blue commits a plain meal), the hold-meal + tap-Blue chord enters `AddInSelect`, rotary-select attaches the tag and advances, "Other" attaches the Unspecified sentinel, and the `addin_idle_seconds` timeout commits untagged. Assert Blue-alone (no meal held) still routes to `SnackMode`. Also cover: the symmetric reverse chord (hold Blue + tap meal) enters `AddInSelect`; last-wins on overlapping meal buttons; the 15-min grace timeout locks with a partial session (un-fed dogs unrecorded) and times expiry from the last meal; Blue long-press in LockedSummary enters SnackMode on the tick; and the all-fed re-check fires after an `AddInSelect` commit.
- `web/handlers/`: spin up `httptest.Server` against the in-memory store; assert HTML fragments returned by HTMX endpoints, including tag chip add/remove and the "name this add-in" affordance on Unspecified-tagged feedings.
- Goal: `go test ./...` runs in < 5 s.

### 11.2 Hardware integration tests (on-device) — ✅

> ✅ **Completed** — all four `cmd/hwprobe` tools exist and every peripheral is confirmed working on the board.

- The `cmd/hwprobe/` tools (one per peripheral: `buttons`, `neopixel`, `oled`, `rotary`) double as integration tests during build. Re-run them after any wiring change.
- A combined `cmd/hwprobe/all` that runs every probe in sequence could be added later; today each probe is run individually.

### 11.3 End-to-end UAT (manual checklist) — ⬜

See § 13.

## 12. Implementation milestones

Suggested execution order. Each milestone ends in a runnable, demonstrable artifact. **This list is the primary progress ledger — update the status marker on each item as it lands, and refresh the Progress Summary above when a major component completes.**

1. ✅ **OS + hardware probes** — Pi provisioned per the hardware doc; `hwprobe` programs verify each peripheral. _(Hardware confirmed working; all four probes present.)_
2. ✅ **OLED hello** — `device/oled` package + a "Hello PupCup" splash on boot. _(Package, fonts, renderer, and `SplashScene` done; the on-boot splash itself lands when `main.go` is wired.)_
3. ✅ **Button + rotary events** — `device/buttons` and `device/rotary` packages emit events on stdout. _(Both drivers + probes done; buttons are press-only pending the §6.1 both-edge upgrade.)_
4. ✅ **NeoPixel pure-Go driver** — solid colors, walking pixel, smooth fade. Verifies SPI 3-bit encoding and level shifter. _(Done & hardware-verified.)_
5. ✅ **SQLite store** — schema, migrations, CRUD, soft-delete, in-memory test DB. _(Done; tag store methods arrive in 10.5.)_
6. ✅ **Domain types + event bus** — typed pub/sub. _(Done; `FeedTag`/`Feeding.Tags` arrive in 10.5.)_
7. ✅ **Device state machine** — wires hardware events to bus + store + OLED scenes + LED states. End of this milestone: pressing buttons records feedings; the OLED reflects state; LEDs glow green for 4 hours after a meal. _(Idle/LockedSummary/SnackMode implemented, unit-tested, and now constructed/run by `cmd/pupcup/main.go` via milestone 7.5. Deferred-commit + `AddInSelect` are added in 10.5; on-device behavior is confirmed in the §13 UAT.)_
7.5. ✅ **Wire the daemon (`main.go`)** — constructs config→store→bus→clock, the real drivers on the Pi (Fakes elsewhere via the build-tag split), and the device state machine; runs the loop with graceful shutdown (SIGINT/SIGTERM) and `sd_notify` readiness + watchdog (`internal/systemd`). Folds in the resolved lock-model refinements (15-min grace timeout, last-meal-timed expiry, snack marker) and idempotent first-boot dog seeding (`internal/seed`). _(Done: builds/vets/tests green, cross-compiles to linux/arm64, and smoke-runs on the laptop — seeds 3 dogs once, no re-seed on restart, clean SIGTERM shutdown. On-device end-to-end behavior is verified in the §13 UAT.)_
8. ✅ **Web shell** — `net/http` server, base template, 404, healthz, embedded statics, mDNS. _(Done: `internal/web` (router, embedded base layout + `app.css`, custom 404, `/healthz`, request logging) and `internal/mdns` (soft-dependency `_http._tcp` advertiser) run alongside the device loop in `main.go` with graceful shutdown. Builds/vets/tests green, arm64 cross-compiles, and a live laptop smoke test served healthz/home/static/404 and shut down cleanly on SIGTERM.)_
9. ✅ **Dashboard + dogs management** — list of today's status, manage dogs (name/color/photo). _(Done: `GET /` renders per-dog "fed today / last fed + score" status for the local day; `internal/web/dogs.go` + `photos.go` add the full dogs CRUD — create/update (name·color·photo, multipart), soft-delete (store-guarded against history), and `GET /photos/{id}` served from `photo_dir` with a Clean + dir-prefix guard. Photo uploads validated to JPEG/PNG · ≤`photo_max_kb` · ≤`photo_max_px`. Plain-form DELETE via a `methodOverride` middleware (no JS). `Store.ActiveEntryCounts` backs the "can't delete — has history" affordance. Builds/vets/tests green (new dashboard/dogs/photo/method-override/traversal tests), arm64 cross-compiles, and a live laptop smoke served the dashboard, created/updated/deleted dogs, round-tripped a photo upload→`/photos/{id}` (byte-exact, `image/png`), rejected an invalid color, and shut down cleanly on SIGTERM.)_
10. ✅ **Feedings & snacks CRUD** — add via HTMX, retroactive timestamp picker, edit, soft-delete with confirm. _(Done: the `/feedings` page records meals & snacks with a `datetime-local` retroactive picker and lists recent activity (feedings + snacks merged, newest-first) with inline edit + `hx-confirm` delete; `POST|GET|PATCH|DELETE /feedings/{id}` and `/snacks/{id}` return HTML fragments. Dashboard `GET /` gained per-dog quick-add (Full/Some/None) that swaps the refreshed `dog_status_card`. htmx is **vendored** as `static/htmx.min.js` (no CDN); validation errors retarget a banner via `HX-Retarget`/`HX-Reswap`; meal/snack create **degrades to a PRG redirect without JS** (edit/delete are htmx-only). Web entries are `source=web`; edits set `edited_at`. Builds/vets/tests green (new feedings/snacks/quick-add/htmx-error/merge-order tests), arm64 cross-compiles, and a live laptop smoke exercised every flow. Add-in tag chips/picker on this page are milestone 10.5.)_
10.5. ⬜ **Add-in tags** — `feed_tags`/`feeding_tags` migration + store methods (incl. `TagsForDog`); tag catalog page (`/tags`); recent-feedings page (`/feedings`) and feeding edit dialog with chip add/remove; device-side deferred-commit + `AddInSelect` state, `AddInSelectScene` rendering, and the both-edge buttons driver upgrade that the chord depends on. (Depends on milestone 7 for the device state machine and milestone 10 for the web feeding surfaces.) End of this milestone: holding a meal button and tapping Blue tags a meal from the device, and that tag is visible/editable on the web.
11. ✅ **Illness + stress events** — `/illness` & `/stress` HTMX pages: date-range form, "ongoing" toggle, one-click set-end action, inline edit, delete-with-confirm; stress events are per-dog or whole-household; calendar dates kept TZ-stable. Create degrades to PRG without JS. Tested + arm64 + live smoke.
12. ✅ **History page** — unified, filterable timeline. _(Done: `GET /history` merges every recorded activity — meals, snacks, illness, stress — into one newest-first timeline, filterable by **dog**, **entry type**, and **date range** via plain GET query params (`dog`/`type`/`from`/`to`), so it works without JavaScript and is bookmarkable. Read-only (the per-kind pages own edits). Feedings/snacks are date-range-filtered in the store (loc-aware instant bounds); the few illness/stress rows are pulled per-dog and overlap-filtered in memory (`[start, end]` ∩ window, `end` nil = ongoing/open). A household stress event surfaces for any single-dog filter; each row shows a neutral category tag + per-kind detail. New `history.go` + `history.html` (one `history_row` partial branching on type); nav gained a History link. Builds/vets/tests green (merged-timeline, dog-filter, type-filter, date-range-bounds, event-overlap, no-dogs tests); arm64 cross-compiles; live laptop smoke exercised every filter.)_
13. ✅ **Per-dog detail + chart** — eating-quality stacked-bar SVG. _(Done: `GET /dogs/{id}` renders a server-rendered SVG stacked-bar chart of full/partial/none meals per day over a selectable 7/30/90-day window (plain `?window=` GET param, default 30, works without JS), a per-score summary (meal total, counts + whole-percent shares, snack count), and a read-only meal/snack history table (newest-first; edits stay on `/feedings`, with links out to `/feedings` and `/history?dog=`). The chart helper is the new minimal `internal/web/chart` package — `StackedBar(days, w, h) template.HTML`, no JS libs, palette-sourced bar colors via CSS classes, all-zero windows show an empty-state. Feedings bucket by household-local day; the window reuses the store's instant bounds. Unknown/non-numeric/soft-deleted dog ids 404. Reached from the dog name on the dashboard and the dogs-management list. New `dogs_detail.go` + `dog_detail.html` + `internal/web/chart`; nav keeps the Dogs tab active. Builds/vets/tests green (chart unit tests + detail 404/render/window-filter/empty-window tests); arm64 cross-compiles; live laptop smoke exercised the links, chart/stats/table, every window + bad-window fallback, and the 404s.)_
14. ⬜ **systemd unit + deploy.sh + bootstrap.sh** — production install on the Pi.
15. ⬜ **UAT pass + polish** — run the checklist; fix any rough edges; finalize the README.

## 13. Verification / UAT checklist — ⬜

> ⬜ **Not started** — every item below is gated on a deployed device + the web app; none have been run yet. Check them off during the UAT pass (milestone 15).

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
- [ ] Lock timing: after the last dog's meal, the LED turns solid green and the 4-hour lock is timed **from that last meal**.
- [ ] Grace timeout: feed only 2 of 3 dogs and wait `meal_complete_grace_minutes` (15) — the device locks with the two recorded; the third has **no record** (added later on the web).
- [ ] Add-in chord: select a dog, **press and hold GREEN**, and while holding tap BLUE — OLED shows the add-in picker ranked by that dog's history with an "Other (name later)" row at the bottom. Scroll with the dial, short-press the rotary on a tag — the feeding commits with exactly that tag and advances to the next dog.
- [ ] Reverse chord: select a dog, **press and hold BLUE**, then tap GREEN/YELLOW/RED — the same add-in picker opens; selecting a tag commits the meal with it (identical to the hold-meal-then-Blue chord).
- [ ] Add-in "Other": run the chord again and pick "Other" — the feeding commits and later shows up on the web `/feedings` page flagged "name this add-in".
- [ ] Add-in walk-away: start the chord, then do nothing for `addin_idle_seconds` — the pending meal commits **untagged** (not lost) and advances.
- [ ] A normal quick tap (press + release, no Blue) still commits a plain meal with no perceptible delay.
- [ ] LED bar transitions to solid green.
- [ ] OLED transitions to the locked summary scene listing each dog with its score.
- [ ] Within 4 hours, taps on G/Y/R are ignored (silent).
- [ ] Tap of BLUE alone in the locked period is ignored; press-and-hold of BLUE (≥ 1.5 s, caught within ~1 s on the tick) enters snack mode; pick a dog; tap BLUE; snack recorded and a `*` appears next to that dog in the summary; the snack-mode scene exits after 60 s of inactivity, returning to the locked summary.
- [ ] Long-press rotary SW (≥ 1.5 s) clears the lock; LED fades out; OLED returns to selector.
- [ ] After 4 hours the lock auto-clears.

### 13.3 Web app
- [ ] Dashboard shows each dog's **last-fed time + score** (e.g. "last fed 8:13 AM — full") accurately.
- [ ] Edit a past feeding (change timestamp + score) — change is reflected in history and in the dog's detail chart.
- [ ] Soft-delete a feeding — entry disappears from the table; chart updates.
- [ ] Add a retroactive feeding via web with a custom timestamp — appears in the correct chronological position.
- [ ] On `/feedings`, add two add-in tags (e.g. shredded chicken + cheese) to one meal via chips — both persist and show on the dog's history; remove one — it detaches without affecting the other.
- [ ] Create a brand-new tag from the feeding edit dialog's create-on-the-fly field — it's reusable on the next feeding and appears in `/tags`.
- [ ] Resolve a device "Other" feeding: open the flagged feeding, replace Unspecified with a real tag — the flag clears.
- [ ] Archive a tag in `/tags` — it disappears from new pickers but remains visible on the past feedings that used it.
- [ ] Manage dogs / tag ranking: a tag used often for dog A sorts above unused tags in A's device and web picker; that ranking is per-dog (not mirrored to dog B).
- [x] Add an illness event spanning yesterday → today with an "ongoing" end; later set the end date.
- [x] Add a stress event for the whole household.
- [ ] Manage dogs: rename, change accent color, upload a photo. Photo appears on dashboard; an over-limit image (>150 KB or >320×320) is rejected with a clear message.

### 13.4 Resilience
- [ ] Reboot Pi with home wifi turned off. Press a button — feeding recorded with a sane timestamp from DS1307. Re-enable wifi — `pupcup.local` reachable; the entry appears.
- [ ] Run `./deploy.sh` while the device is in the locked state — service restarts, OLED briefly blanks, LED bar resumes green, lock state preserved (verified via dashboard).
- [ ] Pull power abruptly mid-write — on next boot, no DB corruption (SQLite WAL mode) and the state machine resumes correctly. (A feeding pressed in the final moment before the cut may be lost — `synchronous=NORMAL` — and is re-addable on the web; the accepted durability stance.)
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
