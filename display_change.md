# Display change: I²C OLED → round GC9A01 (config-selectable)

Tracking doc for swapping the device display from the 128×64 mono SSD1306 OLED to the
240×240 round RGB GC9A01 SPI LCD, while keeping **both** supported from one binary
(selected by the `display:` config field, default `gc9a01`).

Full plan: `~/.claude/plans/sunny-shimmying-pebble.md`.

## Architecture
- One binary. `display: oled | gc9a01` picks the driver at runtime behind a shared
  `display.Renderer` interface. No git fork.
- GC9A01 hand-rolled over raw SPI (periph has no driver) — mirrors the NeoPixel SPI
  pattern + rotary GPIO pattern. No new dependency.

## Hardware (built)
- LCD: VCC→3.3V, GND→gnd, SCL→SPI1 SCLK (BCM21), SDA→SPI1 MOSI (BCM20), CS→SPI1 CE0
  (BCM18, kernel-asserted), DC→BCM25, RST→BCM24.
- NeoPixel stays on SPI0 (`/dev/spidev0.0`). Buttons Red→BCM5, Blue→BCM6 (off 20/21).
- Pi prereq (`/boot/firmware/config.txt`): `dtoverlay=spi1-1cs`, `core_freq=400`,
  `core_freq_min=400` → creates `/dev/spidev1.0`.

---

## Phase 0 — tracking
- [x] Create `display_change.md`

## Phase 1 — hardware bring-up (standalone, low blast radius)
- [x] `internal/config/config.go`: add `Display`, `LCDSPIDevice`, `LCDDCPin`, `LCDRSTPin`;
      `Default()` (display=gc9a01, /dev/spidev1.0, DC=25, RST=24); button pins Red 21→5,
      Blue 20→6; `validate()` (display ∈ {oled,gc9a01}, LCD pins folded into uniqueness map)
- [x] `internal/device/gc9a01/` — `gc9a01.go` (Config, raw `Driver` iface → renamed `Prober` in
      Phase 2, Fake, rgb565), `gc9a01_linux.go`
      (SPI1 + DC/RST GPIO + reset + init seq + RGB565 fb + chunked flush + FillRGB/DrawTestPattern),
      `gc9a01_stub.go`, `gc9a01_test.go`
- [x] `cmd/hwprobe/lcd/main.go` — color-cycle + test pattern probe
- [x] `deploy/config.example.yaml` — document `display` + `lcd_*` fields, oled alternative, config.txt prereq
- [x] Verify: `go build ./...` ✓, `go test ./...` ✓ (host darwin), `go vet ./...` ✓; cross-compile
      `hwprobe-lcd` + daemon + all packages for linux/arm64 ✓, linux vet ✓
- [x] **User:** ran `hwprobe-lcd` on the Pi — red→green→blue→white→black + quadrant/crosshair
      test pattern all rendered correctly (colors true, no swap needed). Phase 1 hardware-validated.

## Phase 2 — software integration
- [x] `internal/device/display/` — `Renderer` + `Scene` types + `SummaryEntry`/`AddInChoice` +
      `Fake` + `ErrUnavailable`, extracted out of `oled` into a neutral package
- [x] `internal/device/font/` — `font5x7` glyph table + sink-based `DrawText`/`DrawTextScaled`
      (`set(x,y)` callback) + `TextWidth`/`Upper`; both panels share one font
- [x] `oled/*` — `frame()` switches on `display.Scene`; `scenes.go` drawers take `display.*Scene`;
      thin `text.go` wrappers bridge `font` → `mono`; `New` returns `display.Renderer`
- [x] `gc9a01/canvas.go` + `gc9a01/scenes_color.go` — RGB565 `canvas`, `colorFrame` + 5 color
      drawers (chord-clamped centering on the round bezel), `Render` copies canvas→fb→`flush()`
- [x] `cmd/pupcup/main.go` — `switch cfg.Display { gc9a01 | oled }` factory → `display.Renderer`
- [x] Rename consumers: `state.go` (`Deps.OLED`→`Display`, all `oled.*`→`display.*`),
      `hwprobe/oled` (scene literals → `display.*`, still `oled.New`), `hwprobe/lcd`
      (`gc9a01.Prober` assertion for the raw fills), `state_test.go`, `addin_test.go`
- [x] Verify: `go build/vet/test ./...` ✓ (host darwin), linux/arm64 `build`+`vet` ✓,
      cross-compile daemon + `hwprobe-lcd` + `hwprobe-oled` ✓; new `TestColorFrameRendersScenes`
      + `TestParseAccent` cover the color path on the laptop
- [ ] **User:** deploy; confirm live color scenes on the round LCD; regression-check `display: oled`

## Notes / decisions as we go
- RGB565 color order: **correct with `MADCTL 0x36 = 0x08` (BGR bit set)** — Phase-1 fills showed
  true red/green/blue, so the init sequence is kept as-is; no `rgb565` r↔b swap needed.
- SPI clock: 32 MHz (`spiHz`) — Phase-1 test pattern was clean (no tearing/garbage), kept.
- spidev transfer limit: `flush()` chunks the 115,200-byte frame via `conn.MaxTxSize()`
  (spidev bufsiz, typ. 4096); the GRAM write pointer auto-increments across chunks, so the
  image stays contiguous. Confirmed working end-to-end in Phase 1.
- `gc9a01.New` returns `display.Renderer`; the raw `FillRGB`/`DrawTestPattern` bring-up surface
  is the `gc9a01.Prober` interface, recovered by the probe via a type assertion.
