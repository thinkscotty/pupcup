# PupCup — Hardware Build Plan

A complete, follow-along build for the PupCup appliance: a single-board, headless Raspberry Pi 3B+ that simultaneously serves the PupCup web application **and** acts as the tactile button device with OLED, rotary encoder, and an 8-pixel front-edge status bar. This document covers parts, tools, wiring, perfboard layout, OS provisioning, and bring-up tests. Once verified, the assembly drops into a 3D-printed enclosure (designed separately).

## 1. Overview

| Aspect | Spec |
|---|---|
| Compute | Raspberry Pi 3B+ |
| OS | Raspberry Pi OS Lite 64-bit, Debian 13 ("Trixie") |
| Inputs | 4 colored momentary buttons (Green / Yellow / Red / Blue), 1 KY-040 rotary encoder |
| Outputs | 0.96" SSD1306 OLED 128×64 (I²C), 8× SK6812 NeoPixel stick (SPI) |
| Timekeeping | DS1307 RTC on I²C (kernel-managed, NTP-disciplined) |
| Power | USB-C PD trigger fixed to 5V / 3A → fused 5V rail → Pi + LEDs |
| Network | Wifi only; advertises `pupcup.local` via mDNS |
| Mechanical | Perfboard build with detachable header connectors for OLED, rotary, NeoPixel, and button harness |

The button device and the web server are the same physical box; there is no separate controller MCU.

## 2. Bill of Materials

Quantities listed are the build quantity (1 device). Suggested suppliers in parentheses; substitute equivalents freely.

### 2.1 Core electronics
| # | Part | Qty | Notes |
|---|---|---|---|
| 1 | Raspberry Pi 3B+ (40-pin header pre-installed) | 1 | The 3B+ ships with the GPIO header. Cortex-A53 quad-core @ 1.4 GHz, 1 GB RAM. |
| 2 | microSD card, ≥ 16 GB, A1 / A2 rated | 1 | SanDisk Industrial or Samsung EVO Plus recommended for write endurance. |
| 3 | 0.96" SSD1306 OLED, I²C, 128×64, monochrome | 1 | 4-pin module (Vcc/GND/SCL/SDA). Default I²C address `0x3C`. |
| 4 | KY-040 rotary encoder module | 1 | 5-pin (CLK / DT / SW / + / GND). On-board 10kΩ pull-ups on CLK/DT/SW. |
| 5 | Adafruit NeoPixel Stick — 8× SK6812 RGBW (or RGB) | 1 | Adafruit p/n 1426 (RGB) or 2868 (RGBW). 5V data, 5V Vcc. |
| 6 | DS1307 RTC module with CR2032 backup battery | 1 | Common ZS-042 or generic blue board, I²C address `0x68`. User's existing stock. |
| 7 | 13 mm momentary push-button — Green | 1 | Round flange, 2 contacts, panel-mount. |
| 8 | 13 mm momentary push-button — Yellow | 1 | Same as above. |
| 9 | 13 mm momentary push-button — Red | 1 | Same as above. |
| 10 | 13 mm momentary push-button — Blue | 1 | Same as above. |
| 11 | 74AHCT125 quad level-shift buffer (DIP-14 or SOIC-14) | 1 | Logic-level shift Pi 3.3V → 5V for SK6812 data line. **Must be `AHCT`** family — not `LVC`/`HC`. |
| 12 | USB-C PD trigger module set to 5V (e.g. CH224K, IP2721, ZY12PDN) | 1 | Output 5V/3A. Verify the 5V config jumper/solder bridge is set. |
| 13 | Inline ATC mini fuse holder + 2A fast-blow fuse | 1 | Protects the 5V rail. Or use a polyfuse (PTC) at 1.5A hold. |
| 14 | 1000 µF / 16V electrolytic capacitor | 1 | Bulk decoupling on the 5V rail near the LEDs. |
| 15 | 0.1 µF ceramic capacitors | 4 | Decoupling near 74AHCT125 (1), DS1307 (1), OLED (1), and across the LED stick (1). |
| 16 | 470 Ω, 1/8 W resistor | 1 | Series resistor on SK6812 data line (between buffer output and DIN). |
| 17 | 1.8 kΩ – 4.7 kΩ I²C pull-up resistors | 2 | Most OLED and DS1307 modules already have these; verify with multimeter and only add if absent. |
| 18 | Perfboard, ≥ 70 × 90 mm, double-sided plated | 1 | Adafruit "Perma-Proto" half-size or generic 70×90 mm. |
| 19 | 2.54 mm female header strip — 1×4 | 1 | OLED connector |
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
Roughly $70–$95 for the full BOM at 2026 pricing, dominated by the Pi 3B+ and the NeoPixel stick. The 74AHCT125 and DS1307 are well under $2 each. Buy spares of buttons, the 74AHCT125, and the OLED — they are the most likely "oops" parts.

