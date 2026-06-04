# PupCup — Hardware Build Plan

A complete, follow-along build for the PupCup appliance: a single-board, headless Raspberry Pi 3B+ that simultaneously serves the PupCup web application **and** acts as the tactile button device with a round LCD, rotary encoder, and an 8-pixel front-edge status bar. This document covers parts, tools, wiring, perfboard layout, OS provisioning, and bring-up tests. Once verified, the assembly drops into a 3D-printed enclosure (designed separately).

The display is **config-selectable from one binary**: the default build drives a 240×240 round RGB GC9A01 SPI LCD (`display: gc9a01`), and the same firmware can instead drive the original 128×64 mono SSD1306 OLED (`display: oled`). `cmd/pupcup/main.go` picks the driver at runtime from `cfg.Display`. This document documents the **default GC9A01 reference build**, with the OLED variant called out where the wiring differs. The default GC9A01 build uses **no I²C at all**.

## 1. Overview

| Aspect | Spec |
|---|---|
| Compute | Raspberry Pi 3B+ |
| OS | Raspberry Pi OS Lite 64-bit, Debian 13 ("Trixie") |
| Inputs | 4 colored momentary buttons (Green / Yellow / Red / Blue), 1 KY-040 rotary encoder |
| Outputs | 240×240 round GC9A01 RGB LCD on SPI1 (default; SSD1306 128×64 mono OLED on I²C optional), 8× SK6812 NeoPixel stick (SPI0) |
| Timekeeping | systemd-timesyncd (NTP online; persists last-known time across cold boots) — no hardware RTC |
| Power | USB-C PD trigger fixed to 5V / 3A → fused 5V rail → Pi + LEDs |
| Network | Wifi only; advertises `pupcup.local` via mDNS |
| Mechanical | Perfboard build with detachable header connectors for the LCD, rotary, NeoPixel, and button harness |

The button device and the web server are the same physical box; there is no separate controller MCU.

## 2. Bill of Materials

Quantities listed are the build quantity (1 device). Suggested suppliers in parentheses; substitute equivalents freely.

### 2.1 Core electronics
| # | Part | Qty | Notes |
|---|---|---|---|
| 1 | Raspberry Pi 3B+ (40-pin header pre-installed) | 1 | The 3B+ ships with the GPIO header. Cortex-A53 quad-core @ 1.4 GHz, 1 GB RAM. |
| 2 | microSD card, ≥ 16 GB, A1 / A2 rated | 1 | SanDisk Industrial or Samsung EVO Plus recommended for write endurance. |
| 3 | 240×240 round GC9A01 RGB SPI LCD module | 1 | Default display. 7-pin SPI module (VCC / GND / SCL / SDA / DC / CS / RST). Hand-rolled driver — see `internal/device/gc9a01`. |
| 3a | *(alt)* 0.96" SSD1306 OLED, I²C, 128×64, monochrome | 0–1 | Optional `display: oled` variant. 4-pin module (Vcc/GND/SCL/SDA). I²C address `0x3C`. Not populated on the default build. |
| 4 | KY-040 rotary encoder module | 1 | 5-pin (CLK / DT / SW / + / GND). On-board 10kΩ pull-ups on CLK/DT/SW. |
| 5 | Adafruit NeoPixel Stick — 8× SK6812 RGBW (or RGB) | 1 | Adafruit p/n 1426 (RGB) or 2868 (RGBW). 5V data, 5V Vcc. |
| 7 | 13 mm momentary push-button — Green | 1 | Round flange, 2 contacts, panel-mount. |
| 8 | 13 mm momentary push-button — Yellow | 1 | Same as above. |
| 9 | 13 mm momentary push-button — Red | 1 | Same as above. |
| 10 | 13 mm momentary push-button — Blue | 1 | Same as above. |
| 11 | 74AHCT125 quad level-shift buffer (DIP-14 or SOIC-14) | 1 | Logic-level shift Pi 3.3V → 5V for SK6812 data line. **Must be `AHCT`** family — not `LVC`/`HC`. |
| 12 | USB-C PD trigger module set to 5V (e.g. CH224K, IP2721, ZY12PDN) | 1 | Output 5V/3A. Verify the 5V config jumper/solder bridge is set. |
| 13 | Inline ATC mini fuse holder + 2A fast-blow fuse | 1 | Protects the 5V rail. Or use a polyfuse (PTC) at 1.5A hold. |
| 14 | 1000 µF / 16V electrolytic capacitor | 1 | Bulk decoupling on the 5V rail near the LEDs. |
| 15 | 0.1 µF ceramic capacitors | 3 | Decoupling near 74AHCT125 (1), the LCD/OLED Vcc (1), and across the LED stick (1). |
| 16 | 470 Ω, 1/8 W resistor | 1 | Series resistor on SK6812 data line (between buffer output and DIN). |
| 17 | 1.8 kΩ – 4.7 kΩ I²C pull-up resistors | 0–2 | **OLED variant only.** Most OLED modules already have these; verify with multimeter and only add if absent. The default GC9A01 build uses no I²C, so none are needed. |
| 18 | Perfboard, ≥ 70 × 90 mm, double-sided plated | 1 | Adafruit "Perma-Proto" half-size or generic 70×90 mm. |
| 19 | 2.54 mm female header strip — 1×7 (LCD) or 1×4 (OLED alt) | 1 | Display connector. GC9A01 needs 7 pins (VCC/GND/SCL/SDA/DC/CS/RST); the OLED variant needs 4 (Vcc/GND/SCL/SDA). |
| 20 | 2.54 mm female header strip — 1×5 | 1 | Rotary encoder connector |
| 21 | 2.54 mm female header strip — 1×3 | 1 | NeoPixel input (5V/GND/Data) |
| 22 | 2.54 mm screw terminal blocks, 2-pin | 5 | One per button (color-coded wire harness in/out) plus one for 5V input. Or use JST-XH if preferred. |
| 23 | Stranded silicone hookup wire — 22 AWG, multi-color | 2 m | Red, black, white, plus colors that match the button harness. |
| 24 | Heat-shrink tubing assortment | — | 2 mm, 4 mm, and 6 mm sizes. |
| 25 | Standoffs + M2.5 screws (Pi mounting) | 4+4 | Optional but recommended for board sandwich. |

