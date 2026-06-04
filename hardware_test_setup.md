# PupCup — Hardware Test Setup

End-to-end instructions for going from a **freshly-flashed Raspberry Pi 3B+** to **running all five hardware probes**. Use this any time you re-flash the SD card or set up a new device.

This document focuses on the *test setup* only. For wiring, BOM, and acceptance criteria see [pupcup_hardware_build.md](docs/pupcup_hardware_build.md). For the overall architecture see [pupcup_build_plan.md](docs/pupcup_build_plan.md).

---

## At a glance

| Step | Where | Time |
|---|---|---|
| 1. Flash OS + first boot | Laptop + Pi | ~10 min |
| 2. Install Pi dependencies | Pi (over SSH) | ~5 min |
| 3. Cross-compile probes | Dev machine | <1 min |
| 4. Upload binaries | Dev machine → Pi | <1 min |
| 5. Run probes | Pi | as long as you want |

Probes available today (all under [cmd/hwprobe/](cmd/hwprobe/)):

| Probe | What it exercises |
|---|---|
| [`hwprobe-buttons`](cmd/hwprobe/buttons/main.go) | 4 momentary buttons (green/yellow/red/blue) on GPIO 12/16/5/6 (header pins 32/36/29/31) |
| [`hwprobe-neopixel`](cmd/hwprobe/neopixel/main.go) | 8× SK6812 LED stick over SPI0 MOSI (via 74AHCT125) |
| [`hwprobe-lcd`](cmd/hwprobe/lcd/main.go) | GC9A01 240×240 round RGB LCD on SPI1 (default display) |
| [`hwprobe-oled`](cmd/hwprobe/oled/main.go) | SSD1306 128×64 OLED on I²C `0x3C` (OLED variant only) |
| [`hwprobe-rotary`](cmd/hwprobe/rotary/main.go) | KY-040 rotary encoder + push switch on GPIO 17/27/22 |

---

## 1. Prerequisites

### On your dev machine (macOS / Linux)
- **Go 1.25+** — `go version` to confirm (the module declares `go 1.25.6` in [go.mod](go.mod)).
- **SSH client** (built into macOS/Linux).
- **Raspberry Pi Imager** — https://www.raspberrypi.com/software/

### On the Pi
- **Raspberry Pi 3B+** with the perfboard build wired up per [pupcup_hardware_build.md §4](docs/pupcup_hardware_build.md).
- microSD card, ≥ 16 GB, A1/A2 rated.
- USB-C PD trigger module set to **5V / 3A**.
- Network: same wifi SSID as your dev machine (mDNS resolution needs L2 connectivity).

---

## 2. First-time Pi setup

### 2.1 Flash the SD card