## 3. Pinout reference

All numbers are **BCM (GPIO) numbering**. Header pins use the standard 40-pin Raspberry Pi convention (pin 1 = 3.3V, pin 2 = 5V, etc.).

| Signal | GPIO | Header pin | Direction | Notes |
|---|---|---|---|---|
| 5V power input | — | 2 (and 4) | in | From the 5V rail; both 5V pins should be tied. |
| 3.3V (for OLED Vcc, DS1307 Vcc, level-shifter Vcc-A) | — | 1 (or 17) | in | Feeds the 3.3V side of devices. |
| Ground | — | 6 | star ground | Plus 9, 14, 20, 25, 30, 34, 39 for short returns. |
| I²C SDA | GPIO 2 | 3 | bidirectional | OLED `0x3C`, DS1307 `0x68` |
| I²C SCL | GPIO 3 | 5 | bidirectional | Same bus |
| SPI MOSI | GPIO 10 | 19 | output (3.3V) | Drives 74AHCT125 input → 5V data → SK6812 DIN |
| Button GREEN | GPIO 21 | 40 | input | Pull-up enabled in software |
| Button YELLOW | GPIO 16 | 36 | input | Pull-up enabled in software |
| Button RED | GPIO 12 | 32 | input | Pull-up enabled in software |
| Button BLUE (snack) | GPIO 20 | 38 | input | Pull-up enabled in software |
| Rotary CLK | GPIO 17 | 11 | input | KY-040 has on-board pull-up; software pull-up also enabled |
| Rotary DT | GPIO 27 | 13 | input | Same as CLK |
| Rotary SW (push) | GPIO 22 | 15 | input | Long-press 1.5 s overrides post-meal lock |

Pins **not** used (kept free for future expansion or accidental shorts to be obvious): GPIO 4, 5, 6, 7, 8, 9, 11, 13, 14, 15, 18, 19, 23, 24, 25, 26. Avoid using GPIO 14/15 (UART) and GPIO 18 (PWM/audio) — they have side effects.

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
5. Ground returns from LED stick, level-shifter, OLED, RTC, rotary, buttons, and Pi all converge at the star-ground node.

### 4.2 3.3V rail
- The Pi supplies 3.3V on header pin 1 (and 17).
- 3.3V → OLED Vcc, DS1307 Vcc (most modules accept either 3.3V or 5V; use 3.3V to avoid a second I²C level concern), 74AHCT125 Vcc-A.
- 0.1 µF decoupling cap between Vcc and GND at each device.

### 4.3 I²C bus (OLED + RTC)
- Pi GPIO 2 (SDA, header 3) → SDA on OLED → SDA on DS1307.
- Pi GPIO 3 (SCL, header 5) → SCL on OLED → SCL on DS1307.
- Confirm with a multimeter that SDA and SCL each measure ~3.3V to ground when the Pi is unpowered with the OLED/RTC modules disconnected (no current flow), then ~3.3V with weak pull-up when powered. If the bus sits low even when idle, you have a short. If it floats (random reading on a high-impedance multimeter), one of the modules is missing pull-ups — add 4.7 kΩ pull-ups from SDA→3.3V and SCL→3.3V.
- The DS1307 module's CR2032 should be present so RTC time persists across power loss.