### 2.2 Tools
- Soldering iron (temperature-controlled, ~340 °C for leaded, ~370 °C for lead-free) and rosin-core solder.
- Side cutters / wire strippers.
- Digital multimeter (continuity buzzer + DC voltage).
- Tweezers, magnifier or loupe.
- USB-C power supply that supports PD ≥ 5V/3A (any modern phone/laptop charger).
- microSD card reader.
- A laptop/desktop to flash the SD card and SSH in (running rpi-imager).

### 2.3 Approximate cost
Roughly $70–$95 for the full BOM at 2026 pricing, dominated by the Pi 3B+ and the NeoPixel stick. The 74AHCT125 is well under $2, and the GC9A01 round LCD is a few dollars. Buy spares of buttons, the 74AHCT125, and the display module — they are the most likely "oops" parts.

## 3. Pinout reference

All numbers are **BCM (GPIO) numbering**. Header pins use the standard 40-pin Raspberry Pi convention (pin 1 = 3.3V, pin 2 = 5V, etc.).

| Signal | GPIO | Header pin | Direction | Notes |
|---|---|---|---|---|
| 5V power input | — | 2 (and 4) | in | From the 5V rail; both 5V pins should be tied. |
| 3.3V (for LCD/OLED Vcc, level-shifter Vcc-A) | — | 1 (or 17) | in | Feeds the 3.3V side of devices. |
| Ground | — | 6 | star ground | Plus 9, 14, 20, 25, 30, 34, 39 for short returns. |
| SPI1 SCLK (LCD SCL) | GPIO 21 | 40 | output (3.3V) | GC9A01 clock. Claimed by the `spi1-1cs` overlay. |
| SPI1 MOSI (LCD SDA) | GPIO 20 | 38 | output (3.3V) | GC9A01 data in. |
| SPI1 CE0 (LCD CS) | GPIO 18 | 12 | output (3.3V) | Chip-select, kernel-asserted by the overlay. |
| LCD DC (data/command) | GPIO 25 | 22 | output (3.3V) | GC9A01 D/C select. |
| LCD RST (reset) | GPIO 24 | 18 | output (3.3V) | GC9A01 hardware reset. |
| SPI0 MOSI | GPIO 10 | 19 | output (3.3V) | Drives 74AHCT125 input → 5V data → SK6812 DIN |
| Button GREEN | GPIO 12 | 32 | input | Pull-up enabled in software |
| Button YELLOW | GPIO 16 | 36 | input | Pull-up enabled in software |
| Button RED | GPIO 5 | 29 | input | Pull-up enabled in software. Moved off GPIO 21 (now SPI1 SCLK). |
| Button BLUE (snack) | GPIO 6 | 31 | input | Pull-up enabled in software. Moved off GPIO 20 (now SPI1 MOSI). |
| Rotary CLK | GPIO 17 | 11 | input | KY-040 has on-board pull-up; software pull-up also enabled |
| Rotary DT | GPIO 27 | 13 | input | Same as CLK |
| Rotary SW (push) | GPIO 22 | 15 | input | Long-press 1.5 s overrides post-meal lock |

**OLED variant only** — when built with `display: oled`, the GC9A01/SPI1 rows above are unused and the OLED is wired on I²C bus 1 instead:

| Signal | GPIO | Header pin | Direction | Notes |
|---|---|---|---|---|
| I²C SDA | GPIO 2 | 3 | bidirectional | SSD1306 OLED `0x3C` |
| I²C SCL | GPIO 3 | 5 | bidirectional | Same bus |

SPI1 MISO (GPIO 19 / pin 35) is reserved by the `spi1-1cs` overlay but unused — the GC9A01 panel is write-only.

Pins **not** used on the default GC9A01 build (kept free for future expansion or accidental shorts to be obvious): GPIO 2, 3, 4, 7, 8, 9, 11, 13, 14, 15, 23, 26. Avoid using GPIO 14/15 (UART) and GPIO 18 (PWM/audio) — note GPIO 18 is repurposed here as SPI1 CE0 by the overlay.

## 4. Wiring narrative

Build in this order. Verify each step with a multimeter before powering on.