1. Open **Raspberry Pi Imager**.
2. **Choose OS** → *Other general-purpose OS* → **Raspberry Pi OS Lite (64-bit)** (Debian 13 "Trixie" base).
3. Click the gear / **Edit Settings**:
   - **Hostname**: `pupcup`
   - **Username**: `scotty`
   - **Password**: set one
   - **Enable SSH**, paste your **public key** (so you don't need a password every time)
   - **Configure wireless LAN**: SSID + password + country
   - **Locale**: timezone `America/New_York`, keyboard `us`
4. Write the SD, eject it, insert into the Pi, plug in 5V power.
5. Wait ~60 seconds for first boot.

### 2.2 First SSH in

From your dev machine:

```sh
ssh scotty@pupcup.local
```

If mDNS doesn't resolve (Android phones and some corporate networks struggle), find the Pi's IP on your router admin page and use that instead.

### 2.3 System updates + base packages

```sh
sudo apt update && sudo apt full-upgrade -y
sudo apt install -y i2c-tools gpiod spi-tools git avahi-daemon
sudo systemctl enable --now avahi-daemon
```

What each provides:
- `i2c-tools` — `i2cdetect` for bus scans
- `gpiod` — `gpioget` / `gpiomon` for raw GPIO testing
- `spi-tools` — `spi-pipe` for SPI sanity checks
- `avahi-daemon` — keeps `pupcup.local` resolvable

### 2.4 Configure `config.txt` for the display + SPI buses

SPI0 (the NeoPixel bus) is always required. The display config depends on which
panel you built:

- **Default GC9A01 round LCD** — needs the auxiliary SPI1 bus plus a pinned core
  clock so the 240×240 panel runs reliably. This build uses **no I²C at all**.
- **OLED variant** — needs I²C bus 1 instead (and the `i2c-dev` module so
  `/dev/i2c-1` appears).

Append the lines for **your** build to `/boot/firmware/config.txt`, under the
`[all]` section:

```
# Always (NeoPixel on SPI0)
dtparam=spi=on

# Default GC9A01 round LCD — creates /dev/spidev1.0 and pins the aux-SPI clock
dtoverlay=spi1-1cs
core_freq=400
core_freq_min=400

# OLED variant instead — drop the three GC9A01 lines above and use:
# dtparam=i2c_arm=on
```

> **Warning:** `config.txt` does **not** support inline `# comments` on the same
> line as a directive, and the lines must sit under the `[all]` section — a
> stray inline comment or a directive placed under a filter block silently
> failed a real install. Put the comments on their own lines as shown above.

For the OLED variant, also load the `i2c-dev` module so the bus shows up:

```sh
echo i2c-dev | sudo tee /etc/modules-load.d/i2c-dev.conf
```

> **Timekeeping:** there is no RTC. `systemd-timesyncd` handles the clock — NTP
> when online, and a persisted timestamp (`/var/lib/systemd/timesync/clock`)
> advanced on boot when offline, so a cold boot never starts at 1970.
> `fake-hwclock` ships **masked** on Raspberry Pi OS (timesyncd replaces it) —
> leave it masked; do not enable or remove it.

### 2.5 Add the `scotty` user to hardware groups

This is what lets the probes run **without `sudo`** after a fresh login:

```sh
sudo usermod -aG gpio,i2c,spi scotty
```

### 2.6 Reboot and verify

```sh
sudo reboot
```

After it comes back:

```sh
ssh scotty@pupcup.local
groups                # expect: scotty ... gpio i2c spi
timedatectl           # expect: System clock synchronized: yes
ls /dev/spidev1.0     # GC9A01 build: must exist (the spi1-1cs overlay)
sudo i2cdetect -y 1   # OLED variant only: expect 0x3C
```

On the OLED variant, if `i2cdetect` shows no `0x3C`, **stop here** — you have a
wiring problem, not a software one. See
[pupcup_hardware_build.md §9](docs/pupcup_hardware_build.md) for OLED footguns. On the
default GC9A01 build there is no I²C, so skip the `i2cdetect` check and confirm
`/dev/spidev1.0` exists instead.

---

## 3. Build the probes (on your dev machine)

The Pi 3B+ is **64-bit ARM** (Cortex-A53), so cross-compile to `linux/arm64`. Run from the repo root:

```sh
cd /Users/scotty/code/webapp_projects/pupcup

GOOS=linux GOARCH=arm64 go build -o /tmp/hwprobe-buttons  ./cmd/hwprobe/buttons
GOOS=linux GOARCH=arm64 go build -o /tmp/hwprobe-neopixel ./cmd/hwprobe/neopixel
GOOS=linux GOARCH=arm64 go build -o /tmp/hwprobe-lcd      ./cmd/hwprobe/lcd
GOOS=linux GOARCH=arm64 go build -o /tmp/hwprobe-oled     ./cmd/hwprobe/oled
GOOS=linux GOARCH=arm64 go build -o /tmp/hwprobe-rotary   ./cmd/hwprobe/rotary
```

Or all at once:

```sh
for p in buttons neopixel lcd oled rotary; do
  GOOS=linux GOARCH=arm64 go build -o /tmp/hwprobe-$p ./cmd/hwprobe/$p
done
```

> No `CGO_ENABLED=0` flag needed — the hardware drivers (`periph.io/x/host/v3` and friends) are pure Go.

---

## 4. Upload to the Pi

```sh
scp /tmp/hwprobe-* scotty@pupcup.local:/tmp/
```

Binaries land in `/tmp/` on the Pi. They're stateless — re-copy any time you rebuild.

---

## 5. Run the probes

SSH to the Pi:

```sh
ssh scotty@pupcup.local
```

All five probes accept `--config <path>` for a YAML config file. Omit it to use the **defaults** baked into [internal/config/config.go](internal/config/config.go) — those defaults already match the wiring in [pupcup_hardware_build.md §3](docs/pupcup_hardware_build.md), so you can just run them bare.

Recommended order: easiest to confirm first, hardest last.

### 5.1 Buttons

```sh
/tmp/hwprobe-buttons
```

- Listens for **30 seconds** (override with `--timeout 1m`).
- Press each colored button. Each press prints a line like `[2026-05-28T19:42:11Z] green`.
- **Pass**: every color produces an event.
- **Fail**: nothing prints → check wiring at the screw terminals; one button silent → that GPIO is shorted or the contact is dead.

### 5.2 NeoPixel LED bar

```sh
/tmp/hwprobe-neopixel
```

Runs to completion (about 8 seconds):

1. Solid red → green → blue → white at low brightness (1s each)
2. Walking pixel (yellow-ish), 3 laps
3. Smooth green fade in/out

- **Pass**: all 8 pixels light, no flicker, walking pixel passes cleanly along the strip.
- **Fail (first pixel wrong color)**: usually a 74AHCT125 problem — see [pupcup_hardware_build.md §4.4](docs/pupcup_hardware_build.md).
- **Fail (flicker / random pixels)**: missing ground tie or the 470 Ω series resistor.

### 5.3 GC9A01 round LCD (default display)

```sh
/tmp/hwprobe-lcd
```

Exercises the 240×240 round panel:

1. Solid fill cycle: red → green → blue → white
2. A test pattern to confirm orientation and full color range

- **Pass**: each solid color fills the whole circle cleanly and the test pattern
  renders right-side up.
- **Fail (blank screen)**: confirm `/dev/spidev1.0` exists (the `spi1-1cs`
  overlay) and re-check the DC (BCM25) / RST (BCM24) wiring and SPI1 SCLK/MOSI
  (BCM21/BCM20).
- **Fail (wrong colors / swapped channels)**: a driver byte-order issue, not
  hardware — flag for a follow-up fix.

### 5.4 OLED display (OLED variant only)

```sh
/tmp/hwprobe-oled
```

Cycles through four scenes (splash → dog selector → locked summary → snack mode), 2 seconds each.

- **Pass**: all four scenes render legibly, right-side up.
- **Fail (blank screen)**: confirm `i2cdetect -y 1` shows `0x3C`. If absent, swap SDA/SCL or check the address jumper on the back of the OLED.
- **Fail (upside down)**: that's a driver flip — flag for a follow-up fix, hardware is fine.

### 5.5 Rotary encoder

```sh
/tmp/hwprobe-rotary
```

- Listens for **60 seconds** (override with `--timeout 2m`).
- Turn the dial clockwise: expect `cw` events. Counter-clockwise: `ccw`. Press the shaft: `press` then `release`.
- **Pass**: both directions register, click registers.
- **Fail (one direction silent)** — this is a known issue from the previous bring-up. Before suspecting the [internal/device/rotary/](internal/device/rotary/) driver, isolate at the OS level:

  ```sh
  gpiomon -c gpiochip0 --bias=pull-up --edges=both 17 27 22
  ```

  Turn slowly each way. If only one of CLK (17) / DT (27) toggles → wiring fault or flaky encoder. If both toggle but the Go probe still only sees one direction → driver decode bug.

---

## 6. Lower-level debugging (skip Go entirely)

When a probe fails, drop a layer down before debugging code:

```sh
# I²C bus scan (OLED variant only) — should show 0x3C
sudo i2cdetect -y 1

# Read button GPIOs directly — pressed = 0 ("inactive"), released = 1 ("active").
# Note: libgpiod v2 (Debian 13 Trixie) requires the chip via `-c`.
gpioget -c gpiochip0 --bias=pull-up 12 16 5 6

# Watch rotary edges in real time
gpiomon -c gpiochip0 --bias=pull-up --edges=both 17 27 22

# Confirm SPI is enabled (won't echo, but must not error)
echo -ne '\xAA\x55\xAA\x55' | spi-pipe -d /dev/spidev0.0 -s 2400000 | xxd
```

If any of those misbehave, the problem is **hardware or kernel config**, not application code.

---

## 7. Optional: use a YAML config

If you ever need to override pins, debounce timings, or I²C address, drop a YAML file on the Pi and pass `--config`:

```yaml
# /home/scotty/pupcup.yaml
display: gc9a01            # gc9a01 (default round LCD) | oled

# GC9A01 round LCD on SPI1 (used when display: gc9a01)
lcd_spi_device: /dev/spidev1.0
lcd_dc_pin: 25
lcd_rst_pin: 24

# SSD1306 OLED on I²C (used when display: oled)
i2c_bus: 1
oled_addr: 0x3C
neopixel_count: 8

button_pins:
  green: 12
  yellow: 16
  red: 5
  blue: 6

rotary_pins:
  clk: 17
  dt: 27
  sw: 22

button_debounce_ms: 25
rotary_debounce_ms: 5
long_press_ms: 1500
```

```sh
/tmp/hwprobe-buttons --config /home/scotty/pupcup.yaml
```

The full list of available keys (with defaults) is in the `Default()` function of [internal/config/config.go](internal/config/config.go). All keys also accept `PUPCUP_<UPPER_NAME>` environment-variable overrides.

---

## 8. What "all green" looks like

You're done when:

- [ ] GC9A01 build: `/dev/spidev1.0` exists — or OLED variant: `i2cdetect -y 1` shows `0x3C`
- [ ] `hwprobe-buttons` registers all 4 colors
- [ ] `hwprobe-neopixel` runs cleanly end-to-end, no flicker
- [ ] `hwprobe-lcd` fills red/green/blue/white and renders the test pattern (or, on the OLED variant, `hwprobe-oled` shows all four scenes legibly)
- [ ] `hwprobe-rotary` registers both rotation directions **and** the press

Once all five are checked, hardware is ready for the full application (see [pupcup_build_plan.md](docs/pupcup_build_plan.md)).

---

## 9. Future improvements to this doc

Things that will get added as the project evolves:

- A `deploy/deploy.sh` script to replace the manual cross-compile + scp loop (already referenced in [README.md](README.md) but not yet written).
- A `deploy/config.example.yaml` sample file.
- An `hwprobe-all` umbrella probe that runs every test in sequence with pass/fail summary.
- Automated tests against a simulated GPIO backend so we can catch driver regressions on the dev machine before flashing.