### 4.4 SPI MOSI → 74AHCT125 → SK6812
The 74AHCT125 is a quad **non-inverting** buffer. We use one of its four gates.

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
| Green wire / Green button | 21 | 40 |
| Yellow wire / Yellow button | 16 | 36 |
| Red wire / Red button | 12 | 32 |
| Blue wire / Blue button | 20 | 38 |

These four pins all live on the bottom-right block of the 40-pin header (pins 32–40), which keeps the button harness wires on a single corner of the perfboard for easier soldering and a cleaner cable run to the panel. Use 2-pin screw terminals or JST-XH headers so the button panel can be detached for enclosure assembly.

### 4.6 Rotary encoder (KY-040)
- KY-040 `+` → Pi 3.3V
- KY-040 `GND` → ground
- KY-040 `CLK` → GPIO 17 (header 11)
- KY-040 `DT` → GPIO 27 (header 13)
- KY-040 `SW` → GPIO 22 (header 15)

The KY-040 module already has 10 kΩ pull-ups on the data lines. Software still asserts internal pull-ups for redundancy and to make wiring less fragile if a future encoder is bare.

## 5. ASCII wiring overview

Header pin layout (looking at the Pi from the top, with the SD card slot at the bottom). Only **used** pins are labeled; unused pins are shown as `·`. Pin 1 is the top-left.

```
     ┌──────────────────────────────────────────────────────┐
     │  3.3V →OLED/RTC/AHCT-Va  ●  ● 5V →fuse→Pi+LEDs       │
     │  SDA  GPIO2 (OLED+RTC)   ●  ● 5V (tied to pin 2)     │
     │  SCL  GPIO3 (OLED+RTC)   ●  ● GND (star)              │
     │  ·                       ●  ● UART TX (unused)        │
     │  GND                     ●  ● UART RX (unused)        │
     │  Rot CLK GPIO17          ●  ● GPIO18 (reserved/audio) │
     │  Rot DT  GPIO27          ●  ● GND                     │
     │  Rot SW  GPIO22          ●  ● GPIO23 (free)           │
     │  3.3V (alt)              ●  ● GPIO24 (free)           │
     │  SPI MOSI GPIO10 → AHCT  ●  ● GND                     │
     │  SPI MISO GPIO9 (unused) ●  ● GPIO25 (free)           │
     │  SPI SCLK GPIO11 (unused)●  ● SPI CE0 GPIO8 (unused)  │
     │  GND                     ●  ● SPI CE1 GPIO7 (unused)  │
     │  ID_SD                   ●  ● ID_SC                   │
     │  GPIO5  (free)           ●  ● GND                     │
     │  GPIO6  (free)           ●  ● Btn RED    GPIO12       │
     │  GPIO13 (free)           ●  ● GND                     │
     │  GPIO19 (free)           ●  ● Btn YELLOW GPIO16       │
     │  GPIO26 (free)           ●  ● Btn BLUE   GPIO20       │
     │  GND                     ●  ● Btn GREEN  GPIO21       │
     └──────────────────────────────────────────────────────┘
```

## 6. Perfboard layout suggestion

A clean layout follows three rules: short signal traces, fat power traces, and detachable peripherals.

```
 Row 1 (top):   [USB-C PD trigger module mounted on edge, 5V output to fuse]
 Row 2:         [Inline fuse]──[1000 µF bulk cap]──[5V rail bus on left edge]
 Row 3:         [3.3V rail bus from Pi pin 1, on right edge]
 Row 4:         [74AHCT125 in DIP socket, with 0.1 µF decoupling cap nearby]
 Row 5:         [470 Ω series resistor → 1×3 header for NeoPixel (5V / GND / Data)]
 Row 6:         [1×4 header for OLED (Vcc 3.3V / GND / SCL / SDA)]
 Row 7:         [1×5 header for KY-040 rotary (+ / GND / CLK / DT / SW)]
 Row 8 (bottom):[4× 2-pin screw terminals for button harness, each labeled by color]
                [1× 2-pin screw terminal for 5V external power input]
 Pi mounted to the right edge with M2.5 standoffs; 40-pin GPIO header used as
 the main signal-bridge — short, color-coded jumpers from the Pi header into
 the labeled landings on the perfboard.
```

