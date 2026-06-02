# PupCup — Hardware Test Setup

End-to-end instructions for going from a **freshly-flashed Raspberry Pi Zero 2W** to **running all four hardware probes**. Use this any time you re-flash the SD card or set up a new device.

This document focuses on the *test setup* only. For wiring, BOM, and acceptance criteria see [pupcup_hardware_build.md](pupcup_hardware_build.md). For the overall architecture see [pupcup_build_plan.md](pupcup_build_plan.md).

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
| [`hwprobe-buttons`](cmd/hwprobe/buttons/main.go) | 4 momentary buttons (green/yellow/red/blue) on GPIO 21/16/12/20 (header pins 40/36/32/38) |
| [`hwprobe-neopixel`](cmd/hwprobe/neopixel/main.go) | 8× SK6812 LED stick over SPI MOSI (via 74AHCT125) |
| [`hwprobe-oled`](cmd/hwprobe/oled/main.go) | SSD1306 128×64 OLED on I²C `0x3C` |
| [`hwprobe-rotary`](cmd/hwprobe/rotary/main.go) | KY-040 rotary encoder + push switch on GPIO 17/27/22 |

---

## 1. Prerequisites

### On your dev machine (macOS / Linux)
- **Go 1.25+** — `go version` to confirm (the module declares `go 1.25.6` in [go.mod](go.mod)).
- **SSH client** (built into macOS/Linux).
- **Raspberry Pi Imager** — https://www.raspberrypi.com/software/

### On the Pi
- **Raspberry Pi Zero 2W** with the perfboard build wired up per [pupcup_hardware_build.md §4](pupcup_hardware_build.md).
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

### 2.4 Enable I²C and SPI

```sh
sudo raspi-config nonint do_i2c 0
sudo raspi-config nonint do_spi 0
```

### 2.5 Configure the DS1307 RTC

Append these three lines to `/boot/firmware/config.txt`:

```
dtparam=i2c_arm=on
dtparam=spi=on
dtoverlay=i2c-rtc,ds1307
```

Then remove the fake hardware clock so Linux talks to the real one:

```sh
sudo apt remove -y fake-hwclock
sudo systemctl disable --now fake-hwclock
sudo update-rc.d -f fake-hwclock remove
```

### 2.6 Add the `scotty` user to hardware groups

This is what lets the probes run **without `sudo`** after a fresh login:

```sh
sudo usermod -aG gpio,i2c,spi scotty
```

### 2.7 Reboot and verify

```sh
sudo reboot
```

After it comes back:

```sh
ssh scotty@pupcup.local
groups                # expect: scotty ... gpio i2c spi
timedatectl           # expect: System clock synchronized: yes
sudo i2cdetect -y 1   # expect: 0x3C (OLED), 0x68 or UU (RTC)
```

If `i2cdetect` shows neither address, **stop here** — you have a wiring problem, not a software one. See [pupcup_hardware_build.md §9](pupcup_hardware_build.md) for OLED/RTC footguns.

---

## 3. Build the probes (on your dev machine)

The Pi Zero 2W is **64-bit ARM** (Cortex-A53), so cross-compile to `linux/arm64`. Run from the repo root:

```sh
cd /Users/scotty/code/webapp_projects/pupcup

GOOS=linux GOARCH=arm64 go build -o /tmp/hwprobe-buttons  ./cmd/hwprobe/buttons
GOOS=linux GOARCH=arm64 go build -o /tmp/hwprobe-neopixel ./cmd/hwprobe/neopixel
GOOS=linux GOARCH=arm64 go build -o /tmp/hwprobe-oled     ./cmd/hwprobe/oled
GOOS=linux GOARCH=arm64 go build -o /tmp/hwprobe-rotary   ./cmd/hwprobe/rotary
```

Or all at once:

```sh
for p in buttons neopixel oled rotary; do
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

All four probes accept `--config <path>` for a YAML config file. Omit it to use the **defaults** baked into [internal/config/config.go](internal/config/config.go) — those defaults already match the wiring in [pupcup_hardware_build.md §3](pupcup_hardware_build.md), so you can just run them bare.

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
- **Fail (first pixel wrong color)**: usually a 74AHCT125 problem — see [pupcup_hardware_build.md §4.4](pupcup_hardware_build.md).
- **Fail (flicker / random pixels)**: missing ground tie or the 470 Ω series resistor.

### 5.3 OLED display

```sh
/tmp/hwprobe-oled
```

Cycles through four scenes (splash → dog selector → locked summary → snack mode), 2 seconds each.

- **Pass**: all four scenes render legibly, right-side up.
- **Fail (blank screen)**: confirm `i2cdetect -y 1` shows `0x3C`. If absent, swap SDA/SCL or check the address jumper on the back of the OLED.
- **Fail (upside down)**: that's a driver flip — flag for a follow-up fix, hardware is fine.

### 5.4 Rotary encoder

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
# I²C bus scan — should show 0x3C and 0x68 (or UU)
sudo i2cdetect -y 1

# Read button GPIOs directly — pressed = 0 ("inactive"), released = 1 ("active").
# Note: libgpiod v2 (Debian 13 Trixie) requires the chip via `-c`.
gpioget -c gpiochip0 --bias=pull-up 21 16 12 20

# Watch rotary edges in real time
gpiomon -c gpiochip0 --bias=pull-up --edges=both 17 27 22

# Read the RTC
sudo hwclock -r

# Confirm SPI is enabled (won't echo, but must not error)
echo -ne '\xAA\x55\xAA\x55' | spi-pipe -d /dev/spidev0.0 -s 2400000 | xxd
```

If any of those misbehave, the problem is **hardware or kernel config**, not application code.

---

## 7. Optional: use a YAML config

If you ever need to override pins, debounce timings, or I²C address, drop a YAML file on the Pi and pass `--config`:

```yaml
# /home/scotty/pupcup.yaml
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

- [ ] `i2cdetect -y 1` shows **both** `0x3C` and `0x68` / `UU`
- [ ] `hwprobe-buttons` registers all 4 colors
- [ ] `hwprobe-neopixel` runs cleanly end-to-end, no flicker
- [ ] `hwprobe-oled` shows all four scenes legibly
- [ ] `hwprobe-rotary` registers both rotation directions **and** the press

Once all five are checked, hardware is ready for the full application (see [pupcup_build_plan.md](pupcup_build_plan.md)).

---

## 9. Future improvements to this doc

Things that will get added as the project evolves:

- A `deploy/deploy.sh` script to replace the manual cross-compile + scp loop (already referenced in [README.md](README.md) but not yet written).
- A `deploy/config.example.yaml` sample file.
- An `hwprobe-all` umbrella probe that runs every test in sequence with pass/fail summary.
- Automated tests against a simulated GPIO backend so we can catch driver regressions on the dev machine before flashing.