### 4.1 Power rail
1. Set the USB-C PD trigger module to **5V output**. Most modules use a solder bridge or a small DIP/jumper; double-check with a multimeter on a known USB-C PD source before connecting to anything else.
2. PD trigger 5V output → inline 2A fast-blow fuse → bulk node on the perfboard.
3. From the bulk node:
   - 1000 µF / 16V electrolytic across the bulk node and ground (long lead = +5V).
   - Branch to **Pi header pin 2 (5V)** and tie pin 4 to the same rail.
   - Branch to the **SK6812 stick's Vcc pin** (separately, to keep LED current spikes off the Pi power trace as much as a single-board layout allows).
   - Branch to the **74AHCT125 Vcc-B** (the level-shifter's "high" supply).
4. PD trigger GND → board ground plane / star-ground node at Pi header pin 6.
5. Ground returns from LED stick, level-shifter, the LCD (or OLED), rotary, buttons, and Pi all converge at the star-ground node.

### 4.2 3.3V rail
- The Pi supplies 3.3V on header pin 1 (and 17).
- 3.3V → GC9A01 LCD Vcc (or, on the OLED variant, OLED Vcc — most OLED modules accept either 3.3V or 5V; use 3.3V to avoid a level concern), 74AHCT125 Vcc-A.
- 0.1 µF decoupling cap between Vcc and GND at each device.

### 4.3 Display — GC9A01 round LCD on SPI1 (default)
The GC9A01 is driven over **SPI1** (`/dev/spidev1.0`) by a hand-rolled driver
(`internal/device/gc9a01`); periph has no GC9A01 driver. SPI1 is enabled by the
`dtoverlay=spi1-1cs` line in `config.txt` (see §7). Wiring:

- LCD `VCC` → 3.3V (header pin 1/17)
- LCD `GND` → ground (star node)
- LCD `SCL` (clock) → SPI1 SCLK, GPIO 21 (header pin 40)
- LCD `SDA` (MOSI) → SPI1 MOSI, GPIO 20 (header pin 38)
- LCD `CS` → SPI1 CE0, GPIO 18 (header pin 12) — kernel-asserted; do not drive manually
- LCD `DC` (data/command) → GPIO 25 (header pin 22)
- LCD `RST` (reset) → GPIO 24 (header pin 18)
- 0.1 µF decoupling cap between LCD Vcc and GND.

Driver facts worth knowing for bring-up: SPI clock **32 MHz**, Mode0, 8-bit;
framebuffer is **RGB565**; `MADCTL` register `0x36 = 0x08` (BGR bit set) — this
yields true red/green/blue with no software channel swap, confirmed on the
bench. SPI1 MISO (GPIO 19 / pin 35) is reserved by the overlay but unused (the
panel is write-only). No I²C is involved on this build.

### 4.3-alt I²C bus (OLED variant only)
*Skip this section on the default GC9A01 build.* When built with `display: oled`,
wire the SSD1306 on I²C bus 1 instead of the LCD:

- Pi GPIO 2 (SDA, header 3) → SDA on OLED.
- Pi GPIO 3 (SCL, header 5) → SCL on OLED.
- Confirm with a multimeter that SDA and SCL each measure ~3.3V to ground when the Pi is unpowered with the OLED module disconnected (no current flow), then ~3.3V with weak pull-up when powered. If the bus sits low even when idle, you have a short. If it floats (random reading on a high-impedance multimeter), the module is missing pull-ups — add 4.7 kΩ pull-ups from SDA→3.3V and SCL→3.3V.

### 4.4 SPI0 MOSI → 74AHCT125 → SK6812
The NeoPixel stick is driven over **SPI0** (`/dev/spidev0.0`), separate from the
GC9A01's SPI1. The 74AHCT125 is a quad **non-inverting** buffer. We use one of its
four gates.

```
Pi GPIO 10 (3.3V) ──► 74AHCT125 pin 2 (1A) ──► pin 3 (1Y, 5V) ──► 470 Ω ──► SK6812 DIN
                     74AHCT125 pin 1 (1!OE) tied to GND
                     74AHCT125 pins 4, 10, 13 (other gate inputs)         tied to GND
                     74AHCT125 pins 5, 12, 11 (other gate !OE pins)        tied to GND (or VCC; don't leave floating)
                     74AHCT125 pin 14 (Vcc) → 5V rail
                     74AHCT125 pin 7 (GND) → ground
                     0.1 µF cap from pin 14 to pin 7, close to the chip
```

- The 470 Ω resistor protects the first SK6812 against transient overshoot.
- Place the 1000 µF bulk cap **physically near** the LED stick's Vcc/GND pins.
- Ground the AHCT125's unused gate inputs explicitly. Floating CMOS inputs cause oscillation and current draw.

The "AHCT" family is critical — its input threshold (~2.0 V) reliably recognizes the Pi's 3.3 V output as logic-high, while still driving 5 V on the output side. Plain `74HC125` fails this; `74LVC` doesn't pull cleanly to 5 V.

### 4.5 Buttons
Each button connects one terminal to GND and the other to its assigned GPIO. The Pi's internal pull-up is enabled in software, so no external pull-up resistor is needed. Wire harness convention:

| Color | GPIO | Header pin |
|---|---|---|
| Green wire / Green button | 12 | 32 |
| Yellow wire / Yellow button | 16 | 36 |
| Red wire / Red button | 5 | 29 |
| Blue wire / Blue button | 6 | 31 |

RED and BLUE moved off GPIO 21 / GPIO 20 (old header pins 40 / 38) to **GPIO 5 / GPIO 6** (header pins 29 / 31) because the GC9A01's SPI1 now claims GPIO 21 (SCLK) and GPIO 20 (MOSI). Green (GPIO 12, pin 32) and Yellow (GPIO 16, pin 36) are unchanged.

Green and Yellow live on the bottom-right block of the header (pins 32 / 36); Red and Blue now sit on the odd-numbered (left) column at pins 29 / 31. Use 2-pin screw terminals or JST-XH headers so the button panel can be detached for enclosure assembly.

> TODO: confirm the perfboard landing/cable routing for the RED (GPIO 5, pin 29) and BLUE (GPIO 6, pin 31) buttons on the physical GC9A01 build — these two moved off the pin 38/40 corner and their harness run may differ from the original layout.

### 4.6 Rotary encoder (KY-040)
- KY-040 `+` → Pi 3.3V
- KY-040 `GND` → ground
- KY-040 `CLK` → GPIO 17 (header 11)
- KY-040 `DT` → GPIO 27 (header 13)
- KY-040 `SW` → GPIO 22 (header 15)

The KY-040 module already has 10 kΩ pull-ups on the data lines. Software still asserts internal pull-ups for redundancy and to make wiring less fragile if a future encoder is bare.

## 5. ASCII wiring overview

Header pin layout for the **default GC9A01 build** (looking at the Pi from the top, with the SD card slot at the bottom). The left column is odd pins (1,3,5…), the right column is even pins (2,4,6…); pin 1 is top-left. Only labeled signals are used.

```
     odd pins (1,3,5…)               even pins (2,4,6…)
     ┌──────────────────────────────────────────────────────────────┐
  1  │  3.3V →LCD/AHCT-Va        ●  ● 5V →fuse→Pi+LEDs           2  │
  3  │  GPIO2  (free, I²C SDA)   ●  ● 5V (tied to pin 2)         4  │
  5  │  GPIO3  (free, I²C SCL)   ●  ● GND (star)                 6  │
  7  │  GPIO4  (free)            ●  ● UART TX GPIO14 (avoid)     8  │
  9  │  GND                      ●  ● UART RX GPIO15 (avoid)    10  │
 11  │  Rot CLK GPIO17           ●  ● LCD CS / SPI1 CE0 GPIO18  12  │
 13  │  Rot DT  GPIO27           ●  ● GND                       14  │
 15  │  Rot SW  GPIO22           ●  ● GPIO23 (free)             16  │
 17  │  3.3V (alt)               ●  ● LCD RST GPIO24            18  │
 19  │  SPI0 MOSI GPIO10 → AHCT  ●  ● GND                       20  │
 21  │  SPI0 MISO GPIO9 (unused) ●  ● LCD DC GPIO25             22  │
 23  │  SPI0 SCLK GPIO11(unused) ●  ● SPI0 CE0 GPIO8 (unused)   24  │
 25  │  GND                      ●  ● SPI0 CE1 GPIO7 (unused)   26  │
 27  │  ID_SD                    ●  ● ID_SC                     28  │
 29  │  Btn RED   GPIO5          ●  ● GND                       30  │
 31  │  Btn BLUE  GPIO6          ●  ● Btn GREEN  GPIO12         32  │
 33  │  GPIO13 (free)            ●  ● GND                       34  │
 35  │  SPI1 MISO GPIO19 (rsvd)  ●  ● Btn YELLOW GPIO16         36  │
 37  │  GPIO26 (free)            ●  ● LCD SDA / SPI1 MOSI GPIO20 38 │
 39  │  GND                      ●  ● LCD SCL / SPI1 SCLK GPIO21 40 │
     └──────────────────────────────────────────────────────────────┘
```

On the **OLED variant** (`display: oled`): the LCD SCL/SDA/CS/DC/RST and SPI1 MISO
pins above are free, GPIO 2 (pin 3) / GPIO 3 (pin 5) carry I²C SDA/SCL to the OLED,
and the buttons stay on the same GPIO 5/6/12/16 pins.

## 6. Perfboard layout suggestion

A clean layout follows three rules: short signal traces, fat power traces, and detachable peripherals.

```
 Row 1 (top):   [USB-C PD trigger module mounted on edge, 5V output to fuse]
 Row 2:         [Inline fuse]──[1000 µF bulk cap]──[5V rail bus on left edge]
 Row 3:         [3.3V rail bus from Pi pin 1, on right edge]
 Row 4:         [74AHCT125 in DIP socket, with 0.1 µF decoupling cap nearby]
 Row 5:         [470 Ω series resistor → 1×3 header for NeoPixel (5V / GND / Data)]
 Row 6:         [1×7 header for GC9A01 LCD (VCC / GND / SCL / SDA / DC / CS / RST)]
                [— OLED variant: 1×4 header (Vcc 3.3V / GND / SCL / SDA) instead]
 Row 7:         [1×5 header for KY-040 rotary (+ / GND / CLK / DT / SW)]
 Row 8 (bottom):[4× 2-pin screw terminals for button harness, each labeled by color]
                [1× 2-pin screw terminal for 5V external power input]
 Pi mounted to the right edge with M2.5 standoffs; 40-pin GPIO header used as
 the main signal-bridge — short, color-coded jumpers from the Pi header into
 the labeled landings on the perfboard.
```

Track-cuts: if you use a Perma-Proto-style board with continuous power rails, cut the 5V rail between the Pi-side branch and the LED-side branch only if you observe brownouts; otherwise leave continuous and let the bulk cap absorb spikes. **Always** keep the ground plane continuous.

Detachable headers matter: bench testing the LCD (or OLED), rotary, NeoPixel, and buttons one-at-a-time is dramatically easier when you can pull the offending peripheral without unsoldering.

> TODO: confirm the physical perfboard layout for the GC9A01 build — the display landing is now a 1×7 header (was 1×4 for the OLED), and the RED/BLUE button landings moved to the GPIO 5/6 (pin 29/31) side of the header. The row-by-row sketch above has not been re-verified against the actual board.

## 7. Pi OS provisioning

1. Insert the microSD into your laptop. Open **Raspberry Pi Imager**.
2. Choose OS → **Raspberry Pi OS Lite (64-bit)** under "Other general-purpose OS" (Debian 13 "Trixie" base).
3. Click the gear / "Edit settings":
   - **Hostname**: `pupcup`
   - **Enable SSH**, paste your public key.
   - **Set username**: `pupcup` (preferred over `pi`).
   - **Configure wireless LAN**: SSID + password + country.
   - **Locale**: timezone `America/New_York`, keyboard `us`.
4. Write the SD, eject it, insert into the Pi, plug in power.
5. Wait ~60 seconds for first boot. From your laptop:

   ```sh
   ssh pupcup@pupcup.local      # mDNS
   # or fall back to IP from your router's admin page
   ```

6. Update and install dependencies:

   ```sh
   sudo apt update && sudo apt full-upgrade -y
   sudo apt install -y i2c-tools git avahi-daemon
   sudo systemctl enable --now avahi-daemon
   ```

7. Enable SPI (and, only on the OLED variant, I²C):

   ```sh
   sudo raspi-config nonint do_spi 0      # 0 = enable
   # OLED variant only:
   sudo raspi-config nonint do_i2c 0
   ```

8. Configure SPI for the displays. Append to `/boot/firmware/config.txt`:

   ```
   # Default GC9A01 build — enable SPI0 (NeoPixel) and SPI1 (round LCD).
   dtparam=spi=on
   dtoverlay=spi1-1cs        # creates /dev/spidev1.0 for the GC9A01 (CS = BCM18/CE0)
   core_freq=400             # pin SPI1 base clock so 32 MHz LCD timing is stable
   core_freq_min=400

   # OLED variant only — uncomment instead of the spi1 lines above:
   # dtparam=i2c_arm=on       # SSD1306 on I²C bus 1 (also needs the i2c-dev module)
   ```

   The `spi1-1cs` overlay is what exposes `/dev/spidev1.0`; pinning `core_freq`
   keeps the SPI1 base clock fixed so the 32 MHz LCD timing doesn't wander with
   CPU frequency scaling. On the OLED variant, also ensure the `i2c-dev` module is
   loaded so `/dev/i2c-1` appears.

9. *(No hardware RTC.)* Timekeeping is handled by **systemd-timesyncd**, which
   ships and is enabled by default on Raspberry Pi OS — nothing to install.
   Online, it disciplines the clock over NTP. Offline, it persists the
   last-known time to `/var/lib/systemd/timesync/clock` and, on the next boot,
   advances the system clock to that timestamp *before* the network comes up, so
   a cold boot never starts at 1970.

   > **Do not** enable or remove `fake-hwclock`. Raspberry Pi OS ships it
   > **masked** because timesyncd replaces it; leave it masked.

   Any feeding the device records before the first NTP sync after a cold boot is
   flagged **"time unverified"** in the web app, which can be corrected there —
   the timestamp will be approximately right (from the persisted clock) but not
   NTP-confirmed.

10. Reboot, then verify timekeeping and the SPI device nodes:

    ```sh
    sudo reboot
    # … wait …
    ssh pupcup@pupcup.local
    timedatectl                    # Should show America/New_York and "System clock synchronized: yes"
    ls /dev/spidev*                # Expect /dev/spidev0.0 (NeoPixel) and /dev/spidev1.0 (GC9A01)
    ```

11. *(OLED variant only.)* Confirm the OLED is visible on I²C:

    ```sh
    sudo i2cdetect -y 1
    # Expect 0x3C (OLED). The default GC9A01 build uses no I²C — skip this.
    ```

12. Add the `pupcup` user to the necessary hardware groups:

    ```sh
    sudo usermod -aG gpio,spi pupcup     # add 'i2c' too on the OLED variant
    ```

13. Set the hostname pretty-name (optional cosmetic):

    ```sh
    sudo hostnamectl set-hostname pupcup --pretty "PupCup"
    ```

## 8. Hardware bring-up tests

Test each peripheral **in isolation before you commit to the enclosure** — a
crossed wire is far easier to fix on an open board than after everything is
mounted. There are two layers to lean on:

1. **The bundled Go probes** (recommended first check). The repo ships five
   standalone programs under [cmd/hwprobe/](../cmd/hwprobe/) — one per peripheral
   — that exercise the *exact* drivers the application uses, with the wiring
   defaults already baked in. This is the highest-confidence "does it work" test.
2. **Raw userspace tools** (`gpioget`, `gpiomon`, `i2cdetect`, `spi-pipe`) as a
   fallback. When a probe fails, drop a layer down to tell a wiring/kernel
   problem apart from an application bug.

The probes:

| Probe | Build target | Exercises |
|---|---|---|
| `hwprobe-lcd` | `./cmd/hwprobe/lcd` | GC9A01 240×240 round LCD on SPI1 (default display) |
| `hwprobe-oled` | `./cmd/hwprobe/oled` | SSD1306 128×64 OLED on I²C `0x3C` (OLED variant) |
| `hwprobe-neopixel` | `./cmd/hwprobe/neopixel` | 8× SK6812 LED bar over SPI0 (via 74AHCT125) |
| `hwprobe-buttons` | `./cmd/hwprobe/buttons` | 4 buttons on GPIO 12/16/5/6 |
| `hwprobe-rotary` | `./cmd/hwprobe/rotary` | KY-040 encoder + push switch on GPIO 17/27/22 |

They cross-compile to `linux/arm64` (the Pi 3B+ is 64-bit) and copy over with
`scp` — pure Go, no `CGO`. The pattern for each:

```sh
GOOS=linux GOARCH=arm64 go build -o /tmp/hwprobe-lcd ./cmd/hwprobe/lcd
scp /tmp/hwprobe-lcd pupcup@pupcup.local:/tmp/
ssh pupcup@pupcup.local /tmp/hwprobe-lcd
```

> The probes run **without `sudo`** once your login user is in the `gpio`, `spi`,
> (and `i2c`, on the OLED variant) groups — the installer and the manual setup
> both add it. **[hardware_test_setup.md](../hardware_test_setup.md)** is the
> full copy-paste walkthrough — building all five at once, the recommended run
> order, and pass/fail criteria for each. The subsections below summarize what
> each test confirms and what a failure points at.

### 8.1 Display — GC9A01 round LCD (default)
Confirm the SPI nodes exist, then run the LCD probe:
```sh
ls /dev/spidev*                 # expect /dev/spidev0.0 and /dev/spidev1.0
GOOS=linux GOARCH=arm64 go build -o /tmp/hwprobe-lcd ./cmd/hwprobe/lcd
scp /tmp/hwprobe-lcd pupcup@pupcup.local:/tmp/ && ssh pupcup@pupcup.local /tmp/hwprobe-lcd
```
The probe color-cycles the panel (red → green → blue → white) and draws a test
pattern. Colors should be true (no red/blue swap — the driver's `MADCTL
0x36=0x08` BGR setting is already correct). Garbage or tearing usually means
`core_freq` wasn't pinned, or the DC/RST wiring (BCM 25 / BCM 24) is crossed; a
blank panel means `/dev/spidev1.0` is missing (re-check the `dtoverlay=spi1-1cs`
line). *(On the OLED variant, use the OLED probe in [§11](#11-building-the-oled-variant-ssd1306) instead.)*

### 8.2 Timekeeping
```sh
timedatectl                # expect "System clock synchronized: yes" once wifi/NTP is up
cat /var/lib/systemd/timesync/clock   # the persisted last-known time (touched on shutdown/sync)
```
Pull mains power for ten minutes, reconnect **with wifi unavailable**, and check
`date` immediately on boot: it should read roughly the pre-power-off time (from
the persisted clock), *not* 1970. Once wifi returns, `timedatectl` should flip to
synchronized within a minute or two. (There is no hardware RTC to test.)

### 8.3 Buttons
Run the probe, then press each colored button:
```sh
GOOS=linux GOARCH=arm64 go build -o /tmp/hwprobe-buttons ./cmd/hwprobe/buttons
scp /tmp/hwprobe-buttons pupcup@pupcup.local:/tmp/ && ssh pupcup@pupcup.local /tmp/hwprobe-buttons
```
Each press prints a line naming the color. **Pass:** all four colors register.
**Fail:** drop to the raw tool — `libgpiod` v2 (Trixie) needs the chip via `-c`:
```sh
gpioget -c gpiochip0 --bias=pull-up 12 16 5 6     # GREEN YELLOW RED BLUE
```
A held button reads `"<pin>"=inactive`, released reads `"<pin>"=active`. Nothing
at all = wiring at the screw terminals; one silent button = a dead contact or a
shorted GPIO.

### 8.4 Rotary encoder
```sh
GOOS=linux GOARCH=arm64 go build -o /tmp/hwprobe-rotary ./cmd/hwprobe/rotary
scp /tmp/hwprobe-rotary pupcup@pupcup.local:/tmp/ && ssh pupcup@pupcup.local /tmp/hwprobe-rotary
```
Turn clockwise → `cw`, counter-clockwise → `ccw`, press the shaft → `press`/`release`.
**Pass:** both directions and the click register. If only one direction shows,
isolate at the OS level before suspecting the driver:
```sh
gpiomon -c gpiochip0 --bias=pull-up --edges=both 17 27 22
```
If only one of CLK (17) / DT (27) toggles → wiring fault or flaky encoder. If
both toggle but the probe still sees one direction → driver decode bug. (The
driver uses the table-based Buxton decoder specifically because the naive
"sample DT on CLK edge" method emits a spurious reverse on this KY-040.)

### 8.5 NeoPixel LED bar
```sh
GOOS=linux GOARCH=arm64 go build -o /tmp/hwprobe-neopixel ./cmd/hwprobe/neopixel
scp /tmp/hwprobe-neopixel pupcup@pupcup.local:/tmp/ && ssh pupcup@pupcup.local /tmp/hwprobe-neopixel
```
The probe runs solid red→green→blue→white, a walking pixel, then a green fade —
about 8 seconds. **Pass:** all eight pixels light, no flicker, the walking pixel
passes cleanly. **Fail (first pixel wrong color):** suspect the 74AHCT125 wiring
or a too-long data trace. **Fail (flicker / random pixels):** a missing ground
tie or the missing 470 Ω series resistor.

As a pre-wiring sanity check that SPI0 itself is enabled (nothing need be
connected):
```sh
sudo apt install -y spi-tools
echo -ne '\xAA\x55\xAA\x55' | spi-pipe -d /dev/spidev0.0 -s 2400000 | xxd
```
You won't get the bytes back without a MISO→MOSI loopback, but the command
running without error confirms the bus exists.

Once all of these pass, the hardware is ready for the application. The OLED
variant's display check is covered in [§11](#11-building-the-oled-variant-ssd1306).

## 9. Safety, troubleshooting, and known footguns

- **No GND between Pi and LEDs** → flickering or dead LEDs. Always tie all grounds at the star-ground node.
- **Wrong level-shifter family** → first LED behaves erratically (especially at cold start). Use `74AHCT125`, not `74HC125` or `74LVC125`.
- **PD trigger never negotiates 5V** → some PD trigger modules need a brief load to wake up. Plug into the supply with the Pi already attached.
- **LCD shows garbage / tearing** → `core_freq`/`core_freq_min` not pinned to 400 in `config.txt`, or SPI1 not enabled (`/dev/spidev1.0` missing). Re-check the `dtoverlay=spi1-1cs` line.
- **LCD blank / no reset** → check DC (BCM 25, pin 22) and RST (BCM 24, pin 18); a crossed DC/RST gives a dark or frozen panel. CS is kernel-asserted on SPI1 CE0 (BCM 18) — don't also drive it from GPIO.
- **LCD colors swapped (red↔blue)** → the driver already sets `MADCTL 0x36=0x08` (BGR) and the bench build needs no swap; if yours differs it's a different panel revision.
- **OLED not detected (OLED variant)** → 9 times out of 10 it's swapped SDA/SCL or a `0x3D` jumper. Confirm with `i2cdetect`.
- **Clock reads 1970 on cold boot** → systemd-timesyncd's persisted clock at `/var/lib/systemd/timesync/clock` is missing or timesyncd is disabled. Confirm `systemctl is-enabled systemd-timesyncd`. Do **not** re-enable `fake-hwclock` (it ships masked).
- **NeoPixel data line 470 Ω resistor missing** → can damage the first LED on power-up. Always include it.
- **Brownouts under all-LEDs-white load** → 8 SK6812 at full white draw ~480 mA. Verify the PD supply is genuinely 3 A capable; cheap chargers may sag. Add a second 470 µF cap at the LED stick if needed.
- **Loose perfboard solder joints under thermal cycling** → reflow with a hotter iron and add a small fillet on each joint.
- **`pupcup.local` doesn't resolve from Android phones** → Android still has spotty mDNS support. Print the IP at the top of the dashboard for fallback, and document opening `http://<ip>` in the README.

## 10. Acceptance criteria for "hardware build is done"

- Pi boots from cold; SSH in via `pupcup.local` works in < 60 s.
- `/dev/spidev0.0` (NeoPixel) and `/dev/spidev1.0` (GC9A01) both present. *(OLED variant: `i2cdetect -y 1` shows `0x3C`.)*
- `timedatectl` shows synchronized + `America/New_York`; a cold boot with no wifi comes up at the persisted time, not 1970.
- All four buttons (GREEN 12 / YELLOW 16 / RED 5 / BLUE 6) read low when pressed via `gpioget`.
- Rotary encoder produces clean CLK/DT events on both rotation directions and the SW press registers.
- The NeoPixel walking-pixel test runs cleanly with no flicker for 60 seconds.
- The GC9A01 `hwprobe-lcd` color-cycle and test pattern render with true colors and no tearing. *(OLED variant: the OLED shows test text right-side-up.)*
- The 5V rail measures 4.95–5.10 V at the Pi 5V pin and at the LED Vcc terminal under "all eight LEDs white" load.

When all of the above pass, the device is ready for the software build (see [pupcup_build_plan.md](pupcup_build_plan.md)).

## 11. Building the OLED variant (SSD1306)

Everything above describes the **default GC9A01 round LCD** build. PupCup also
supports a cheaper, easier-to-source **0.96" SSD1306 OLED** (128×64 mono, I²C) —
the same one binary drives either panel, chosen by the `display:` config field.
This section collects **every difference** for the OLED build in one place; treat
it as a patch over the GC9A01 reference build, not a separate procedure.

### 11.1 Why you might pick it
- A few dollars cheaper and stocked everywhere (Adafruit, generic 4-pin modules).
- Only 4 wires (Vcc/GND/SCL/SDA) vs. the LCD's 7, and no `core_freq` pinning.
- The trade-offs: 128×64 monochrome instead of 240×240 full color, and it puts
  the device back on the I²C bus (the GC9A01 build uses no I²C at all).

### 11.2 BOM deltas
- **Add:** one SSD1306 128×64 I²C OLED (4-pin: Vcc/GND/SCL/SDA, address `0x3C`).
- **Add (only if your module lacks them):** 2× 1.8 kΩ–4.7 kΩ I²C pull-ups
  (SDA→3.3V, SCL→3.3V). Most modules already include them — verify with a
  multimeter before adding.
- **Drop:** the GC9A01 LCD module and its 1×7 display header (a 1×4 header
  suffices for the OLED).

### 11.3 Wiring deltas
The buttons, rotary, NeoPixel, power, and ground are **identical** to the
GC9A01 build. Only the display changes — instead of the SPI1 LCD, wire the OLED
on I²C bus 1:

| Signal | GPIO | Header pin | Notes |
|---|---|---|---|
| I²C SDA | GPIO 2 | 3 | SSD1306 `0x3C` |
| I²C SCL | GPIO 3 | 5 | Same bus |
| OLED Vcc | — | 1 / 17 (3.3V) | Most modules accept 3.3V or 5V; use 3.3V |
| OLED GND | — | 6 (star) | — |

The GC9A01's SPI1 pins (BCM 21/20/18 SCLK/MOSI/CE0) and the DC/RST pins (BCM
25/24) are simply **unused** on this build. The RED/BLUE buttons stay on GPIO
5/6 — they moved off 20/21 for the GC9A01 build and there's no need to move them
back. Confirm the bus with a multimeter: SDA and SCL should each sit near 3.3V
(weak pull-up) when idle; sitting low = a short, floating = missing pull-ups.

### 11.4 `config.txt` deltas
Replace the three GC9A01 lines with the single I²C line (keep `dtparam=spi=on`
for the NeoPixel). Under the `[all]` section, with **no inline comments**:

```
dtparam=spi=on        # NeoPixel on SPI0 — unchanged
dtparam=i2c_arm=on    # SSD1306 on I²C bus 1 (replaces the spi1-1cs / core_freq lines)
```

Then load the userspace I²C node module so `/dev/i2c-1` appears (the dtparam
enables the controller; this exposes the device node `i2cdetect` and the driver
open):

```sh
echo i2c-dev | sudo tee /etc/modules-load.d/i2c-dev.conf
```

Also add your login user to the `i2c` group (alongside `gpio`/`spi`):
`sudo usermod -aG gpio,i2c,spi <user>`.

> The one-line installer does all of this for you when you pass
> `--display oled` — it writes the `i2c_arm` dtparam instead of the SPI1 lines,
> creates the `i2c-dev` module-load file, adds the `i2c` group, and sets
> `display: oled` in the config. The deltas above are for a manual build.

### 11.5 Config
Set the panel in `/etc/pupcup/config.yaml`:

```yaml
display: "oled"     # default is "gc9a01"
i2c_bus: 1
oled_addr: 0x3C
```

The `lcd_*` fields are ignored on this build, and the `i2c_bus`/`oled_addr`
fields (ignored on the GC9A01 build) take effect.

### 11.6 Bring-up
The display check in [§8.1](#8-hardware-bring-up-tests) is replaced by the I²C
scan plus the OLED probe; everything else in §8 is unchanged.

```sh
sudo i2cdetect -y 1                                    # expect 0x3C (or 0x3D if the alt-address jumper is set)
GOOS=linux GOARCH=arm64 go build -o /tmp/hwprobe-oled ./cmd/hwprobe/oled
scp /tmp/hwprobe-oled pupcup@pupcup.local:/tmp/ && ssh pupcup@pupcup.local /tmp/hwprobe-oled
```

The probe cycles through four scenes (splash → dog selector → locked summary →
snack mode), 2 s each. **Pass:** all four render legibly, right-side up.
**Fail (blank):** `i2cdetect` shows nothing → swapped SDA/SCL, a `0x3D` jumper,
missing pull-ups, or a short. **Fail (upside down):** a driver flip, not a wiring
fault.

### 11.7 Acceptance criteria deltas
In the §10 checklist, substitute for the GC9A01 rows:
- `/dev/spidev0.0` (NeoPixel) present **and** `i2cdetect -y 1` shows `0x3C`
  (there is no `/dev/spidev1.0` on this build).
- `hwprobe-oled` shows all four scenes legibly and right-side up.

All other acceptance criteria (buttons, rotary, NeoPixel, timekeeping, 5V rail)
are unchanged.