Track-cuts: if you use a Perma-Proto-style board with continuous power rails, cut the 5V rail between the Pi-side branch and the LED-side branch only if you observe brownouts; otherwise leave continuous and let the bulk cap absorb spikes. **Always** keep the ground plane continuous.

Detachable headers matter: bench testing the OLED, rotary, NeoPixel, and buttons one-at-a-time is dramatically easier when you can pull the offending peripheral without unsoldering.

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

7. Enable I²C and SPI:

   ```sh
   sudo raspi-config nonint do_i2c 0      # 0 = enable
   sudo raspi-config nonint do_spi 0
   ```

8. Configure the DS1307 RTC. Append to `/boot/firmware/config.txt`:

   ```
   dtparam=i2c_arm=on
   dtparam=spi=on
   dtoverlay=i2c-rtc,ds1307
   ```

9. Remove the fake hwclock so Linux uses the real RTC:

   ```sh
   sudo apt remove -y fake-hwclock
   sudo systemctl disable --now fake-hwclock
   sudo update-rc.d -f fake-hwclock remove
   ```

10. Reboot, then verify:

    ```sh
    sudo reboot
    # … wait …
    ssh pupcup@pupcup.local
    timedatectl                    # Should show America/New_York and "System clock synchronized: yes"
    sudo hwclock -r                # Reads the DS1307 directly
    sudo hwclock -w                # Writes system time → DS1307 (do once after first NTP sync)
    ```

11. Confirm both I²C devices are visible:

    ```sh
    sudo i2cdetect -y 1
    # Expect 0x3C (OLED) and 0x68 (DS1307; sometimes shown as "UU" when claimed by the kernel rtc driver)
    ```

    **RTC drift note**: the DS1307 is a low-cost RTC and commonly drifts 1–2 minutes per month. This is fine for PupCup because Linux re-syncs the system clock from NTP whenever wifi is up (typically every few minutes initially, then hourly), and writes the corrected time back to the DS1307. Drift only matters during a cold boot with no wifi available, in which case timestamps may be off by whatever the RTC has drifted since its last NTP sync — generally seconds, not enough to misorder feedings. For more robust handling of intermittent connectivity, install `chrony` (`sudo apt install chrony`) instead of the default `systemd-timesyncd` — chrony explicitly manages the RTC. A future upgrade to a DS3231 (TCXO, ±2 ppm) is a drop-in I²C replacement at the same `0x68` address; no software changes needed.

12. Add the `pupcup` user to the necessary hardware groups:

    ```sh
    sudo usermod -aG gpio,i2c,spi pupcup
    ```

13. Set the hostname pretty-name (optional cosmetic):

    ```sh
    sudo hostnamectl set-hostname pupcup --pretty "PupCup"
    ```

## 8. Hardware bring-up tests

Run these before bringing up the full Go application. Each isolates one subsystem.

### 8.1 I²C scan (OLED + RTC)
```sh
sudo i2cdetect -y 1
```
Expected: `0x3C` (or `0x3D` if your OLED has the alt-address jumper) and `0x68` (or `UU` if the kernel has already claimed it). Anything else = bad bus, swapped SDA/SCL, missing pull-ups, or a short.

### 8.2 RTC read/write
```sh
date
sudo hwclock -w           # write current system time → RTC
sudo hwclock -r           # read RTC, should match
sudo timedatectl           # confirm "RTC time" matches
```
Pull mains power for ten minutes (don't short anything!), reconnect, confirm RTC retains time. If it resets, the CR2032 is dead or the module's solder jumper for backup is open.

### 8.3 GPIO buttons
A small one-liner via the `gpiod` user-space tools (Trixie ships `libgpiod` **v2**, whose CLI requires the chip via `-c`):
```sh
sudo apt install -y gpiod
gpioget -c gpiochip0 --bias=pull-up 21 16 12 20
```
Press each button while watching the output: held button shows `"<pin>"=inactive`, released shows `"<pin>"=active`. Repeat for each.

### 8.4 Rotary encoder
```sh
gpioget -c gpiochip0 --bias=pull-up 17 27 22
gpiomon -c gpiochip0 --bias=pull-up --edges=both 17 27 22
```
Rotate the dial — you should see CLK and DT toggle; the relative phase indicates direction. Press the encoder shaft and pin 22 should drop briefly.

### 8.5 SPI loopback (sanity)
With nothing connected to MOSI yet:
```sh
sudo apt install -y spi-tools
echo -ne '\xAA\x55\xAA\x55' | spi-pipe -d /dev/spidev0.0 -s 2400000 | xxd
```
You won't get the same bytes back without wiring MISO→MOSI loopback, but the command running without error confirms SPI is enabled.

### 8.6 NeoPixel walking-pixel test
A 30-line test program (to be added under `cmd/hwprobe/neopixel/` in the source tree) writes a buffered SK6812 frame at 2.4 MHz where one pixel is dim white and the rest are off, advancing the lit pixel every 250 ms. Compile on the Pi (`go build`) and run as the `pupcup` user (member of `spi` group). All eight LEDs should light briefly in sequence with no flicker. If the first LED shows the wrong color (typical: green when you expected red), suspect level-shifter wiring or a too-long data trace.

### 8.7 OLED hello
A second small probe program writes "PupCup ✓" to the SSD1306 at 128×64. Confirms I²C address, init sequence, and orientation. (Some modules ship rotated; the driver supports a `--flip` flag.)

Once all bring-up tests pass, the hardware is ready for the application.

## 9. Safety, troubleshooting, and known footguns

- **No GND between Pi and LEDs** → flickering or dead LEDs. Always tie all grounds at the star-ground node.
- **Wrong level-shifter family** → first LED behaves erratically (especially at cold start). Use `74AHCT125`, not `74HC125` or `74LVC125`.
- **PD trigger never negotiates 5V** → some PD trigger modules need a brief load to wake up. Plug into the supply with the Pi already attached.
- **OLED not detected** → 9 times out of 10 it's swapped SDA/SCL or a `0x3D` jumper. Confirm with `i2cdetect`.
- **DS1307 shows `UU` in i2cdetect** → that's correct; the kernel rtc driver has claimed it. Use `hwclock` to interact, not raw I²C.
- **NeoPixel data line 470 Ω resistor missing** → can damage the first LED on power-up. Always include it.
- **Brownouts under all-LEDs-white load** → 8 SK6812 at full white draw ~480 mA. Verify the PD supply is genuinely 3 A capable; cheap chargers may sag. Add a second 470 µF cap at the LED stick if needed.
- **Loose perfboard solder joints under thermal cycling** → reflow with a hotter iron and add a small fillet on each joint.
- **`pupcup.local` doesn't resolve from Android phones** → Android still has spotty mDNS support. Print the IP at the top of the dashboard for fallback, and document opening `http://<ip>` in the README.

## 10. Acceptance criteria for "hardware build is done"

- Pi boots from cold; SSH in via `pupcup.local` works in < 60 s.
- `i2cdetect -y 1` shows `0x3C` and `0x68` (or `UU`).
- `timedatectl` shows synchronized + `America/New_York`; `hwclock -r` returns a sane date.
- All four buttons read low when pressed via `gpioget`.
- Rotary encoder produces clean CLK/DT events on both rotation directions and the SW press registers.
- The NeoPixel walking-pixel test runs cleanly with no flicker for 60 seconds.
- The OLED shows test text right-side-up.
- The 5V rail measures 4.95–5.10 V at the Pi 5V pin and at the LED Vcc terminal under "all eight LEDs white" load.

When all of the above pass, the device is ready for the software build (see [pupcup_build_plan.md](pupcup_build_plan.md)).
